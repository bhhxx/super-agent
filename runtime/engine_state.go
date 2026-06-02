package runtime

type EngineState struct {
	State              State
	Messages           []Message
	PendingTool        *ToolCall
	QueuedToolCalls    []ToolCall
	StreamingContent   string
	StreamingReasoning string
}
