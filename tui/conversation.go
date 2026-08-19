package tui

import "context"

type AgentStatus struct {
	Label            string
	Busy             bool
	AwaitingApproval bool
}

type Role string

const RoleAssistant Role = "assistant"

type ToolCall struct{ ID, Name, Input string }

type Message struct {
	Role             Role
	Content          string
	ReasoningContent string
	ToolCallID       string
	ToolName         string
	ToolCalls        []*ToolCall
	Interrupted      bool
}

type PermissionRequest struct {
	ToolName     string
	Command      string
	CommandClass string
	CWD          string
	TouchedPaths []string
	EnvVars      []string
	Reason       string
}

type Snapshot struct {
	AgentStatus           AgentStatus
	Messages              []Message
	PendingTool           *ToolCall
	PendingPermission     *PermissionRequest
	PendingToolBatchIndex int
	PendingToolBatchTotal int
	StreamingMessage      *Message
}

type SessionSummary struct{ ID, Title, Provider, Model, CWD string }

type ApprovalDecision string

const (
	ApproveOnce   ApprovalDecision = "once"
	ApproveAlways ApprovalDecision = "always"
	DenyApproval  ApprovalDecision = "deny"
)

type Event interface{ isEvent() }
type AgentStatusChanged struct{ Status AgentStatus }

func (AgentStatusChanged) isEvent() {}

type ToolApprovalRequested struct {
	ToolCall               ToolCall
	Request                PermissionRequest
	BatchIndex, BatchTotal int
}

func (ToolApprovalRequested) isEvent() {}

type ToolApprovalCleared struct{}

func (ToolApprovalCleared) isEvent() {}

type StreamChunkReceived struct{ Message *Message }

func (StreamChunkReceived) isEvent() {}

type MessageAppended struct{ Message Message }

func (MessageAppended) isEvent() {}

type ConversationError struct{ Err error }

func (ConversationError) isEvent() {}

// Conversation is the TUI input port. Its DTOs contain no runtime or storage types.
type Conversation interface {
	Snapshot() Snapshot
	RunTurn(context.Context, string, chan<- Event, <-chan ApprovalDecision) error
	Cancel() error
	Reset() error
	Sessions() ([]SessionSummary, error)
	Resume(string) error
	Rename(string, string) error
	DeleteSession(string) error
	Compact(context.Context, string) error
	Undo() error
	SetPermissionMode(string) error
	// PermissionMode and AutoApproveTools report the runtime policy so the
	// TUI never derives behavior locally.
	PermissionMode() string
	AutoApproveTools() bool
}
