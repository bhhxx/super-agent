package runtime

type ExecutionResult interface {
	isExecutionResult()
}

type ModelReplied struct {
	Response ModelResponse
}

func (ModelReplied) isExecutionResult() {}

type ToolFinished struct {
	Call   ToolCall
	Result string
}

func (ToolFinished) isExecutionResult() {}

type ToolQueueChecked struct{}

func (ToolQueueChecked) isExecutionResult() {}
