package machine

import "errors"

type TransitionResult struct {
	NextState State
	Mutations []Mutation
	Effects   []Effect
}

func toolCallKey(call ToolCall) string {
	return call.Name + "\x00" + call.Input
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

func Transition(state State, event Event) (TransitionResult, error) {
	switch ev := event.(type) {
	case UserMessageSubmitted:
		if state != StateIdle {
			return TransitionResult{}, errors.New("runtime is not idle")
		}
		return TransitionResult{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{AppendUserMessage{Content: ev.Content}},
			Effects:   []Effect{CallModel{}},
		}, nil
	case AssistantMessageReceived:
		if state != StateWaitingLLM {
			return TransitionResult{}, errors.New("runtime is not waiting for llm")
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
			return TransitionResult{}, errors.New("runtime is not waiting for llm")
		}
		if len(ev.Calls) == 0 {
			return TransitionResult{}, errors.New("empty tool calls")
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
			return TransitionResult{}, errors.New("no tool is waiting for approval")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{ClearPendingTool{}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ApprovalAlwaysGranted:
		if state != StateWaitingApproval {
			return TransitionResult{}, errors.New("no tool is waiting for approval")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{ClearPendingTool{}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ApprovalDenied:
		if state != StateWaitingApproval {
			return TransitionResult{}, errors.New("no tool is waiting for approval")
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
			return TransitionResult{}, errors.New("runtime is not waiting for tool result")
		}
		return TransitionResult{
			NextState: StateAdvancingQueue,
			Mutations: []Mutation{
				AppendToolResult{Call: ev.Call, Result: ev.Result},
			},
			Effects: []Effect{ProcessNextToolCall{}},
		}, nil
	case ToolBatchFinished:
		if state != StateAdvancingQueue {
			return TransitionResult{}, errors.New("invalid state for no more tool calls")
		}
		return TransitionResult{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{ClearToolCallBatch{}},
			Effects:   []Effect{CallModel{}},
		}, nil
	case ToolCallNeedsApproval:
		if state != StateAdvancingQueue {
			return TransitionResult{}, errors.New("invalid state for next tool call")
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
			return TransitionResult{}, errors.New("invalid state for next tool call")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{AdvanceToolCallBatch{}},
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
				ClearToolCallBatch{},
				ClearPendingEffects{},
			},
		}, nil
	case EngineReady:
		if state != StateInitializing {
			return TransitionResult{}, errors.New("runtime is not initializing")
		}
		return TransitionResult{NextState: StateIdle}, nil
	case ResetRequested:
		return TransitionResult{NextState: StateIdle, Mutations: []Mutation{ResetContext{}}}, nil
	default:
		return TransitionResult{}, errors.New("unknown event")
	}
}
