package engine

type Snapshot struct {
	State                 State
	Messages              []Message
	PendingTool           *ToolCall
	PendingToolBatchID    string
	PendingToolBatchIndex int
	PendingToolBatchTotal int
	StreamingMessage      *Message
	IsBusy                bool
	NeedsInput            bool
}
