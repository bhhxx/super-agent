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
}

type Session struct {
	engine *Engine
}

func NewSession(engine *Engine) *Session {
	return &Session{engine: engine}
}

func (s *Session) Run(ctx context.Context, query string, events chan<- SessionEvent, approvals <-chan ConfirmationAction) error {
	defer close(events)
	emittedMessages := 0
	err := s.drainRun(ctx, events, approvals, query, &emittedMessages)
	s.emitSnapshot(events, &emittedMessages)
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

func (s *Session) emitSnapshot(events chan<- SessionEvent, emittedMessages *int) {
	snapshot := s.Snapshot()
	events <- StateChanged{State: snapshot.State}
	if snapshot.PendingTool != nil {
		events <- ToolApprovalRequested{ToolCall: *snapshot.PendingTool}
	}
	messages := snapshot.Messages
	if emittedMessages == nil {
		return
	}
	if *emittedMessages > len(messages) {
		*emittedMessages = 0
	}
	for _, msg := range messages[*emittedMessages:] {
		events <- MessageAppended{Message: msg}
	}
	*emittedMessages = len(messages)
}

func (s *Session) drainRun(ctx context.Context, events chan<- SessionEvent, approvals <-chan ConfirmationAction, query string, emittedMessages *int) error {
	chunkFunc := func(chunk StreamChunk) {
		events <- StreamChunkReceived{Chunk: chunk}
	}
	if err := s.engine.runEventWithBeforeEffects(ctx, UserMessageSubmitted{Content: query}, chunkFunc, func() {
		s.emitSnapshot(events, emittedMessages)
	}); err != nil {
		events <- SessionError{Err: err}
		return err
	}
	s.emitSnapshot(events, emittedMessages)
	for {
		switch s.engine.State() {
		case StateWaitingApproval:
			s.emitSnapshot(events, emittedMessages)
			action, err := waitApproval(ctx, approvals)
			if err != nil {
				events <- SessionError{Err: err}
				return err
			}
			if err := s.applyApproval(ctx, action, chunkFunc); err != nil {
				events <- SessionError{Err: err}
				return err
			}
			s.emitSnapshot(events, emittedMessages)
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
