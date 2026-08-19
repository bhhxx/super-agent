package session

import (
	"context"
	"errors"
)

func (s *Session) RunTurn(ctx context.Context, query string, events chan<- SessionEvent, approvals <-chan ApprovalDecision) error {
	defer close(events)
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	s.startTurn()
	// Track live state transitions while effects drain: states such as
	// RunningTool and AdvancingQueue pass between snapshot points, and the
	// TUI header should follow them as they happen.
	s.engine.SetStateObserver(func() { s.emitSnapshot(events) })
	defer s.engine.SetStateObserver(nil)
	err := s.drainRun(ctx, events, approvals, query)
	s.emitter.emit(events, s.Snapshot(), s.persistMessage)
	return err
}

func (s *Session) drainRun(ctx context.Context, events chan<- SessionEvent, approvals <-chan ApprovalDecision, query string) error {
	chunks := func(chunk StreamChunk) {
		events <- StreamChunkReceived{Chunk: chunk, Message: s.Snapshot().StreamingMessage}
	}
	if err := s.engine.DispatchEventThenRunEffects(ctx, UserMessageSubmitted{Content: query}, chunks, func() { s.emitSnapshot(events) }); err != nil {
		return s.failTurn(events, err)
	}
	s.emitSnapshot(events)
	for {
		switch s.engine.State() {
		case StateWaitingApproval:
			s.emitSnapshot(events)
			decision, err := waitApproval(ctx, approvals)
			if err != nil {
				_ = s.engine.Cancel()
				return s.failTurn(events, err)
			}
			// Mark the approval consumed before applying the decision: the
			// engine emits snapshots while effects drain, so the next
			// pending tool is announced from inside applyApproval. Consuming
			// afterwards would clear the dedup key and re-announce it.
			s.emitter.markApprovalConsumed()
			if err := s.applyApproval(ctx, decision, chunks); err != nil {
				return s.failTurn(events, err)
			}
			s.emitSnapshot(events)
		case StateIdle:
			return nil
		default:
			return s.failTurn(events, errors.New("runtime cannot continue from state "+string(s.engine.State())))
		}
	}
}

func (s *Session) failTurn(events chan<- SessionEvent, err error) error {
	events <- SessionError{Err: err}
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
			return "", errors.New("approval channel closed")
		}
		return decision, nil
	}
}

func (s *Session) applyApproval(ctx context.Context, decision ApprovalDecision, chunks func(StreamChunk)) error {
	s.persistApproval(decision)
	switch decision {
	case ApproveOnce:
		return s.engine.Approve(ctx, chunks)
	case ApproveAlways:
		return s.engine.ApproveAlways(ctx, chunks)
	case DenyApproval:
		return s.engine.Deny(ctx, chunks)
	default:
		return errors.New("unknown approval decision")
	}
}
