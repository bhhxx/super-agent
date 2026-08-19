package engine

import (
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
)

func (e *Engine) State() machine.State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state.State
}

func (e *Engine) Messages() []protocol.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]protocol.Message(nil), e.state.Messages...)
}

func (e *Engine) PendingTool() (protocol.ToolCall, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state.PendingTool == nil {
		return protocol.ToolCall{}, false
	}
	return *e.state.PendingTool, true
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot := Snapshot{State: e.state.State, Messages: append([]protocol.Message(nil), e.state.Messages...), IsBusy: e.state.State == machine.StateWaitingLLM || e.state.State == machine.StateRunningTool || e.state.State == machine.StateAdvancingQueue, NeedsInput: e.state.State == machine.StateWaitingApproval}
	if e.state.PendingTool != nil {
		call := *e.state.PendingTool
		snapshot.PendingTool = &call
		if e.state.PendingPermission != nil {
			request := *e.state.PendingPermission
			snapshot.PendingPermission = &request
		}
		if e.state.ToolBatch != nil {
			snapshot.PendingToolBatchID = e.state.ToolBatch.ID
			snapshot.PendingToolBatchIndex = e.state.ToolBatch.Index
			snapshot.PendingToolBatchTotal = len(e.state.ToolBatch.Calls)
		}
	}
	if e.state.StreamingContent != "" || e.state.StreamingReasoning != "" {
		snapshot.StreamingMessage = &protocol.Message{Role: protocol.RoleAssistant, Content: e.state.StreamingContent, ReasoningContent: e.state.StreamingReasoning}
	}
	return snapshot
}
