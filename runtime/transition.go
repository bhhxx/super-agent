package runtime

import "errors"

type Decision struct {
	NextState State
	Mutations []Mutation
	Effects   []Effect
}

func Transition(state State, event Event) (Decision, error) {
	switch ev := event.(type) {
	case UserMessageSubmitted:
		if state != StateIdle {
			return Decision{}, errors.New("runtime is not idle")
		}
		return Decision{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{AppendUserMessage{Content: ev.Content}},
			Effects:   []Effect{CallModel{}},
		}, nil
	case AssistantMessageReceived:
		if state != StateWaitingLLM {
			return Decision{}, errors.New("runtime is not waiting for llm")
		}
		return Decision{
			NextState: StateIdle,
			Mutations: []Mutation{AppendAssistantMessage{Message: Message{
				Role:             RoleAssistant,
				Content:          ev.Response.FinalAnswer,
				ReasoningContent: ev.Response.ReasoningContent,
			}}},
		}, nil
	case ToolCallsRequested:
		if state != StateWaitingLLM {
			return Decision{}, errors.New("runtime is not waiting for llm")
		}
		if len(ev.Calls) == 0 {
			return Decision{}, errors.New("tool call list is empty")
		}
		toolCalls := make([]*ToolCall, 0, len(ev.Calls))
		for i := range ev.Calls {
			call := ev.Calls[i]
			toolCalls = append(toolCalls, &call)
		}
		message := Message{
			Role:             RoleAssistant,
			Content:          ev.FinalAnswer,
			ReasoningContent: ev.ReasoningContent,
			ToolCalls:        toolCalls,
		}
		mutations := []Mutation{
			AppendAssistantMessage{Message: message},
			SetPendingToolQueue{Calls: ev.Calls[1:]},
		}
		firstCall := ev.Calls[0]
		if ev.NeedsApproval {
			mutations = append(mutations, SetPendingTool{Call: firstCall})
			return Decision{NextState: StateWaitingApproval, Mutations: mutations}, nil
		}
		return Decision{NextState: StateRunningTool, Mutations: mutations, Effects: []Effect{RunTool{Call: firstCall}}}, nil
	case ApprovalGranted:
		if state != StateWaitingApproval {
			return Decision{}, errors.New("no tool is waiting for approval")
		}
		return Decision{
			NextState: StateRunningTool,
			Mutations: []Mutation{ClearPendingTool{}},
			Effects:   []Effect{RunTool{Call: ev.Call}},
		}, nil
	case ApprovalDenied:
		if state != StateWaitingApproval {
			return Decision{}, errors.New("no tool is waiting for approval")
		}
		mutations := []Mutation{
			ClearPendingTool{},
			AppendToolResult{Call: ev.Call, Result: "denied: " + ev.Call.Name},
		}
		if ev.NextCall != nil {
			if ev.NextNeedsApproval {
				mutations = append(mutations, SetPendingTool{Call: *ev.NextCall})
				return Decision{NextState: StateWaitingApproval, Mutations: mutations}, nil
			}
			return Decision{NextState: StateRunningTool, Mutations: mutations, Effects: []Effect{RunTool{Call: *ev.NextCall}}}, nil
		}
		return Decision{NextState: StateWaitingLLM, Mutations: mutations, Effects: []Effect{CallModel{}}}, nil
	case ToolResultReceived:
		if state != StateRunningTool {
			return Decision{}, errors.New("runtime is not waiting for tool result")
		}
		mutations := []Mutation{AppendToolResult{Call: ev.Call, Result: ev.Result}}
		if ev.NextCall != nil {
			if ev.NextNeedsApproval {
				mutations = append(mutations, SetPendingTool{Call: *ev.NextCall})
				return Decision{NextState: StateWaitingApproval, Mutations: mutations}, nil
			}
			return Decision{NextState: StateRunningTool, Mutations: mutations, Effects: []Effect{RunTool{Call: *ev.NextCall}}}, nil
		}
		return Decision{NextState: StateWaitingLLM, Mutations: mutations, Effects: []Effect{CallModel{}}}, nil
	case ErrorOccurred:
		return Decision{
			NextState: StateIdle,
			Mutations: []Mutation{
				ClearPendingTool{},
				ClearPendingToolQueue{},
				ClearPendingEffects{},
			},
		}, nil
	case CancelRequested:
		return Decision{
			NextState: StateIdle,
			Mutations: []Mutation{
				ClearPendingTool{},
				ClearPendingToolQueue{},
				ClearPendingEffects{},
			},
		}, nil
	case ResetRequested:
		return Decision{NextState: StateIdle, Mutations: []Mutation{ResetContext{}}}, nil
	default:
		return Decision{}, errors.New("unknown event")
	}
}
