package runtime

type Effect interface {
	isEffect()
}

// AllEffects lists every Effect type
var AllEffects = []Effect{
	CallModel{},
	RunTool{},
}

type CallModel struct{}

func (CallModel) isEffect() {}

type RunTool struct {
	Call ToolCall
}

func (RunTool) isEffect() {}
