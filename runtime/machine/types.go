package machine

import "super-agent/runtime/protocol"

type State string

const (
	StateInitializing    State = "Initializing"
	StateIdle            State = "Idle"
	StateWaitingLLM      State = "WaitingLLM"
	StateWaitingApproval State = "WaitingApproval"
	StateRunningTool     State = "RunningTool"
	StateAdvancingQueue  State = "AdvancingQueue"
)

type ToolCallBatch struct {
	ID    string              `json:"id"`
	Calls []protocol.ToolCall `json:"calls"`
	Index int                 `json:"index"`
}
