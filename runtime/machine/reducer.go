package machine

import "fmt"

type Reducer interface {
	Apply(state *EngineState, clearEffects func(), mutation Mutation)
}

type DefaultReducer struct{}

func (DefaultReducer) Apply(state *EngineState, clearEffects func(), mutation Mutation) {
	switch m := mutation.(type) {
	case AppendUserMessage:
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		state.Messages = append(state.Messages, Message{Role: RoleUser, Content: m.Content})
	case AppendAssistantMessage:
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		state.Messages = append(state.Messages, m.Message)
	case AppendToolResult:
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		state.Messages = append(state.Messages, Message{Role: RoleTool, Content: m.Result, ToolCallID: m.Call.ID, ToolName: m.Call.Name})
	case AppendStreamingAssistant:
		state.StreamingContent += m.Chunk.ContentDelta
		state.StreamingReasoning += m.Chunk.ReasoningContentDelta
	case FlushStreamingAssistant:
		if state.StreamingContent != "" || state.StreamingReasoning != "" {
			state.Messages = append(state.Messages, Message{
				Role:             RoleAssistant,
				Content:          state.StreamingContent,
				ReasoningContent: state.StreamingReasoning,
				Interrupted:      m.Interrupted,
			})
		}
		state.StreamingContent = ""
		state.StreamingReasoning = ""
	case SetPendingTool:
		call := m.Call
		state.PendingTool = &call
	case SetToolCallBatch:
		state.ToolBatch = &ToolCallBatch{
			ID:    m.ID,
			Calls: append([]ToolCall(nil), m.Calls...),
		}
	case AdvanceToolCallBatch:
		if state.ToolBatch != nil && state.ToolBatch.Index < len(state.ToolBatch.Calls) {
			state.ToolBatch.Index++
		}
	case ClearPendingTool:
		state.PendingTool = nil
	case ClearToolCallBatch:
		state.ToolBatch = nil
	case ClearPendingEffects:
		clearEffects()
	case ResetContext:
		state.Messages = systemMessages(state.Messages)
		state.PendingTool = nil
		state.ToolBatch = nil
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		clearEffects()
	default:
		panic(fmt.Sprintf("unknown mutation: %T", m))
	}
}

func systemMessages(messages []Message) []Message {
	var kept []Message
	for _, message := range messages {
		if message.Role == RoleSystem {
			kept = append(kept, message)
		}
	}
	return kept
}
