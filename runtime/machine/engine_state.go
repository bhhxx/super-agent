package machine

type EngineState struct {
	State              State
	Messages           []Message
	PendingTool        *ToolCall
	ToolBatch          *ToolCallBatch
	StreamingContent   string
	StreamingReasoning string
}
