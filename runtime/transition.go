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
	case ToolCallRequested:
		if state != StateWaitingLLM {
			return Decision{}, errors.New("runtime is not waiting for llm")
		}
		message := Message{
			Role:             RoleAssistant,
			ReasoningContent: ev.ReasoningContent,
			ToolCalls:        []*ToolCall{&ev.Call},
		}
		mutations := []Mutation{AppendAssistantMessage{Message: message}}
		if ev.NeedsApproval {
			mutations = append(mutations, SetPendingTool{Call: ev.Call})
			return Decision{NextState: StateWaitingApproval, Mutations: mutations}, nil
		}
		return Decision{NextState: StateRunningTool, Mutations: mutations, Effects: []Effect{RunTool{Call: ev.Call}}}, nil
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
		return Decision{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{
				ClearPendingTool{},
				AppendToolResult{Call: ev.Call, Result: "denied: " + ev.Call.Name},
			},
			Effects: []Effect{CallModel{}},
		}, nil
	case ToolResultReceived:
		if state != StateRunningTool && state != StateWaitingTool {
			return Decision{}, errors.New("runtime is not waiting for tool result")
		}
		return Decision{
			NextState: StateWaitingLLM,
			Mutations: []Mutation{AppendToolResult{Call: ev.Call, Result: ev.Result}},
			Effects:   []Effect{CallModel{}},
		}, nil
	case ErrorOccurred:
		return Decision{
			NextState: StateIdle,
			Mutations: []Mutation{
				ClearPendingTool{},
				ClearPendingEffects{},
			},
		}, nil
	case CancelRequested:
		return Decision{
			NextState: StateIdle,
			Mutations: []Mutation{
				ClearPendingTool{},
				ClearPendingEffects{},
			},
		}, nil
	case ResetRequested:
		return Decision{NextState: StateIdle, Mutations: []Mutation{ResetContext{}}}, nil
	default:
		return Decision{}, errors.New("unknown event")
	}
}
