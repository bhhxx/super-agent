package runtime

import (
	"context"
	"errors"
)

type ConfirmationAction string

const (
	ConfirmOnce   ConfirmationAction = "once"
	ConfirmAlways ConfirmationAction = "always"
	ConfirmDeny   ConfirmationAction = "deny"
)

type SessionEvent interface {
	isSessionEvent()
}

type StateChanged struct {
	State    State
	ToolCall *ToolCall
}

func (StateChanged) isSessionEvent() {}

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
}

type Session struct {
	engine *Engine
}

func NewSession(engine *Engine) *Session {
	return &Session{engine: engine}
}

func (s *Session) Run(ctx context.Context, query string, events chan<- SessionEvent, approvals <-chan ConfirmationAction) error {
	defer close(events)
	err := s.drainRun(ctx, events, approvals, query)
	s.emitSnapshot(events)
	return err
}

func (s *Session) Cancel() error {
	return s.engine.Cancel()
}

func (s *Session) Reset() {
	s.engine.Reset()
}

func (s *Session) Snapshot() Snapshot {
	snapshot := Snapshot{
		State:    s.engine.State(),
		Messages: s.engine.Messages(),
	}
	if call, ok := s.engine.PendingTool(); ok {
		snapshot.PendingTool = &call
	}
	return snapshot
}

func (s *Session) emitSnapshot(events chan<- SessionEvent) {
	snapshot := s.Snapshot()
	events <- StateChanged{State: snapshot.State, ToolCall: snapshot.PendingTool}
	messages := snapshot.Messages
	if len(messages) > 0 {
		msg := messages[len(messages)-1]
		events <- MessageAppended{Message: msg}
	}
}

func (s *Session) drainRun(ctx context.Context, events chan<- SessionEvent, approvals <-chan ConfirmationAction, query string) error {
	chunkFunc := func(chunk StreamChunk) {
		events <- StreamChunkReceived{Chunk: chunk}
	}
	if err := s.engine.runEventWithBeforeEffects(ctx, UserMessageSubmitted{Content: query}, chunkFunc, func() {
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

func waitApproval(ctx context.Context, approvals <-chan ConfirmationAction) (ConfirmationAction, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case action := <-approvals:
		return action, nil
	}
}

func (s *Session) applyApproval(ctx context.Context, action ConfirmationAction, chunkFunc func(StreamChunk)) error {
	switch action {
	case ConfirmOnce:
		return s.engine.Approve(ctx, chunkFunc)
	case ConfirmAlways:
		return s.engine.ApproveAlways(ctx, chunkFunc)
	case ConfirmDeny:
		return s.engine.Deny(ctx, chunkFunc)
	default:
		return errors.New("unknown confirmation action")
	}
}
