package protocol

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role             Role         `json:"role"`
	Content          string       `json:"content,omitempty"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
	ToolName         string       `json:"tool_name,omitempty"`
	ToolCalls        []*ToolCall  `json:"tool_calls,omitempty"`
	Interrupted      bool         `json:"interrupted,omitempty"`
	Attachments      []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Risky       bool           `json:"risky,omitempty"`
}

// Usage carries the provider-reported token counts for one model response.
// A nil Usage on a ModelResponse means the adapter could not obtain exact
// numbers and telemetry should estimate instead.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type ModelResponse struct {
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	Usage            *Usage     `json:"usage,omitempty"`
}

type StreamChunk struct {
	ContentDelta          string `json:"content_delta,omitempty"`
	ReasoningContentDelta string `json:"reasoning_content_delta,omitempty"`
}

type Model interface {
	Next(context.Context, []Message, []ToolSpec, func(StreamChunk)) (ModelResponse, error)
}

type ToolRunner interface {
	Specs() []ToolSpec
	Run(context.Context, ToolCall) (string, error)
}
