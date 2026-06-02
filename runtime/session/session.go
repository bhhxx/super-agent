package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"super-agent/runtime/store"
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
	ToolCall   ToolCall
	Request    PermissionRequest
	BatchID    string
	BatchIndex int
	BatchTotal int
}

func (ToolApprovalRequested) isSessionEvent() {}

type ToolApprovalCleared struct{}

func (ToolApprovalCleared) isSessionEvent() {}

type StreamChunkReceived struct {
	Chunk   StreamChunk
	Message *Message
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

type Session struct {
	engine          *Engine
	emitter         *snapshotEmitter
	store           *store.Store
	meta            store.Metadata
	permissionMode  PermissionMode
	permissionRules PermissionRules
	mu              sync.Mutex
}

func NewSession(engine *Engine) *Session {
	return &Session{engine: engine, emitter: newSnapshotEmitter()}
}

func NewPersistentSession(engine *Engine, st *store.Store, meta store.Metadata) *Session {
	emitter := newSnapshotEmitter()
	emitter.emittedMessages = len(engine.Snapshot().Messages)
	return &Session{engine: engine, emitter: emitter, store: st, meta: meta}
}

func (s *Session) Metadata() store.Metadata {
	return s.meta
}

func (s *Session) ConfigurePermissions(mode PermissionMode, rules PermissionRules) {
	s.permissionMode = mode
	s.permissionRules = rules
}

func (s *Session) SetPermissionMode(mode PermissionMode) error {
	if mode == "" {
		mode = PermissionModeAsk
	}
	if err := s.engine.SetPermissionPolicy(mode, s.permissionRules); err != nil {
		return err
	}
	s.permissionMode = mode
	return nil
}

func (s *Session) RunTurn(ctx context.Context, query string, events chan<- SessionEvent, approvals <-chan ApprovalDecision) error {
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	defer close(events)
	s.startTurn()
	err := s.drainRun(ctx, events, approvals, query)
	s.emitter.emit(events, s.Snapshot(), s.persistMessage)
	return err
}

func (s *Session) Cancel() error {
	err := s.engine.Cancel()
	if s.store != nil {
		_ = s.store.Append(s.meta.ID, store.Record{Type: store.EventCancel})
	}
	return err
}

func (s *Session) Reset() error {
	err := s.engine.Reset()
	if err == nil && s.store != nil {
		s.emitter = newSnapshotEmitter()
		s.emitter.emittedMessages = len(s.engine.Snapshot().Messages)
		_ = s.store.Append(s.meta.ID, store.Record{Type: store.EventReset})
	}
	return err
}

func (s *Session) Snapshot() Snapshot {
	return s.engine.Snapshot()
}

func (s *Session) emitSnapshot(events chan<- SessionEvent) {
	s.emitter.emit(events, s.Snapshot(), s.persistMessage)
}

func (s *Session) drainRun(ctx context.Context, events chan<- SessionEvent, approvals <-chan ApprovalDecision, query string) error {
	chunkFunc := func(chunk StreamChunk) {
		snapshot := s.Snapshot()
		events <- StreamChunkReceived{Chunk: chunk, Message: snapshot.StreamingMessage}
	}
	if err := s.engine.DispatchEventThenRunEffects(ctx, UserMessageSubmitted{Content: query}, chunkFunc, func() {
		s.emitSnapshot(events)
	}); err != nil {
		events <- SessionError{Err: err}
		s.persistError(err)
		return err
	}
	s.emitSnapshot(events)
	for {
		switch s.engine.State() {
		case StateWaitingApproval:
			s.emitSnapshot(events)
			action, err := waitApproval(ctx, approvals)
			if err != nil {
				_ = s.engine.Cancel()
				events <- SessionError{Err: err}
				s.persistError(err)
				return err
			}
			if err := s.applyApproval(ctx, action, chunkFunc); err != nil {
				events <- SessionError{Err: err}
				s.persistError(err)
				return err
			}
			s.emitSnapshot(events)
		case StateIdle:
			return nil
		default:
			err := errors.New("runtime cannot continue from state " + string(s.engine.State()))
			s.persistError(err)
			return err
		}
	}
}

func waitApproval(ctx context.Context, approvals <-chan ApprovalDecision) (ApprovalDecision, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case action, ok := <-approvals:
		if !ok {
			return "", errors.New("approval channel closed")
		}
		return action, nil
	}
}

