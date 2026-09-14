package engine

import (
	"context"
	"errors"
	"reflect"
	"time"

	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
	"super-agent/runtime/telemetry"
)

func (e *Engine) DispatchEvent(ctx context.Context, event machine.Event, onStreamChunk func(protocol.StreamChunk)) error {
	if _, startsTurn := event.(machine.UserMessageSubmitted); startsTurn {
		return errors.New("user messages must be submitted through Engine.RunTurn")
	}
	_, err := e.dispatchEvent(ctx, event, onStreamChunk, nil)
	return err
}

func (e *Engine) RunTurn(ctx context.Context, event machine.UserMessageSubmitted, onStreamChunk func(protocol.StreamChunk), approvalWaiter execution.ApprovalWaiter) error {
	started := time.Now()
	// dispatchEvent returns the run ID captured right after the run started,
	// so the record names this turn even when cancel paths later bump the ID.
	runID, err := e.dispatchEvent(ctx, event, onStreamChunk, approvalWaiter)
	telemetry.Record("run", telemetry.Fields{"run_id": string(runID), "duration_ms": time.Since(started).Milliseconds(), "error": errorString(err)})
	return err
}

func (e *Engine) dispatchEvent(ctx context.Context, event machine.Event, onStreamChunk func(protocol.StreamChunk), approvalWaiter execution.ApprovalWaiter) (execution.RunID, error) {
	e.mu.Lock()
	decision, err := e.calculateTransitionLocked(event)
	if err != nil {
		e.mu.Unlock()
		return "", err
	}
	runCtx := ctx
	startedRun := false
	if _, startsRun := event.(machine.UserMessageSubmitted); startsRun {
		_, runCtx = e.runs.StartRun(ctx)
		startedRun = true
	} else if len(decision.ActionPlan.Schedule) > 0 {
		currentCtx, ok := e.runs.CurrentContext()
		if !ok {
			e.mu.Unlock()
			return "", errors.New("event scheduled actions without an active run")
		}
		runCtx = currentCtx
	}
	if err := e.commitTransitionLocked(decision); err != nil { // commit the transition's data changes and action plan atomically
		if startedRun {
			e.runs.CancelRun()
		}
		e.mu.Unlock()
		return "", err
	}
	state := e.runtimeData.State
	runID := e.runs.CurrentRunID()
	e.mu.Unlock()
	telemetry.Record("transition", telemetry.Fields{"run_id": string(runID), "event": typeName(event), "state": string(state), "scheduled_actions": len(decision.ActionPlan.Schedule)})
	e.notifyStateObserver()
	return runID, e.runScheduledActions(runCtx, onStreamChunk, approvalWaiter)
}

func (e *Engine) calculateTransitionLocked(event machine.Event) (machine.TransitionResult, error) {
	snapshot, err := machine.SnapshotFrom(e.runtimeData)
	if err != nil {
		return machine.TransitionResult{}, err
	}
	return machine.Transition(snapshot, event)
}

func (e *Engine) commitTransitionLocked(decision machine.TransitionResult) error {
	changeResult, err := e.runtimeDataChangeApplier.ApplyRuntimeDataChanges(e.runtimeData, decision)
	if err != nil {
		return err
	}
	if err := machine.ValidateRuntimeData(changeResult.RuntimeData); err != nil {
		return err
	}
	e.runtimeData = changeResult.RuntimeData
	if decision.ActionPlan.ClearExisting {
		e.actionQueue.Clear()
	}
	for _, action := range decision.ActionPlan.Schedule {
		e.actionQueue.Queue(e.runs.CurrentRunID(), action)
	}
	return nil
}

func (e *Engine) runScheduledActions(ctx context.Context, onStreamChunk func(protocol.StreamChunk), approvalWaiter execution.ApprovalWaiter) error {
	runID := e.runs.CurrentRunID()
	for {
		e.mu.Lock()
		action, ok := e.actionQueue.Pop()
		if !ok {
			if e.runtimeData.State == machine.StateIdle {
				e.runs.FinishRun(runID)
				e.mu.Unlock()
				return nil
			}
			state := e.runtimeData.State
			e.mu.Unlock()
			return machine.InvariantViolationError{Reason: "action queue is empty in state " + string(state)}
		}
		e.mu.Unlock()
		if err := e.executeScheduledAction(ctx, action, onStreamChunk, approvalWaiter); err != nil {
			// Cancellation is decided by error identity, not by action type:
			// a dropped approval or a cancelled context cancels the run,
			// while a misconfigured waiter or any other fault takes the
			// error path, which answers the outstanding tool calls.
			if errors.Is(err, context.Canceled) || errors.Is(err, execution.ErrApprovalDismissed) {
				e.runs.CancelRun()
				_ = e.DispatchEvent(ctx, machine.CancelRequested{}, nil)
			} else {
				_ = e.DispatchEvent(ctx, machine.ErrorOccurred{Err: err}, nil)
			}
			return err
		}
		// The action may have committed a transition; notify so
		// observers see states that pass between snapshot points, such as
		// RunningTool while a tool executes.
		e.notifyStateObserver()
	}
}

