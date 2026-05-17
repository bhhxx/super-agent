package runtime

type Mutation interface {
	isMutation()
}

// AllMutations lists every Mutation type
var AllMutations = []Mutation{
	AppendUserMessage{},
	AppendAssistantMessage{},
	AppendToolResult{},
	SetPendingTool{},
	SetQueuedToolCalls{},
	ClearPendingTool{},
	ClearPendingEffects{},
	ClearQueuedToolCalls{},
	PopQueuedToolCall{},
	ResetContext{},
}

type PopQueuedToolCall struct{}

func (PopQueuedToolCall) isMutation() {}

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

type SetQueuedToolCalls struct {
	Calls []ToolCall
}

func (SetQueuedToolCalls) isMutation() {}

type ClearPendingTool struct{}

func (ClearPendingTool) isMutation() {}

type ClearPendingEffects struct{}

func (ClearPendingEffects) isMutation() {}

type ClearQueuedToolCalls struct{}

func (ClearQueuedToolCalls) isMutation() {}

type ResetContext struct{}

func (ResetContext) isMutation() {}
