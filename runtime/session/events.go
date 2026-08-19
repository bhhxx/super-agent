package session

type ApprovalDecision string

const (
	ApproveOnce   ApprovalDecision = "once"
	ApproveAlways ApprovalDecision = "always"
	DenyApproval  ApprovalDecision = "deny"
)

type SessionEvent interface{ isSessionEvent() }

type StateChanged struct{ State State }

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

type MessageAppended struct{ Message Message }

func (MessageAppended) isSessionEvent() {}

type SessionError struct{ Err error }

func (SessionError) isSessionEvent() {}
