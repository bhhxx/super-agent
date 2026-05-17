package runtime

import (
	"context"
	"errors"
)

type ApprovalDecision string

const (
	ApproveOnce   ApprovalDecision = "once"
	ApproveAlways ApprovalDecision = "always"
	DenyApproval  ApprovalDecision = "deny"
)

type SessionEvent interface {
	isSessionEvent()
}

type StateChanged struct {
	State State
}

func (StateChanged) isSessionEvent() {}

type ToolApprovalRequested struct {
	ToolCall ToolCall
}

func (ToolApprovalRequested) isSessionEvent() {}

type StreamChunkReceived struct {
	Chunk StreamChunk
}

func (StreamChunkReceived) isSessionEvent() {}

type MessageAppended struct {
	Message Message
}

func (MessageAppended) isSessionEvent() {}

type SessionError struct {
	Err error
}

func (SessionError) isSessionEvent() {}

type Snapshot struct {
	State       State
	Messages    []Message
	PendingTool *ToolCall
	IsBusy      bool
	NeedsInput  bool
}

type Session struct {
	engine  *Engine
	emitter *snapshotEmitter
}

func NewSession(engine *Engine) *Session {
	return &Session{engine: engine, emitter: newSnapshotEmitter()}
}

func (s *Session) Run(ctx context.Context, query string, events chan<- SessionEvent, approvals <-chan ApprovalDecision) error {
	defer close(events)
	err := s.drainRun(ctx, events, approvals, query)
	s.emitter.emit(events, s.Snapshot())
	return err
}

func (s *Session) Cancel() error {
	return s.engine.Cancel()
}

func (s *Session) Reset() {
	s.engine.Reset()
}

func (s *Session) Snapshot() Snapshot {
	return s.engine.snapshot()
}

func (s *Session) emitSnapshot(events chan<- SessionEvent) {
	s.emitter.emit(events, s.Snapshot())
}

func (s *Session) drainRun(ctx context.Context, events chan<- SessionEvent, approvals <-chan ApprovalDecision, query string) error {
	chunkFunc := func(chunk StreamChunk) {
		events <- StreamChunkReceived{Chunk: chunk}
	}
	if err := s.engine.dispatchEventThenRunEffects(ctx, UserMessageSubmitted{Content: query}, chunkFunc, func() {
		s.emitSnapshot(events)
	}); err != nil {
		events <- SessionError{Err: err}
		return err
	}
	s.emitSnapshot(events)
	for {
		switch s.engine.State() {
		case StateWaitingApproval:
			s.emitSnapshot(events)
			action, err := waitApproval(ctx, approvals)
			if err != nil {
				events <- SessionError{Err: err}
				return err
			}
			if err := s.applyApproval(ctx, action, chunkFunc); err != nil {
				events <- SessionError{Err: err}
				return err
			}
			s.emitSnapshot(events)
		case StateIdle:
			return nil
		default:
			return errors.New("runtime cannot continue from state " + string(s.engine.State()))
		}
	}
}

func waitApproval(ctx context.Context, approvals <-chan ApprovalDecision) (ApprovalDecision, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case action := <-approvals:
		return action, nil
	}
}

func (s *Session) applyApproval(ctx context.Context, action ApprovalDecision, chunkFunc func(StreamChunk)) error {
	switch action {
	case ApproveOnce:
		return s.engine.Approve(ctx, chunkFunc)
	case ApproveAlways:
		return s.engine.ApproveAlways(ctx, chunkFunc)
	case DenyApproval:
		return s.engine.Deny(ctx, chunkFunc)
	default:
		return errors.New("unknown approval decision")
	}
}
