package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const memorySystemPrefix = "Cross-session memory:\n"

func (s *Session) ListSessions() ([]Summary, error) {
	if s.repository == nil {
		return nil, errors.New("session store is not configured")
	}
	return s.repository.List()
}

func (s *Session) Fork(title string) (Metadata, error) {
	if !s.mu.TryLock() {
		return Metadata{}, errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository == nil {
		return Metadata{}, errors.New("session store is not configured")
	}
	current := s.Metadata()
	title = strings.TrimSpace(title)
	if title == "" {
		title = current.Title + " (fork)"
	}
	messages := append([]Message(nil), s.Snapshot().Messages...)
	created, err := s.repository.Create(Metadata{Title: title, Provider: current.Provider, Model: current.Model, CWD: current.CWD, InstructionSources: current.InstructionSources, ParentID: current.ID, ProjectID: current.ProjectID, ConfigRoot: current.ConfigRoot, WorkspaceSpec: workspaceSpecPointer(s.workspace.Spec())}, messages)
	if err != nil {
		return Metadata{}, err
	}
	s.engine.ReplaceMessages(messages)
	s.metaMu.Lock()
	s.meta = created
	s.metaMu.Unlock()
	s.emitter = newSnapshotEmitter()
	s.emitter.emittedMessages = len(messages)
	return created, nil
}

func (s *Session) Memories() ([]string, error) {
	if s.repository == nil {
		return nil, errors.New("session store is not configured")
	}
	return s.repository.LoadMemory()
}

func (s *Session) Remember(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("memory text is required")
	}
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	items, err := s.repository.LoadMemory()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item == value {
			return nil
		}
	}
	items = append(items, value)
	if err := s.repository.SaveMemory(items); err != nil {
		return err
	}
	return s.replaceMemoryContext(items)
}

func (s *Session) ForgetMemories() error {
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	if err := s.repository.SaveMemory(nil); err != nil {
		return err
	}
	return s.replaceMemoryContext(nil)
}

func (s *Session) replaceMemoryContext(items []string) error {
	kept := withMemoryContext(s.Snapshot().Messages, items)
	if err := s.repository.SaveConversationReplacement(s.metaID(), kept); err != nil {
		return err
	}
	s.engine.ReplaceMessages(kept)
	s.emitter.reset(len(kept))
	return nil
}

func withMemoryContext(messages []Message, items []string) []Message {
	kept := make([]Message, 0, len(messages)+1)
	for _, message := range messages {
		if message.Role != RoleSystem || !strings.HasPrefix(message.Content, memorySystemPrefix) {
			kept = append(kept, message)
		}
	}
	if len(items) > 0 {
		memory := Message{Role: RoleSystem, Content: memorySystemPrefix + "- " + strings.Join(items, "\n- ")}
		kept = append([]Message{memory}, kept...)
	}
	return kept
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
	if s.workspace == nil {
		return errors.New("workspace is not configured")
	}
	spec, legacy, err := savedWorkspaceSpec(meta)
	if err != nil {
		return err
	}
	if legacy {
		// Sessions written before WorkspaceSpec saved only a cwd and never
		// promised it was canonical, so upgrade it once: re-resolve the saved
		// cwd against the current filesystem and persist the result. Every
		// later resume then takes the strict WorkspaceSpec path.
		spec, err = s.workspace.Canonicalize(spec)
		if err != nil {
			return fmt.Errorf("saved workspace is no longer valid: %w", err)
		}
	}
	if err := s.workspace.Validate(spec); err != nil {
		return fmt.Errorf("saved workspace is no longer valid: %w", err)
	}
	if legacy {
		// Persist before mutating the active session so a failed write leaves
		// the resume untouched instead of half-applied.
		if err := s.repository.SaveWorkspaceDescription(id, spec); err != nil {
			return fmt.Errorf("persist migrated workspace: %w", err)
		}
	}
	memories, err := s.repository.LoadMemory()
	if err != nil {
		return err
	}
	messages = withMemoryContext(messages, memories)
	if err := s.repository.SaveConversationReplacement(id, messages); err != nil {
		return err
	}
	if err := s.workspace.Activate(spec); err != nil {
		return fmt.Errorf("saved workspace is no longer valid: %w", err)
	}
	meta.WorkspaceSpec = workspaceSpecPointer(spec)
	meta.CWD = spec.CWD
	s.engine.ReplaceMessages(messages)
	s.metaMu.Lock()
	s.meta = meta
	s.metaMu.Unlock()
	s.emitter = newSnapshotEmitter()
	s.emitter.emittedMessages = len(messages)
	return nil
}

// savedWorkspaceSpec returns the durable workspace description a resume must
// restore. The legacy flag marks metadata written before WorkspaceSpec: it
// carries only a cwd, which the resume upgrades to a canonical spec once.
func savedWorkspaceSpec(meta Metadata) (WorkspaceSpec, bool, error) {
	if meta.WorkspaceSpec != nil {
		return *workspaceSpecPointer(*meta.WorkspaceSpec), false, nil
	}
	if meta.CWD == "" {
		return WorkspaceSpec{}, false, errors.New("saved workspace is no longer valid: legacy session has no cwd")
	}
	return WorkspaceSpec{
		PrimaryRoot: meta.CWD,
		CWD:         meta.CWD,
		Roots:       []WorkspaceRootSpec{{Path: meta.CWD, Access: WorkspaceAccessReadWrite}},
	}, true, nil
}

func (s *Session) RenameSession(id SessionID, title string) error {
	if s.repository == nil {
		return errors.New("session store is not configured")
	}
	return s.repository.RenameSession(id, title)
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
	if nonSystemMessageCount(snapshot.Messages) <= keepNewest {
		return nil
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
	files, messages, index, err := s.repository.LoadUndoPoint(s.metaID())
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

func nonSystemMessageCount(messages []Message) int {
	count := 0
	for _, message := range messages {
		if message.Role != RoleSystem {
			count++
		}
	}
	return count
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
	start := len(rest) - keepNewest
	if rest[start].Role == RoleTool {
		for start > 0 && rest[start].Role == RoleTool {
			start--
		}
	}
	return append(kept, rest[start:]...)
}
