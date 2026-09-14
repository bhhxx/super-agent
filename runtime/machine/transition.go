package machine

type TransitionResult struct {
	NextState          State
	RuntimeDataChanges []RuntimeDataChange
	ActionPlan         ActionPlan
}

type transitionKey struct {
	state State
	event eventKind
}

type transitionHandler func(MachineSnapshot, Event) (TransitionResult, error)

type transitionRule struct {
	key     transitionKey
	handler transitionHandler
}

// stateTransitions is the complete static edge registry for the machine graph.
var stateTransitions = newTransitionRegistry()

func newTransitionRegistry() map[transitionKey]transitionHandler {
	registry := make(map[transitionKey]transitionHandler)
	rules := []transitionRule{
		{transitionKey{StateInitializing, eventEngineReady}, adaptTransition(handleEngineReady)},
		{transitionKey{StateIdle, eventUserMessageSubmitted}, adaptTransition(handleUserMessageSubmitted)},
		{transitionKey{StateWaitingLLM, eventAssistantMessageReceived}, adaptTransition(handleAssistantMessageReceived)},
		{transitionKey{StateWaitingLLM, eventToolBatchReceived}, adaptTransition(handleToolBatchReceived)},
		{transitionKey{StateWaitingApproval, eventApprovalGranted}, adaptTransition(handleApprovalGranted)},
		{transitionKey{StateWaitingApproval, eventApprovalAlwaysGranted}, adaptTransition(handleApprovalAlwaysGranted)},
		{transitionKey{StateWaitingApproval, eventApprovalDenied}, adaptTransition(handleApprovalDenied)},
		{transitionKey{StateRunningTool, eventToolResultReceived}, adaptTransition(handleToolResultReceived)},
		{transitionKey{StateAdvancingQueue, eventToolBatchFinished}, adaptTransition(handleToolBatchFinished)},
		{transitionKey{StateAdvancingQueue, eventToolCallNeedsApproval}, adaptTransition(handleToolCallNeedsApproval)},
		{transitionKey{StateAdvancingQueue, eventToolCallReadyToRun}, adaptTransition(handleToolCallReadyToRun)},
		{transitionKey{StateAdvancingQueue, eventToolCallDenied}, adaptTransition(handleToolCallDenied)},
		{transitionKey{event: eventErrorOccurred}, adaptTransition(handleErrorOccurred)},
		{transitionKey{event: eventCancelRequested}, adaptTransition(handleCancelRequested)},
		{transitionKey{event: eventResetRequested}, adaptTransition(handleResetRequested)},
	}
	for _, rule := range rules {
		registerTransition(registry, rule.key, rule.handler)
	}
	return registry
}

func registerTransition(registry map[transitionKey]transitionHandler, key transitionKey, handler transitionHandler) {
	if _, exists := registry[key]; exists {
		panic("duplicate state transition: " + string(key.state) + " + " + string(key.event))
	}
	registry[key] = handler
}

// adaptTransition 函数只有一个参数，通过该参数，可以泛型编译时推导出 E
func adaptTransition[E Event](
	handler func(MachineSnapshot, E) (TransitionResult, error),
) transitionHandler {
	return func(snapshot MachineSnapshot, event Event) (TransitionResult, error) {
		typed, ok := event.(E)
		if !ok {
			return protocolViolation(snapshot, event, "registered handler has incompatible event type")
		}
		return handler(snapshot, typed)
	}
}

func Transition(snapshot MachineSnapshot, event Event) (TransitionResult, error) {
	if event == nil {
		return unexpectedEvent(snapshot, event)
	}
	handler, ok := stateTransitions[transitionKey{state: snapshot.state, event: event.kind()}]
	if !ok {
		// The zero State key represents an event accepted from any state.
		handler, ok = stateTransitions[transitionKey{event: event.kind()}]
		if !ok {
			return unexpectedEvent(snapshot, event)
		}
	}
	return handler(snapshot, event)
}

func unexpectedEvent(snapshot MachineSnapshot, event Event) (TransitionResult, error) {
	return TransitionResult{}, UnexpectedEventError{State: snapshot.state, Event: event}
}

func protocolViolation(snapshot MachineSnapshot, event Event, reason string) (TransitionResult, error) {
	return TransitionResult{}, ProtocolViolationError{State: snapshot.state, Event: event, Reason: reason}
}

func handleEngineReady(MachineSnapshot, EngineReady) (TransitionResult, error) {
	return TransitionResult{NextState: StateIdle}, nil
}

func handleUserMessageSubmitted(_ MachineSnapshot, event UserMessageSubmitted) (TransitionResult, error) {
	return TransitionResult{
		NextState:          StateWaitingLLM,
		RuntimeDataChanges: []RuntimeDataChange{AppendUserMessage{Content: event.Content, Attachments: event.Attachments}},
		ActionPlan:         ActionPlan{Schedule: []ScheduledAction{CallModel{}}},
	}, nil
}

