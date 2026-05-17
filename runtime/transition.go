package runtime

import "errors"

type TransitionResult struct {
	NextState State
	Mutations []Mutation
	Effects   []Effect
}

func toolCallKey(call ToolCall) string {
	return call.Name + "\x00" + call.Input
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
	case ToolCallBatchFirstNeedsApproval:
		if state != StateWaitingLLM {
			return TransitionResult{}, errors.New("runtime is not waiting for llm")
		}
		if len(ev.Calls) == 0 {
			return TransitionResult{}, errors.New("empty tool calls")
		}
		toolCalls := make([]*ToolCall, 0, len(ev.Calls))
		for i := range ev.Calls {
			call := ev.Calls[i]
			toolCalls = append(toolCalls, &call)
		}
		return TransitionResult{
			NextState: StateWaitingApproval,
			Mutations: []Mutation{
				AppendAssistantMessage{Message: Message{
					Role:             RoleAssistant,
					Content:          ev.Content,
					ReasoningContent: ev.ReasoningContent,
					ToolCalls:        toolCalls,
				}},
				SetQueuedToolCalls{Calls: ev.Calls[1:]},
				SetPendingTool{Call: ev.Calls[0]},
			},
		}, nil
	case ToolCallBatchFirstReadyToRun:
		if state != StateWaitingLLM {
			return TransitionResult{}, errors.New("runtime is not waiting for llm")
		}
		if len(ev.Calls) == 0 {
			return TransitionResult{}, errors.New("empty tool calls")
		}
		toolCalls := make([]*ToolCall, 0, len(ev.Calls))
		for i := range ev.Calls {
			call := ev.Calls[i]
			toolCalls = append(toolCalls, &call)
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{
				AppendAssistantMessage{Message: Message{
					Role:             RoleAssistant,
					Content:          ev.Content,
					ReasoningContent: ev.ReasoningContent,
					ToolCalls:        toolCalls,
				}},
				SetQueuedToolCalls{Calls: ev.Calls[1:]},
			},
			Effects: []Effect{RunTool{Call: ev.Calls[0]}},
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
			Mutations: []Mutation{ClearPendingTool{}, AddAlwaysAllow{Key: toolCallKey(ev.Call)}},
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
	case NoMoreToolCalls:
		if state != StateAdvancingQueue {
			return TransitionResult{}, errors.New("invalid state for no more tool calls")
		}
		return TransitionResult{
			NextState: StateWaitingLLM,
			Effects:   []Effect{CallModel{}},
		}, nil
	case QueuedToolCallNeedsApproval:
		if state != StateAdvancingQueue {
			return TransitionResult{}, errors.New("invalid state for next tool call")
		}
		return TransitionResult{
			NextState: StateWaitingApproval,
			Mutations: []Mutation{
				SetPendingTool{Call: ev.Call},
				PopQueuedToolCall{},
			},
		}, nil
	case QueuedToolCallReadyToRun:
		if state != StateAdvancingQueue {
			return TransitionResult{}, errors.New("invalid state for next tool call")
		}
		return TransitionResult{
			NextState: StateRunningTool,
			Mutations: []Mutation{PopQueuedToolCall{}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ErrorOccurred:
		return TransitionResult{
			NextState: StateIdle,
			Mutations: []Mutation{
				ClearPendingTool{},
				ClearQueuedToolCalls{},
				ClearPendingEffects{},
			},
		}, nil
	case CancelRequested:
		return TransitionResult{
			NextState: StateIdle,
			Mutations: []Mutation{
				ClearPendingTool{},
				ClearQueuedToolCalls{},
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
