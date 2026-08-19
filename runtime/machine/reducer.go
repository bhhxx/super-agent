package machine

import "fmt"

type EffectOp interface {
	isEffectOp()
}

type ClearPendingEffectsOp struct{}

func (ClearPendingEffectsOp) isEffectOp() {}

type Reduction struct {
	State     EngineState
	EffectOps []EffectOp
}

type Reducer interface {
	Reduce(state EngineState, result TransitionResult) (Reduction, error)
}

type DefaultReducer struct{}

func (DefaultReducer) Reduce(state EngineState, result TransitionResult) (Reduction, error) {
	next := cloneEngineState(state)
	next.State = result.NextState
	reduction := Reduction{State: next}
	for _, mutation := range result.Mutations {
		if err := applyMutation(&reduction, mutation); err != nil {
			return Reduction{}, err
		}
	}
	if err := ValidateState(reduction.State); err != nil {
		return Reduction{}, err
	}
	return reduction, nil
}

func applyMutation(reduction *Reduction, mutation Mutation) error {
	state := &reduction.State
	switch m := mutation.(type) {
	case AppendUserMessage:
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		state.Messages = append(state.Messages, Message{Role: RoleUser, Content: m.Content})
	case AppendAssistantMessage:
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		state.Messages = append(state.Messages, cloneMessage(m.Message))
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
		request := clonePermissionRequest(m.Request)
		state.PendingTool = &call
		state.PendingPermission = &request
	case SetCurrentTool:
		call := m.Call
		state.CurrentTool = &call
	case SetToolCallBatch:
		state.ToolBatch = &ToolCallBatch{ID: m.ID, Calls: append([]ToolCall(nil), m.Calls...)}
	case AdvanceToolCallBatch:
		if state.ToolBatch == nil || state.ToolBatch.Index >= len(state.ToolBatch.Calls) {
			return InvariantViolationError{Reason: "cannot advance an empty tool batch"}
		}
		state.ToolBatch.Index++
	case ClearPendingTool:
		state.PendingTool = nil
		state.PendingPermission = nil
	case ClearCurrentTool:
		state.CurrentTool = nil
	case ClearToolCallBatch:
		state.ToolBatch = nil
	case ClearPendingEffects:
		reduction.EffectOps = append(reduction.EffectOps, ClearPendingEffectsOp{})
	case ResetContext:
		state.Messages = systemMessages(state.Messages)
		state.PendingTool = nil
		state.PendingPermission = nil
		state.CurrentTool = nil
		state.ToolBatch = nil
		state.StreamingContent = ""
		state.StreamingReasoning = ""
		reduction.EffectOps = append(reduction.EffectOps, ClearPendingEffectsOp{})
	default:
		return InvariantViolationError{Reason: fmt.Sprintf("unknown mutation %T", m)}
	}
	return nil
}

func cloneEngineState(state EngineState) EngineState {
	cloned := state
	cloned.Messages = make([]Message, len(state.Messages))
	for i, message := range state.Messages {
		cloned.Messages[i] = cloneMessage(message)
	}
	cloned.PendingTool = cloneToolCall(state.PendingTool)
	if state.PendingPermission != nil {
		request := clonePermissionRequest(*state.PendingPermission)
		cloned.PendingPermission = &request
	}
	cloned.CurrentTool = cloneToolCall(state.CurrentTool)
	if state.ToolBatch != nil {
		cloned.ToolBatch = &ToolCallBatch{
			ID:    state.ToolBatch.ID,
			Calls: append([]ToolCall(nil), state.ToolBatch.Calls...),
			Index: state.ToolBatch.Index,
		}
	}
	return cloned
}

func cloneMessage(message Message) Message {
	cloned := message
	if message.ToolCalls != nil {
		cloned.ToolCalls = make([]*ToolCall, len(message.ToolCalls))
		for i, call := range message.ToolCalls {
			cloned.ToolCalls[i] = cloneToolCall(call)
		}
	}
	return cloned
}

func clonePermissionRequest(request PermissionRequest) PermissionRequest {
	request.TouchedPaths = append([]string(nil), request.TouchedPaths...)
	request.EnvVars = append([]string(nil), request.EnvVars...)
	return request
}

func systemMessages(messages []Message) []Message {
	var kept []Message
	for _, message := range messages {
		if message.Role == RoleSystem {
			kept = append(kept, cloneMessage(message))
		}
	}
	return kept
}
