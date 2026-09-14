package session

import (
	"context"
	"errors"

	"super-agent/runtime/execution"
)

func (s *Session) RunTurn(ctx context.Context, query string, notifications chan<- SessionNotification, approvals <-chan ApprovalDecision) error {
	defer close(notifications)
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	s.persistTurnBoundary()
	// Track live state transitions while actions drain: states such as
	// RunningTool and AdvancingQueue pass between snapshot points, and the
	// TUI header should follow them as they happen.
	s.engine.SetStateObserver(func() { s.emitSnapshot(notifications) })
	defer s.engine.SetStateObserver(nil)
	onStreamChunk := func(chunk StreamChunk) {
		notifications <- StreamChunkReceived{Chunk: chunk, Message: s.Snapshot().StreamingMessage}
	}
	approvalWaiter := ApprovalWaitFunc(func(waitCtx context.Context, call ToolCall, _ PermissionRequest) (ApprovalDecision, error) {
		decision, err := waitApproval(waitCtx, approvals)
		if err != nil {
			return "", err
		}
		s.persistApproval(decision, call)
		s.emitter.markApprovalConsumed()
		return decision, nil
	})
	s.attachmentMu.Lock()
	attachments := append([]Attachment(nil), s.attachments...)
	s.attachments = nil
	s.attachmentMu.Unlock()
	err := s.engine.RunTurn(ctx, UserMessageSubmitted{Content: query, Attachments: attachments}, onStreamChunk, approvalWaiter)
	if err != nil {
		err = s.failTurn(notifications, err)
	}
	s.emitter.emit(notifications, s.Snapshot(), s.persistMessage)
	return err
}

func (s *Session) failTurn(notifications chan<- SessionNotification, err error) error {
	notifications <- SessionError{Err: err}
	s.persistError(err)
	return err
}

func waitApproval(ctx context.Context, approvals <-chan ApprovalDecision) (ApprovalDecision, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case decision, ok := <-approvals:
		if !ok {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// A closed channel means the interface gave up waiting; report
			// it as a dismissal so the engine cancels instead of failing.
			return "", execution.ErrApprovalDismissed
		}
		return decision, nil
	}
}
