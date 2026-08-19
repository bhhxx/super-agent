package runtime

import "super-agent/runtime/session"

type ApprovalDecision = session.ApprovalDecision

const (
	ApproveOnce   = session.ApproveOnce
	ApproveAlways = session.ApproveAlways
	DenyApproval  = session.DenyApproval
)

type SessionEvent = session.SessionEvent
type StateChanged = session.StateChanged
type ToolApprovalRequested = session.ToolApprovalRequested
type ToolApprovalCleared = session.ToolApprovalCleared
type StreamChunkReceived = session.StreamChunkReceived
type MessageAppended = session.MessageAppended
type SessionError = session.SessionError
type Snapshot = session.Snapshot
type Session = session.Session
type SessionID = session.SessionID
type SessionSummary = session.Summary
type SessionMetadata = session.Metadata
type SessionRepository = session.Repository
type SessionWorkspace = session.Workspace
type FileSnapshot = session.FileSnapshot

func NewSession(engine *Engine) *Session { return session.NewSession(engine) }
func NewPersistentSession(engine *Engine, repository SessionRepository, workspace SessionWorkspace, meta SessionMetadata) *Session {
	return session.NewPersistentSession(engine, repository, workspace, meta)
}
func CreatePersistentSession(engine *Engine, repository SessionRepository, workspace SessionWorkspace, meta SessionMetadata, initial []Message) (*Session, error) {
	return session.CreatePersistentSession(engine, repository, workspace, meta, initial)
}
