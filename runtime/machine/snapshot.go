package machine

import "fmt"

type queueView struct {
	hasBatch bool
	next     *ToolCall
}

func (q queueView) empty() bool { return q.hasBatch && q.next == nil }

type MachineSnapshot struct {
	state       State
	pendingTool *ToolCall
	currentTool *ToolCall
	queue       queueView
}

func SnapshotFrom(state EngineState) (MachineSnapshot, error) {
	if err := ValidateState(state); err != nil {
		return MachineSnapshot{}, err
	}
	snapshot := MachineSnapshot{
		state:       state.State,
		pendingTool: cloneToolCall(state.PendingTool),
		currentTool: cloneToolCall(state.CurrentTool),
		queue:       queueView{hasBatch: state.ToolBatch != nil},
	}
	if state.ToolBatch != nil && state.ToolBatch.Index < len(state.ToolBatch.Calls) {
		call := state.ToolBatch.Calls[state.ToolBatch.Index]
		snapshot.queue.next = &call
	}
	return snapshot, nil
}

func ValidateState(state EngineState) error {
	invalid := func(reason string) error { return InvariantViolationError{Reason: reason} }
	if state.ToolBatch != nil && (state.ToolBatch.Index < 0 || state.ToolBatch.Index > len(state.ToolBatch.Calls)) {
		return invalid("tool batch index is out of range")
	}
	if state.PendingTool == nil && state.PendingPermission != nil {
		return invalid("pending permission has no pending tool")
	}
	if state.StreamingContent != "" || state.StreamingReasoning != "" {
		if state.State != StateWaitingLLM {
			return invalid("streaming content exists outside WaitingLLM")
		}
	}

	switch state.State {
	case StateInitializing, StateIdle, StateWaitingLLM:
		if state.PendingTool != nil || state.PendingPermission != nil || state.CurrentTool != nil || state.ToolBatch != nil {
			return invalid(fmt.Sprintf("%s contains tool execution context", state.State))
		}
	case StateAdvancingQueue:
		if state.ToolBatch == nil {
			return invalid("AdvancingQueue has no tool batch")
		}
		if state.PendingTool != nil || state.PendingPermission != nil || state.CurrentTool != nil {
			return invalid("AdvancingQueue contains a pending or current tool")
		}
	case StateWaitingApproval:
		if state.ToolBatch == nil || state.PendingTool == nil || state.PendingPermission == nil {
			return invalid("WaitingApproval requires a batch, pending tool, and permission")
		}
		if state.CurrentTool != nil {
			return invalid("WaitingApproval contains a current tool")
		}
		if !batchPreviousCallMatches(state.ToolBatch, state.PendingTool) {
			return invalid("pending tool does not match the advanced batch call")
		}
	case StateRunningTool:
		if state.ToolBatch == nil || state.CurrentTool == nil {
			return invalid("RunningTool requires a batch and current tool")
		}
		if state.PendingTool != nil || state.PendingPermission != nil {
			return invalid("RunningTool contains pending approval context")
		}
		if !batchPreviousCallMatches(state.ToolBatch, state.CurrentTool) {
			return invalid("current tool does not match the advanced batch call")
		}
	default:
		return invalid("unknown state " + string(state.State))
	}
	return nil
}

func batchPreviousCallMatches(batch *ToolCallBatch, call *ToolCall) bool {
	return batch != nil && call != nil && batch.Index > 0 && batch.Index <= len(batch.Calls) && sameToolCall(batch.Calls[batch.Index-1], *call)
}

func sameToolCall(left, right ToolCall) bool {
	if left.ID != "" || right.ID != "" {
		return left.ID != "" && left.ID == right.ID
	}
	return left.Name == right.Name && left.Input == right.Input
}

func cloneToolCall(call *ToolCall) *ToolCall {
	if call == nil {
		return nil
	}
	cloned := *call
	return &cloned
}
