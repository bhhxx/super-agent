package runtime

type Mutation interface {
	isMutation()
}

type AppendUserMessage struct {
	Content string
}

func (AppendUserMessage) isMutation() {}

type AppendAssistantMessage struct {
	Message Message
}

func (AppendAssistantMessage) isMutation() {}

type AppendToolResult struct {
	Call   ToolCall
	Result string
}

func (AppendToolResult) isMutation() {}

type SetPendingTool struct {
	Call ToolCall
}

func (SetPendingTool) isMutation() {}

type SetPendingToolQueue struct {
	Calls []ToolCall
}

func (SetPendingToolQueue) isMutation() {}

type ClearPendingTool struct{}

func (ClearPendingTool) isMutation() {}

type ClearPendingEffects struct{}

func (ClearPendingEffects) isMutation() {}

type ClearPendingToolQueue struct{}

func (ClearPendingToolQueue) isMutation() {}

type ResetContext struct{}

func (ResetContext) isMutation() {}
