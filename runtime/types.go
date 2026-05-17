package runtime

import "context"

type State string

const (
	StateInitializing    State = "Initializing"
	StateIdle            State = "Idle"
	StateWaitingLLM      State = "WaitingLLM"
	StateWaitingApproval State = "WaitingApproval"
	StateRunningTool     State = "RunningTool"
	StateAdvancingQueue  State = "AdvancingQueue"
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
	ToolName         string
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
	Content          string
	ReasoningContent string
	ToolCalls        []ToolCall
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

func MarkRiskyToolCalls(calls []ToolCall, specs []ToolSpec) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	risky := make(map[string]bool, len(specs))
	for _, spec := range specs {
		risky[spec.Name] = spec.Risky
	}
	for i := range calls {
		calls[i].Risky = risky[calls[i].Name]
	}
	return calls
}
