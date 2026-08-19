package session

import (
	"context"
	"errors"
	"strings"
)

func (s *Session) Sessions() ([]Summary, error) {
	if s.repository == nil {
		return nil, errors.New("session store is not configured")
	}
	return s.repository.List()
}

func (s *Session) Resume(id SessionID) error {
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	messages, meta, err := s.repository.Load(id)
	if err != nil {
		return err
	}
	s.engine.ReplaceMessages(messages)
	s.metaMu.Lock()
	s.meta = meta
	s.metaMu.Unlock()
	s.emitter = newSnapshotEmitter()
	s.emitter.emittedMessages = len(messages)
	return nil
}

func (s *Session) Rename(id SessionID, title string) error {
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	return s.repository.Rename(id, title)
}

func (s *Session) DeleteSession(id SessionID) error {
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	if id == s.metaID() {
		return errors.New("cannot delete active session")
	}
	return s.repository.Delete(id)
}

func (s *Session) Compact(ctx context.Context, summary string, keepNewest int) error {
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository == nil {
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
	// Persist before mutating so a failed save cannot leave the engine
	// compacted while the store still holds the old transcript.
	if err := s.repository.SaveCompaction(s.metaID(), summary, original, kept); err != nil {
		return err
	}
	s.engine.ReplaceMessages(kept)
	// The kept messages were already emitted and persisted through the
	// compaction record; re-emission would duplicate the transcript.
	s.emitter.emittedMessages = len(kept)
	return nil
}

func (s *Session) Undo() error {
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	if s.workspace == nil {
		return errors.New("workspace is not configured")
	}
	files, messages, index, err := s.repository.CheckpointState(s.metaID())
	if err != nil {
		return errors.New("no checkpoint to undo")
	}
	// Restore the workspace first; only when that succeeds do we truncate
	// the transcript so both sides stay consistent.
	if err := s.workspace.Restore(files); err != nil {
		return err
	}
	if err := s.repository.TruncateAfter(s.metaID(), index); err != nil {
		return err
	}
	s.engine.ReplaceMessages(messages)
	s.emitter.emittedMessages = len(messages)
	return nil
}

func compactedMessages(messages []Message, summary string, keepNewest int) []Message {
	var system, rest []Message
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
	kept := append([]Message(nil), system...)
	kept = append(kept, Message{Role: RoleSystem, Content: "Conversation summary:\n" + summary})
	return append(kept, rest[len(rest)-keepNewest:]...)
}
