package machine

type TransitionResult struct {
	NextState State
	Mutations []Mutation
	Effects   []Effect
}

func toolBatchID(calls []ToolCall) string {
	if len(calls) == 0 || calls[0].ID == "" {
		return "batch"
	}
	return "batch-" + calls[0].ID
}

func toolCallPointers(calls []ToolCall) []*ToolCall {
	toolCalls := make([]*ToolCall, 0, len(calls))
	for i := range calls {
		call := calls[i]
		toolCalls = append(toolCalls, &call)
	}
	return toolCalls
}

func runtimeErrorMessage(err error) string {
	if err == nil {
		return "unknown runtime error"
	}
	return err.Error()
}

func Transition(snapshot MachineSnapshot, event Event) (TransitionResult, error) {
	state := snapshot.state
	unexpected := func() (TransitionResult, error) {
		return TransitionResult{}, UnexpectedEventError{State: state, Event: event}
	}
	protocolViolation := func(reason string) (TransitionResult, error) {
		return TransitionResult{}, ProtocolViolationError{State: state, Event: event, Reason: reason}
	}
	switch ev := event.(type) {
	case UserMessageSubmitted:
		if state != StateIdle {
			return unexpected()
		}
		return TransitionResult{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{AppendUserMessage{Content: ev.Content}},
			Effects:   []Effect{CallModel{}},
		}, nil
	case AssistantMessageReceived:
		if state != StateWaitingLLM {
			return unexpected()
		}
		return TransitionResult{
			NextState: StateIdle,
			Mutations: []Mutation{AppendAssistantMessage{Message: Message{
				Role:             RoleAssistant,
				Content:          ev.Response.Content,
				ReasoningContent: ev.Response.ReasoningContent,
			}}},
		}, nil
	case ToolBatchReceived:
		if state != StateWaitingLLM {
			return unexpected()
		}
		if len(ev.Calls) == 0 {
			return protocolViolation("tool batch is empty")
		}
		return TransitionResult{
			NextState: StateAdvancingQueue,
			Mutations: []Mutation{
				AppendAssistantMessage{Message: Message{
					Role:             RoleAssistant,
					Content:          ev.Content,
					ReasoningContent: ev.ReasoningContent,
					ToolCalls:        toolCallPointers(ev.Calls),
				}},
				SetToolCallBatch{ID: toolBatchID(ev.Calls), Calls: ev.Calls},
			},
			Effects: []Effect{ProcessNextToolCall{}},
		}, nil
	case ApprovalGranted:
		if state != StateWaitingApproval {
			return unexpected()
		}
		if snapshot.pendingTool == nil {
			return protocolViolation("approval has no pending tool")
		}
		if !sameToolCall(ev.Call, *snapshot.pendingTool) {
			return protocolViolation("approved call does not match pending tool")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{SetCurrentTool{Call: ev.Call}, ClearPendingTool{}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ApprovalAlwaysGranted:
		if state != StateWaitingApproval {
			return unexpected()
		}
		if snapshot.pendingTool == nil {
			return protocolViolation("approval has no pending tool")
		}
		if !sameToolCall(ev.Call, *snapshot.pendingTool) {
			return protocolViolation("approved call does not match pending tool")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{SetCurrentTool{Call: ev.Call}, ClearPendingTool{}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ApprovalDenied:
		if state != StateWaitingApproval {
			return unexpected()
		}
		if snapshot.pendingTool == nil {
			return protocolViolation("denial has no pending tool")
		}
		if !sameToolCall(ev.Call, *snapshot.pendingTool) {
			return protocolViolation("denied call does not match pending tool")
		}
		return TransitionResult{
			NextState: StateAdvancingQueue,
			Mutations: []Mutation{
				ClearPendingTool{},
				AppendToolResult{Call: ev.Call, Result: "denied: " + ev.Call.Name},
			},
			Effects: []Effect{ProcessNextToolCall{}},
		}, nil
	case ToolResultReceived:
		if state != StateRunningTool {
			return unexpected()
		}
		if snapshot.currentTool == nil {
			return protocolViolation("tool result has no current tool")
		}
		if !sameToolCall(ev.Call, *snapshot.currentTool) {
			return protocolViolation("result call does not match current tool")
		}
		return TransitionResult{
			NextState: StateAdvancingQueue,
			Mutations: []Mutation{
				AppendToolResult{Call: ev.Call, Result: ev.Result},
				ClearCurrentTool{},
			},
			Effects: []Effect{ProcessNextToolCall{}},
		}, nil
	case ToolBatchFinished:
		if state != StateAdvancingQueue {
			return unexpected()
		}
		if !snapshot.queue.empty() {
			return protocolViolation("tool batch finished before the queue was empty")
		}
		return TransitionResult{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{ClearToolCallBatch{}},
			Effects:   []Effect{CallModel{}},
		}, nil
	case ToolCallNeedsApproval:
		if state != StateAdvancingQueue {
			return unexpected()
		}
		if snapshot.queue.next == nil {
			return protocolViolation("approval requested with no next tool")
		}
		if !sameToolCall(ev.Call, *snapshot.queue.next) {
			return protocolViolation("approval call does not match next tool")
		}
		return TransitionResult{
			NextState: StateWaitingApproval,
			Mutations: []Mutation{
				SetPendingTool{Call: ev.Call, Request: ev.Request},
				AdvanceToolCallBatch{},
			},
		}, nil
	case ToolCallReadyToRun:
		if state != StateAdvancingQueue {
			return unexpected()
		}
		if snapshot.queue.next == nil {
			return protocolViolation("ready call has no next tool")
		}
		if !sameToolCall(ev.Call, *snapshot.queue.next) {
			return protocolViolation("ready call does not match next tool")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{AdvanceToolCallBatch{}, SetCurrentTool{Call: ev.Call}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ErrorOccurred:
		return TransitionResult{
			NextState: StateIdle,
			Mutations: []Mutation{
				FlushStreamingAssistant{Interrupted: true},
				AppendToolResult{
					Call:   ToolCall{ID: "runtime_error", Name: "runtime_error"},
					Result: runtimeErrorMessage(ev.Err),
				},
				ClearPendingTool{},
				ClearCurrentTool{},
				ClearToolCallBatch{},
				ClearPendingEffects{},
			},
		}, nil
	case CancelRequested:
		return TransitionResult{
			NextState: StateIdle,
			Mutations: []Mutation{
				FlushStreamingAssistant{Interrupted: true},
				ClearPendingTool{},
				ClearCurrentTool{},
				ClearToolCallBatch{},
				ClearPendingEffects{},
			},
		}, nil
	case EngineReady:
		if state != StateInitializing {
			return unexpected()
		}
		return TransitionResult{NextState: StateIdle}, nil
	case ResetRequested:
		return TransitionResult{NextState: StateIdle, Mutations: []Mutation{ResetContext{}}}, nil
	default:
		return unexpected()
	}
}
