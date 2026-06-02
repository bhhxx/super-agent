package machine

type Mutation interface {
	isMutation()
}

// AllMutations lists every Mutation type
var AllMutations = []Mutation{
	AppendUserMessage{},
	AppendAssistantMessage{},
	AppendToolResult{},
	AppendStreamingAssistant{},
	FlushStreamingAssistant{},
	SetPendingTool{},
	SetToolCallBatch{},
	AdvanceToolCallBatch{},
	ClearPendingTool{},
	ClearPendingEffects{},
	ClearToolCallBatch{},
	ResetContext{},
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

type AppendStreamingAssistant struct {
	Chunk StreamChunk
}

func (AppendStreamingAssistant) isMutation() {}

type FlushStreamingAssistant struct {
	Interrupted bool
}

func (FlushStreamingAssistant) isMutation() {}

type SetPendingTool struct {
	Call ToolCall
}

func (SetPendingTool) isMutation() {}

type SetToolCallBatch struct {
	ID    string
	Calls []ToolCall
}

func (SetToolCallBatch) isMutation() {}

type AdvanceToolCallBatch struct{}

func (AdvanceToolCallBatch) isMutation() {}

type ClearPendingTool struct{}

func (ClearPendingTool) isMutation() {}

type ClearPendingEffects struct{}

func (ClearPendingEffects) isMutation() {}

type ClearToolCallBatch struct{}

func (ClearToolCallBatch) isMutation() {}

type ResetContext struct{}

func (ResetContext) isMutation() {}