func handleAssistantMessageReceived(_ MachineSnapshot, event AssistantMessageReceived) (TransitionResult, error) {
	return TransitionResult{
		NextState: StateIdle,
		RuntimeDataChanges: []RuntimeDataChange{AppendAssistantMessage{Message: Message{
			Role:             RoleAssistant,
			Content:          event.Response.Content,
			ReasoningContent: event.Response.ReasoningContent,
		}}},
	}, nil
}

func handleToolBatchReceived(snapshot MachineSnapshot, event ToolBatchReceived) (TransitionResult, error) {
	if len(event.Calls) == 0 {
		return protocolViolation(snapshot, event, "tool batch is empty")
	}
	return TransitionResult{
		NextState: StateAdvancingQueue,
		RuntimeDataChanges: []RuntimeDataChange{
			AppendAssistantMessage{Message: Message{
				Role:             RoleAssistant,
				Content:          event.Content,
				ReasoningContent: event.ReasoningContent,
				ToolCalls:        toolCallPointers(event.Calls),
			}},
			SetToolCallBatch{ID: toolBatchID(event.Calls), Calls: event.Calls},
		},
		ActionPlan: ActionPlan{Schedule: []ScheduledAction{CheckToolQueue{}}},
	}, nil
}

func handleApprovalGranted(snapshot MachineSnapshot, event ApprovalGranted) (TransitionResult, error) {
	return approveTool(snapshot, event, event.Call)
}

func handleApprovalAlwaysGranted(snapshot MachineSnapshot, event ApprovalAlwaysGranted) (TransitionResult, error) {
	return approveTool(snapshot, event, event.Call)
}

func approveTool(snapshot MachineSnapshot, event Event, call ToolCall) (TransitionResult, error) {
	if snapshot.pendingTool == nil {
		return protocolViolation(snapshot, event, "approval has no pending tool")
	}
	if !sameToolCall(call, *snapshot.pendingTool) {
		return protocolViolation(snapshot, event, "approved call does not match pending tool")
	}
	return TransitionResult{
		NextState:          StateRunningTool,
		RuntimeDataChanges: []RuntimeDataChange{SetCurrentTool{Call: call}, ClearPendingTool{}},
		ActionPlan:         ActionPlan{Schedule: []ScheduledAction{RunTool{Call: call}}},
	}, nil
}

func handleApprovalDenied(snapshot MachineSnapshot, event ApprovalDenied) (TransitionResult, error) {
	if snapshot.pendingTool == nil {
		return protocolViolation(snapshot, event, "denial has no pending tool")
	}
	if !sameToolCall(event.Call, *snapshot.pendingTool) {
		return protocolViolation(snapshot, event, "denied call does not match pending tool")
	}
	return TransitionResult{
		NextState: StateAdvancingQueue,
		RuntimeDataChanges: []RuntimeDataChange{
			ClearPendingTool{},
			AppendToolResult{Call: event.Call, Result: "denied: " + event.Call.Name},
		},
		ActionPlan: ActionPlan{Schedule: []ScheduledAction{CheckToolQueue{}}},
	}, nil
}

func handleToolResultReceived(snapshot MachineSnapshot, event ToolResultReceived) (TransitionResult, error) {
	if snapshot.currentTool == nil {
		return protocolViolation(snapshot, event, "tool result has no current tool")
	}
	if !sameToolCall(event.Call, *snapshot.currentTool) {
		return protocolViolation(snapshot, event, "result call does not match current tool")
	}
	return TransitionResult{
		NextState: StateAdvancingQueue,
		RuntimeDataChanges: []RuntimeDataChange{
			AppendToolResult{Call: event.Call, Result: event.Result},
			ClearCurrentTool{},
		},
		ActionPlan: ActionPlan{Schedule: []ScheduledAction{CheckToolQueue{}}},
	}, nil
}

func handleToolBatchFinished(snapshot MachineSnapshot, event ToolBatchFinished) (TransitionResult, error) {
	if !snapshot.queue.empty() {
		return protocolViolation(snapshot, event, "tool batch finished before the queue was empty")
	}
	return TransitionResult{
		NextState:          StateWaitingLLM,
		RuntimeDataChanges: []RuntimeDataChange{ClearToolCallBatch{}},
		ActionPlan:         ActionPlan{Schedule: []ScheduledAction{CallModel{}}},
	}, nil
}