func (e *Engine) executeScheduledAction(ctx context.Context, action execution.QueuedAction, onStreamChunk func(protocol.StreamChunk), approvalWaiter execution.ApprovalWaiter) error {
	started := time.Now()
	stream := onStreamChunk
	if onStreamChunk != nil {
		stream = func(chunk protocol.StreamChunk) { e.recordStreamChunk(action.RunID, chunk); onStreamChunk(chunk) }
	}
	env := execution.ScheduledActionInput{Messages: e.Messages(), ToolSpecs: e.toolSpecs(), ApprovalWaiter: approvalWaiter}
	actionCtx := telemetry.WithIDs(ctx, string(action.RunID), string(action.ActionID))
	completion, err := e.runner.Run(actionCtx, action, env, stream)
	if err != nil {
		telemetry.Record("action", telemetry.Fields{"run_id": string(action.RunID), "action_id": string(action.ActionID), "action": typeName(action.Action), "duration_ms": time.Since(started).Milliseconds(), "error": err.Error()})
		return err
	}
	fields := telemetry.Fields{"run_id": string(action.RunID), "action_id": string(action.ActionID), "action": typeName(action.Action), "duration_ms": time.Since(started).Milliseconds()}
	switch typed := action.Action.(type) {
	case machine.CallModel:
		fields["component"] = "model"
	case machine.RunTool:
		fields["component"] = "tool"
		fields["tool"] = typed.Call.Name
	case machine.AwaitApproval:
		fields["component"] = "approval"
	}
	if reply, ok := completion.Result.(execution.ModelReplied); ok {
		fields["input_tokens_estimate"] = estimateMessageTokens(env.Messages)
		fields["output_tokens_estimate"] = estimateTokens(reply.Response.Content + reply.Response.ReasoningContent)
		// Exact provider counts win over the rune-count estimates when the
		// adapter could obtain them.
		if reply.Response.Usage != nil {
			fields["input_tokens"] = reply.Response.Usage.InputTokens
			fields["output_tokens"] = reply.Response.Usage.OutputTokens
			fields["total_tokens"] = reply.Response.Usage.TotalTokens
		}
	}
	telemetry.Record("action", fields)
	if !e.runs.IsCurrent(completion.RunID) {
		return nil
	}
	// Specs are fetched outside the engine lock on purpose: a registry
	// change (an MCP reconnect, say) can block Specs for seconds, and the
	// lock must stay free for queries and dispatch. The cost is that this
	// one resolution may classify against a spec set that just changed —
	// benign, since the next action re-fetches.
	toolSpecs := e.toolSpecs()
	e.mu.Lock()
	batch := cloneToolBatch(e.runtimeData.ToolBatch)
	event, err := e.resolver.Resolve(completion.Result, execution.ActionResultInput{ToolBatch: batch, ToolSpecs: toolSpecs})
	if err != nil {
		// runScheduledActions dispatches ErrorOccurred once for the returned
		// error; dispatching here too would append the runtime-error tool
		// message twice.
		e.mu.Unlock()
		return err
	}
	decision, err := e.calculateTransitionLocked(event)
	if err == nil {
		err = e.commitTransitionLocked(decision)
	}
	e.mu.Unlock()
	if err == nil {
		if approved, ok := event.(machine.ApprovalAlwaysGranted); ok {
			e.approvals.AllowAlways(execution.NewApprovalKey(approved.Call))
		}
	}
	return err
}

func typeName(value any) string {
	typeOf := reflect.TypeOf(value)
	if typeOf == nil {
		return "nil"
	}
	return typeOf.Name()
}

func estimateMessageTokens(messages []protocol.Message) int {
	total := 0
	for _, message := range messages {
		total += estimateTokens(message.Content + message.ReasoningContent)
	}
	return total
}

func estimateTokens(value string) int {
	count := len([]rune(value))
	if count == 0 {
		return 0
	}
	return (count + 3) / 4
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func cloneToolBatch(batch *machine.ToolCallBatch) *machine.ToolCallBatch {
	if batch == nil {
		return nil
	}
	return &machine.ToolCallBatch{ID: batch.ID, Calls: append([]protocol.ToolCall(nil), batch.Calls...), Index: batch.Index}
}

func (e *Engine) toolSpecs() []protocol.ToolSpec { return e.runner.ToolSpecs() }

func (e *Engine) recordStreamChunk(runID execution.RunID, chunk protocol.StreamChunk) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Re-check staleness under the lock: a reset that lands between the
	// stream callback and this commit must not append into a cleared
	// conversation.
	if !e.runs.IsCurrent(runID) {
		return
	}
	if err := e.commitTransitionLocked(machine.TransitionResult{
		NextState:          e.runtimeData.State,
		RuntimeDataChanges: []machine.RuntimeDataChange{machine.AppendStreamingAssistant{Chunk: chunk}},
	}); err != nil {
		telemetry.Record("stream_chunk_rejected", telemetry.Fields{"run_id": string(runID), "error": err.Error()})
	}
}
