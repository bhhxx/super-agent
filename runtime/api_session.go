package runtime

import "super-agent/runtime/session"

type ApprovalDecision = session.ApprovalDecision

const (
	ApproveOnce   = session.ApproveOnce
	ApproveAlways = session.ApproveAlways
	DenyApproval  = session.DenyApproval
)

type SessionNotification = session.SessionNotification
type StateChanged = session.StateChanged
type ToolApprovalRequested = session.ToolApprovalRequested
type ToolApprovalCleared = session.ToolApprovalCleared
type StreamChunkReceived = session.StreamChunkReceived
type MessageAppended = session.MessageAppended
type SessionError = session.SessionError
type EngineView = session.EngineView
type Session = session.Session
type SessionID = session.SessionID
type SessionSummary = session.Summary
type SessionMetadata = session.Metadata
type SessionRepository = session.Repository
type SessionWorkspace = session.Workspace
type FileSnapshot = session.FileSnapshot
type WorkspaceAccessMode = session.WorkspaceAccessMode
type WorkspaceRootSpec = session.WorkspaceRootSpec
type WorkspaceSpec = session.WorkspaceSpec

const (
	WorkspaceAccessRead      = session.WorkspaceAccessRead
	WorkspaceAccessReadWrite = session.WorkspaceAccessReadWrite
)

func NewSession(engine *Engine) *Session { return session.NewSession(engine) }
func NewPersistentSession(engine *Engine, repository SessionRepository, workspace SessionWorkspace, meta SessionMetadata) *Session {
	return session.NewPersistentSession(engine, repository, workspace, meta)
}
func CreatePersistentSession(engine *Engine, repository SessionRepository, workspace SessionWorkspace, meta SessionMetadata, initial []Message) (*Session, error) {
	return session.CreatePersistentSession(engine, repository, workspace, meta, initial)
}
