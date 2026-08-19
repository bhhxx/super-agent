package engine

import (
	"context"
	"errors"
	"strings"

	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
)

func (e *Engine) EnableAutoApproveTools() { e.approvals.SetAutoApproveTools(true) }

func (e *Engine) Ready() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dispatchLocked(machine.EngineReady{})
}

func (e *Engine) Approve(ctx context.Context, chunks func(protocol.StreamChunk)) error {
	return e.resolveApproval(ctx, machine.ApprovalGranted{}, chunks)
}

func (e *Engine) Deny(ctx context.Context, chunks func(protocol.StreamChunk)) error {
	return e.resolveApproval(ctx, machine.ApprovalDenied{}, chunks)
}

func (e *Engine) ApproveAlways(ctx context.Context, chunks func(protocol.StreamChunk)) error {
	e.mu.Lock()
	call, err := e.pendingApprovalCallLocked()
	if err == nil {
		err = e.dispatchLocked(machine.ApprovalAlwaysGranted{Call: call})
	}
	e.mu.Unlock()
	if err != nil {
		return err
	}
	e.approvals.AllowAlways(execution.NewApprovalKey(call))
	// The transition moved to StateRunningTool before the tool executes;
	// notify so observers see it during the run.
	e.notifyStateObserver()
	return e.continueRun(chunks)
}

func (e *Engine) resolveApproval(_ context.Context, decision machine.Event, chunks func(protocol.StreamChunk)) error {
	e.mu.Lock()
	call, err := e.pendingApprovalCallLocked()
	if err == nil {
		switch decision.(type) {
		case machine.ApprovalGranted:
			decision = machine.ApprovalGranted{Call: call}
		case machine.ApprovalDenied:
			decision = machine.ApprovalDenied{Call: call}
		}
		err = e.dispatchLocked(decision)
	}
	e.mu.Unlock()
	if err != nil {
		return err
	}
	// The transition moved to StateRunningTool before the tool executes;
	// notify so observers see it during the run.
	e.notifyStateObserver()
	return e.continueRun(chunks)
}

func (e *Engine) pendingApprovalCallLocked() (protocol.ToolCall, error) {
	if e.state.State != machine.StateWaitingApproval || e.state.PendingTool == nil {
		return protocol.ToolCall{}, errors.New("no tool is waiting for approval")
	}
	return *e.state.PendingTool, nil
}

func (e *Engine) continueRun(chunks func(protocol.StreamChunk)) error {
	runCtx, ok := e.runs.CurrentContext()
	if !ok {
		return errors.New("no active run context")
	}
	return e.runPendingEffects(runCtx, chunks)
}

func (e *Engine) Cancel() error { e.runs.CancelRun(); return e.dispatch(machine.CancelRequested{}) }
func (e *Engine) Reset() error {
	e.runs.CancelRun()
	e.runs.StartNewGeneration()
	return e.dispatch(machine.ResetRequested{})
}

func (e *Engine) ReplaceMessages(messages []protocol.Message) {
	e.runs.CancelRun()
	e.runs.StartNewGeneration()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state.Messages = append([]protocol.Message(nil), messages...)
	e.state.PendingTool = nil
	e.state.PendingPermission = nil
	e.state.CurrentTool = nil
	e.state.ToolBatch = nil
	e.state.StreamingContent = ""
	e.state.StreamingReasoning = ""
	e.state.State = machine.StateIdle
	e.scheduler.Clear()
}

func (e *Engine) SetPermissionPolicy(mode execution.PermissionMode, rules execution.PermissionRules) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !execution.ValidPermissionMode(mode) {
		return errors.New("invalid permission mode: " + string(mode))
	}
	setter, ok := e.resolver.(policySetter)
	if !ok {
		return errors.New("outcome resolver does not support policy updates")
	}
	setter.SetPolicy(execution.NewPolicy(mode, rules))
	if store, ok := e.approvals.(policyStore); ok {
		store.SetPermissionPolicy(mode, rules)
	}
	return nil
}

func (e *Engine) CompactSummary(ctx context.Context) (string, error) {
	messages := e.Messages()
	if len(messages) == 0 {
		return "", nil
	}
	prompt := protocol.Message{Role: protocol.RoleUser, Content: "Summarize this conversation for context compaction. Preserve goals, decisions, files changed, tool results, and unresolved next steps."}
	outcome, err := e.runner.Run(ctx, execution.QueuedEffect{Effect: machine.CallModel{}}, execution.ExecutionInput{Messages: append(messages, prompt)}, nil)
	if err != nil {
		return "", err
	}
	reply, ok := outcome.Result.(execution.ModelReplied)
	if !ok {
		return "", errors.New("compact summary did not return a model response")
	}
	summary := strings.TrimSpace(reply.Response.Content)
	if summary == "" {
		summary = strings.TrimSpace(reply.Response.ReasoningContent)
	}
	if summary == "" {
		return "", errors.New("compact summary is empty")
	}
	return summary, nil
}
