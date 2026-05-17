package runtime

type Effect interface {
	isEffect()
}

// AllEffects lists every Effect type
var AllEffects = []Effect{
	CallModel{},
	RunTool{},
	ProcessNextToolCall{},
}

type CallModel struct{}

func (CallModel) isEffect() {}

type RunTool struct {
	Call ToolCall
}

func (RunTool) isEffect() {}

type ProcessNextToolCall struct{}

func (ProcessNextToolCall) isEffect() {}
