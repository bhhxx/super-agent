package engine

type Snapshot struct {
	State                 State
	Messages              []Message
	PendingTool           *ToolCall
	PendingPermission     *PermissionRequest
	PendingToolBatchID    string
	PendingToolBatchIndex int
	PendingToolBatchTotal int
	StreamingMessage      *Message
	IsBusy                bool
	NeedsInput            bool
}
