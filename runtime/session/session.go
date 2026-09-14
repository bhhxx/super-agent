package session

import (
	"errors"
	"io"
	"sync"
)

type Session struct {
	engine          *Engine
	emitter         *snapshotEmitter
	repository      Repository
	workspace       Workspace
	meta            Metadata
	metaMu          sync.RWMutex
	permissionMode  PermissionMode
	permissionRules PermissionRules
	mu              sync.Mutex
	closerMu        sync.Mutex
	closers         []io.Closer
	attachmentMu    sync.Mutex
	attachments     []Attachment
}

func (s *Session) AddCloser(closer io.Closer) {
	if closer == nil {
		return
	}
	s.closerMu.Lock()
	defer s.closerMu.Unlock()
	s.closers = append(s.closers, closer)
}

func (s *Session) Close() error {
	_ = s.Cancel()
	s.closerMu.Lock()
	closers := s.closers
	s.closers = nil
	s.closerMu.Unlock()
	var closeErr error
	for index := len(closers) - 1; index >= 0; index-- {
		closeErr = errors.Join(closeErr, closers[index].Close())
	}
	return closeErr
}

// metaID returns the active session id. Cancel runs lock-free, so meta
// reads go through the meta mutex to stay safe against Resume.
func (s *Session) metaID() SessionID {
	s.metaMu.RLock()
	defer s.metaMu.RUnlock()
	return s.meta.ID
}

func NewSession(engine *Engine) *Session {
	return &Session{engine: engine, emitter: newSnapshotEmitter()}
}

func NewPersistentSession(engine *Engine, repository Repository, workspace Workspace, meta Metadata) *Session {
	emitter := newSnapshotEmitter()
	emitter.emittedMessages = len(engine.Snapshot().Messages)
	return &Session{engine: engine, emitter: emitter, repository: repository, workspace: workspace, meta: meta}
}

func CreatePersistentSession(engine *Engine, repository Repository, workspace Workspace, meta Metadata, initial []Message) (*Session, error) {
	if workspace == nil {
		return nil, errors.New("workspace is not configured")
	}
	spec := workspace.Spec()
	if err := workspace.Validate(spec); err != nil {
		return nil, err
	}
	meta.WorkspaceSpec = workspaceSpecPointer(spec)
	meta.CWD = spec.CWD
	created, err := repository.Create(meta, initial)
	if err != nil {
		return nil, err
	}
	return NewPersistentSession(engine, repository, workspace, created), nil
}

func workspaceSpecPointer(spec WorkspaceSpec) *WorkspaceSpec {
	cloned := spec
	cloned.Roots = append([]WorkspaceRootSpec(nil), spec.Roots...)
	return &cloned
}

func (s *Session) ConfigurePermissions(mode PermissionMode, rules PermissionRules) {
	s.permissionMode = mode
	s.permissionRules = rules
}

// PermissionMode reports the active policy mode for display and queries.
func (s *Session) PermissionMode() PermissionMode {
	return s.permissionMode
}

// AutoApproveTools reports whether the active policy approves tools
// without prompting. This is a runtime decision, not a TUI derivation.
func (s *Session) AutoApproveTools() bool {
	return s.permissionMode == PermissionModeBypass
}

func (s *Session) SetPermissionMode(mode PermissionMode) error {
	if mode == "" {
		mode = PermissionModeAsk
	}
	if !ValidPermissionMode(mode) {
		return errors.New("invalid permission mode: " + string(mode))
	}
	if err := s.engine.SetPermissionPolicy(mode, s.permissionRules); err != nil {
		return err
	}
	s.permissionMode = mode
	return nil
}

func (s *Session) Cancel() error {
	err := s.engine.Cancel()
	if s.repository != nil {
		_ = s.repository.SaveCancel(s.metaID())
	}
	return err
}

// Reset stays available while a turn runs: the engine drops stale results
// and the emitter is internally synchronized, so resetting mid-run clears
// the conversation without racing the turn goroutine.
func (s *Session) Reset() error {
	// Persist before mutating so a failed save cannot leave the engine
	// reset while the store still holds the old transcript.
	if s.repository != nil {
		if err := s.repository.SaveReset(s.metaID()); err != nil {
			return err
		}
	}
	err := s.engine.Reset()
	if err == nil {
		s.emitter.reset(len(s.engine.Snapshot().Messages))
	}
	return err
}

// ReplaceConversation activates a new system context and clears prior turns.
func (s *Session) ReplaceConversation(messages []Message) error {
	if !s.mu.TryLock() {
		return errors.New("session is already running a turn")
	}
	defer s.mu.Unlock()
	if s.repository != nil {
		if err := s.repository.SaveConversationReplacement(s.metaID(), messages); err != nil {
			return err
		}
	}
	s.engine.ReplaceMessages(messages)
	s.emitter.reset(len(messages))
	return nil
}

// Metadata returns the session metadata known to this session, including
// the id and title used by persistence.
func (s *Session) Metadata() Metadata {
	s.metaMu.RLock()
	defer s.metaMu.RUnlock()
	return s.meta
}

func (s *Session) Snapshot() EngineView {
	return s.engine.Snapshot()
}

func (s *Session) emitSnapshot(notifications chan<- SessionNotification) {
	s.emitter.emit(notifications, s.Snapshot(), s.persistMessage)
}
