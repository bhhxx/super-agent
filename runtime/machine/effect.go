package machine

type Effect interface {
	isEffect()
}

type CallModel struct{}

func (CallModel) isEffect() {}

type RunTool struct {
	Call ToolCall
}

func (RunTool) isEffect() {}

type ProcessNextToolCall struct{}

func (ProcessNextToolCall) isEffect() {}

// AllEffects lists every Effect type for registration, serialization, and testing.
var AllEffects = []Effect{
	CallModel{},
	RunTool{},
	ProcessNextToolCall{},
}