func handleToolCallNeedsApproval(snapshot MachineSnapshot, event ToolCallNeedsApproval) (TransitionResult, error) {
	if snapshot.queue.next == nil {
		return protocolViolation(snapshot, event, "approval requested with no next tool")
	}
	if !sameToolCall(event.Call, *snapshot.queue.next) {
		return protocolViolation(snapshot, event, "approval call does not match next tool")
	}
	return TransitionResult{
		NextState: StateWaitingApproval,
		RuntimeDataChanges: []RuntimeDataChange{
			SetPendingTool{Call: event.Call, Request: event.Request},
			AdvanceToolCallBatch{},
		},
		ActionPlan: ActionPlan{Schedule: []ScheduledAction{AwaitApproval{Call: event.Call, Request: event.Request}}},
	}, nil
}

func handleToolCallReadyToRun(snapshot MachineSnapshot, event ToolCallReadyToRun) (TransitionResult, error) {
	if snapshot.queue.next == nil {
		return protocolViolation(snapshot, event, "ready call has no next tool")
	}
	if !sameToolCall(event.Call, *snapshot.queue.next) {
		return protocolViolation(snapshot, event, "ready call does not match next tool")
	}
	return TransitionResult{
		NextState:          StateRunningTool,
		RuntimeDataChanges: []RuntimeDataChange{AdvanceToolCallBatch{}, SetCurrentTool{Call: event.Call}},
		ActionPlan:         ActionPlan{Schedule: []ScheduledAction{RunTool{Call: event.Call}}},
	}, nil
}

func handleToolCallDenied(snapshot MachineSnapshot, event ToolCallDenied) (TransitionResult, error) {
	if snapshot.queue.next == nil {
		return protocolViolation(snapshot, event, "denied call has no next tool")
	}
	if !sameToolCall(event.Call, *snapshot.queue.next) {
		return protocolViolation(snapshot, event, "denied call does not match next tool")
	}
	return TransitionResult{
		NextState: StateAdvancingQueue,
		RuntimeDataChanges: []RuntimeDataChange{
			AdvanceToolCallBatch{},
			AppendToolResult{Call: event.Call, Result: "denied by permission policy: " + event.Reason},
		},
		ActionPlan: ActionPlan{Schedule: []ScheduledAction{CheckToolQueue{}}},
	}, nil
}

// outstandingToolResults answers every tool call the model asked for but that
// has not been answered yet. A dispatched call is always in exactly one of
// three places: awaiting approval, running, or already answered by an earlier
// append. So the outstanding set is the pending call, the current call, and
// every call the batch has not reached yet.
//
// Every tool call the model asked for must be answered. An unanswered call
// produces a transcript the provider rejects with a 400, and because the
// transcript is persisted the failure survives a resume. Both the error and
// the cancel path go through here.
func outstandingToolResults(snapshot MachineSnapshot, runningResult, notExecuted string) []RuntimeDataChange {
	var results []RuntimeDataChange
	if snapshot.pendingTool != nil {
		results = append(results, AppendToolResult{
			Call:   *snapshot.pendingTool,
			Result: notExecuted,
		})
	}
	if snapshot.currentTool != nil {
		results = append(results, AppendToolResult{Call: *snapshot.currentTool, Result: runningResult})
	}
	for _, call := range snapshot.queue.remaining {
		results = append(results, AppendToolResult{
			Call:   call,
			Result: notExecuted,
		})
	}
	return results
}

func handleErrorOccurred(snapshot MachineSnapshot, event ErrorOccurred) (TransitionResult, error) {
	reason := runtimeErrorMessage(event.Err)
	changes := append([]RuntimeDataChange{FlushStreamingAssistant{Interrupted: true}},
		outstandingToolResults(snapshot, reason, "not executed: "+reason)...)
	changes = append(changes,
		ClearPendingTool{},
		ClearCurrentTool{},
		ClearToolCallBatch{},
	)
	return TransitionResult{
		NextState:          StateIdle,
		RuntimeDataChanges: changes,
		ActionPlan:         ActionPlan{ClearExisting: true},
	}, nil
}

func handleCancelRequested(snapshot MachineSnapshot, _ CancelRequested) (TransitionResult, error) {
	changes := append([]RuntimeDataChange{FlushStreamingAssistant{Interrupted: true}},
		outstandingToolResults(snapshot, "cancelled", "not executed: cancelled")...)
	changes = append(changes,
		ClearPendingTool{},
		ClearCurrentTool{},
		ClearToolCallBatch{},
	)
	return TransitionResult{
		NextState:          StateIdle,
		RuntimeDataChanges: changes,
		ActionPlan:         ActionPlan{ClearExisting: true},
	}, nil
}

func handleResetRequested(MachineSnapshot, ResetRequested) (TransitionResult, error) {
	return TransitionResult{
		NextState:          StateIdle,
		RuntimeDataChanges: []RuntimeDataChange{ResetConversation{}},
		ActionPlan:         ActionPlan{ClearExisting: true},
	}, nil
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
