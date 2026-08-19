package session

import (
	"errors"
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
	created, err := repository.Create(meta, initial)
	if err != nil {
		return nil, err
	}
	return NewPersistentSession(engine, repository, workspace, created), nil
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

// Metadata returns the session metadata known to this session, including
// the id and title used by persistence.
func (s *Session) Metadata() Metadata {
	s.metaMu.RLock()
	defer s.metaMu.RUnlock()
	return s.meta
}

func (s *Session) Snapshot() Snapshot {
	return s.engine.Snapshot()
}

func (s *Session) emitSnapshot(events chan<- SessionEvent) {
	s.emitter.emit(events, s.Snapshot(), s.persistMessage)
}
