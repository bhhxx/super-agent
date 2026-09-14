package engine

import (
	"context"
	"errors"
	"strings"

	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
)

func (e *Engine) Ready() error {
	return e.DispatchEvent(context.Background(), machine.EngineReady{}, nil)
}

func (e *Engine) Cancel() error {
	e.runs.CancelRun()
	return e.DispatchEvent(context.Background(), machine.CancelRequested{}, nil)
}
func (e *Engine) Reset() error {
	e.runs.CancelRun()
	e.runs.InvalidateCurrentRun()
	return e.DispatchEvent(context.Background(), machine.ResetRequested{}, nil)
}

func (e *Engine) ReplaceMessages(messages []protocol.Message) {
	e.runs.CancelRun()
	e.runs.InvalidateCurrentRun()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runtimeData.Messages = append([]protocol.Message(nil), messages...)
	e.runtimeData.PendingTool = nil
	e.runtimeData.PendingPermission = nil
	e.runtimeData.CurrentTool = nil
	e.runtimeData.ToolBatch = nil
	e.runtimeData.StreamingContent = ""
	e.runtimeData.StreamingReasoning = ""
	e.runtimeData.State = machine.StateIdle
	e.actionQueue.Clear()
}

func (e *Engine) SetPermissionPolicy(mode execution.PermissionMode, rules execution.PermissionRules) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !execution.ValidPermissionMode(mode) {
		return errors.New("invalid permission mode: " + string(mode))
	}
	setter, ok := e.resolver.(policySetter)
	if !ok {
		return errors.New("action result resolver does not support policy updates")
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
	completion, err := e.runner.Run(ctx, execution.QueuedAction{Action: machine.CallModel{}}, execution.ScheduledActionInput{Messages: append(messages, prompt)}, nil)
	if err != nil {
		return "", err
	}
	reply, ok := completion.Result.(execution.ModelReplied)
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
