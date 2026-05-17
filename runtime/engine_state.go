package runtime

type EngineState struct {
	State            State
	Messages         []Message
	PendingTool      *ToolCall
	QueuedToolCalls  []ToolCall
	PendingEffects   []Effect
	AlwaysAllow      map[string]bool
	AutoApproveTools bool
}
