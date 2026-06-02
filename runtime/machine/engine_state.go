package machine

type EngineState struct {
	State              State
	Messages           []Message
	PendingTool        *ToolCall
	PendingPermission  *PermissionRequest
	ToolBatch          *ToolCallBatch
	StreamingContent   string
	StreamingReasoning string
}
