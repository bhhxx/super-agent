package runtime

import "context"

type State string

const (
	StateInitializing    State = "Initializing"
	StateIdle            State = "Idle"
	StateWaitingLLM      State = "WaitingLLM"
	StateWaitingApproval State = "WaitingApproval"
	StateRunningTool     State = "RunningTool"
	StateWaitingTool     State = "WaitingTool"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role             Role
	Content          string
	ReasoningContent string
	ToolCallID       string
	ToolCalls        []*ToolCall
}

type ToolCall struct {
	ID    string
	Name  string
	Input string
	Risky bool
}

type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any
	Risky       bool
}

type ModelResponse struct {
	FinalAnswer      string
	ReasoningContent string
	ToolCall         *ToolCall
}

type StreamChunk struct {
	ContentDelta          string
	ReasoningContentDelta string
}

type Model interface {
	Next(ctx context.Context, messages []Message, tools []ToolSpec, chunkFunc func(StreamChunk)) (ModelResponse, error)
}

type ToolRunner interface {
	Specs() []ToolSpec
	Run(ctx context.Context, call ToolCall) (string, error)
}