func (s *Session) applyApproval(ctx context.Context, action ApprovalDecision, chunkFunc func(StreamChunk)) error {
	s.persistApproval(action)
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

func (s *Session) Sessions() ([]store.Summary, error) {
	if s.store == nil {
		return nil, errors.New("session store is not configured")
	}
	return s.store.List()
}

func (s *Session) Resume(id store.SessionID) error {
	if s.store == nil {
		return errors.New("session store is not configured")
	}
	messages, err := s.store.Messages(id)
	if err != nil {
		return err
	}
	meta, err := s.store.Metadata(id)
	if err != nil {
		return err
	}
	s.engine.ReplaceMessages(messages)
	s.meta = meta
	s.emitter = newSnapshotEmitter()
	s.emitter.emittedMessages = len(messages)
	return nil
}

func (s *Session) Rename(id store.SessionID, title string) error {
	if s.store == nil {
		return errors.New("session store is not configured")
	}
	return s.store.Rename(id, title)
}

func (s *Session) DeleteSession(id store.SessionID) error {
	if s.store == nil {
		return errors.New("session store is not configured")
	}
	if id == s.meta.ID {
		return errors.New("cannot delete active session")
	}
	return s.store.Delete(id)
}

func (s *Session) Compact(ctx context.Context, summary string, keepNewest int) error {
	if s.store == nil {
		return errors.New("session store is not configured")
	}
	snapshot := s.Snapshot()
	if len(snapshot.Messages) == 0 {
		return nil
	}
	if keepNewest < 1 {
		keepNewest = 4
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		var err error
		summary, err = s.engine.CompactSummary(ctx)
		if err != nil {
			return err
		}
	}
	kept := compactedMessages(snapshot.Messages, summary, keepNewest)
	original := append([]Message(nil), snapshot.Messages...)
	s.engine.ReplaceMessages(kept)
	if err := s.store.Append(s.meta.ID, store.Record{
		Type: store.EventCompact,
		Compact: &store.Compact{
			Summary: summary, OriginalMessages: original, KeptMessages: kept,
		},
	}); err != nil {
		return err
	}
	s.emitter = newSnapshotEmitter()
	return nil
}

func (s *Session) Undo() error {
	if s.store == nil {
		return errors.New("session store is not configured")
	}
	cp, err := s.store.LastCheckpoint(s.meta.ID)
	if err != nil {
		return errors.New("no checkpoint to undo")
	}
	for _, file := range cp.Files {
		if !file.Exists {
			if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.Path), 0755); err != nil {
			return err
		}
		mode := os.FileMode(file.Mode)
		if mode == 0 {
			mode = 0644
		}
		if err := os.WriteFile(file.Path, []byte(file.Content), mode); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) startTurn() {
	if s.store == nil {
		return
	}
	turn := store.NewTurnID(time.Now())
	s.meta.CurrentTurnID = turn
	_ = s.store.SetCurrentTurn(s.meta.ID, turn)
}

func (s *Session) persistMessage(message Message) {
	if s.store == nil {
		return
	}
	msg := message
	record := store.Record{Type: store.EventMessageAppended, Message: &msg}
	if msg.Role == RoleTool {
		call := ToolCall{ID: msg.ToolCallID, Name: msg.ToolName}
		record.Type = store.EventToolResult
		record.ToolCall = &call
		record.Result = msg.Content
	}
	_ = s.store.Append(s.meta.ID, record)
}

func (s *Session) persistApproval(action ApprovalDecision) {
	if s.store == nil {
		return
	}
	var call *ToolCall
	if pending, ok := s.engine.PendingTool(); ok {
		call = &pending
	}
	_ = s.store.Append(s.meta.ID, store.Record{
		Type: store.EventApprovalDecision, Decision: string(action), ToolCall: call,
	})
}

func (s *Session) persistError(err error) {
	if s.store == nil || err == nil {
		return
	}
	_ = s.store.Append(s.meta.ID, store.Record{Type: store.EventError, Error: err.Error()})
}

func compactedMessages(messages []Message, summary string, keepNewest int) []Message {
	var system []Message
	var rest []Message
	for _, message := range messages {
		if message.Role == RoleSystem {
			system = append(system, message)
		} else {
			rest = append(rest, message)
		}
	}
	if len(rest) <= keepNewest {
		return append(system, rest...)
	}
	summaryMessage := Message{Role: RoleSystem, Content: "Conversation summary:\n" + summary}
	kept := append([]Message(nil), system...)
	kept = append(kept, summaryMessage)
	kept = append(kept, rest[len(rest)-keepNewest:]...)
	return kept
}
