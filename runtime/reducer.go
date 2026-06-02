package runtime

import "fmt"

type Reducer interface {
	Apply(state *EngineState, clearEffects func(), mutation Mutation)
}

type DefaultReducer struct{}

func (DefaultReducer) Apply(state *EngineState, clearEffects func(), mutation Mutation) {
	switch m := mutation.(type) {
	case AppendUserMessage:
		state.Messages = append(state.Messages, Message{Role: RoleUser, Content: m.Content})
	case AppendAssistantMessage:
		state.Messages = append(state.Messages, m.Message)
	case AppendToolResult:
		state.Messages = append(state.Messages, Message{Role: RoleTool, Content: m.Result, ToolCallID: m.Call.ID, ToolName: m.Call.Name})
	case SetPendingTool:
		call := m.Call
		state.PendingTool = &call
	case SetQueuedToolCalls:
		state.QueuedToolCalls = append([]ToolCall(nil), m.Calls...)
	case PopQueuedToolCall:
		if len(state.QueuedToolCalls) > 0 {
			state.QueuedToolCalls[0] = ToolCall{}
			state.QueuedToolCalls = state.QueuedToolCalls[1:]
		}
	case ClearPendingTool:
		state.PendingTool = nil
	case ClearQueuedToolCalls:
		state.QueuedToolCalls = nil
	case ClearPendingEffects:
		clearEffects()
	case ResetContext:
		state.Messages = nil
		state.PendingTool = nil
		state.QueuedToolCalls = nil
		clearEffects()
	default:
		panic(fmt.Sprintf("unknown mutation: %T", m))
	}
}
