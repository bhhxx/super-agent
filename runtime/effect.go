package runtime

type Effect interface {
	isEffect()
}

type CallModel struct{}

func (CallModel) isEffect() {}

type RunTool struct {
	Call ToolCall
}

func (RunTool) isEffect() {}
