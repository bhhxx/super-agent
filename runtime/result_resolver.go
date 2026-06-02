package runtime

import "fmt"

type ResultResolver interface {
	Resolve(result ExecutionResult, input ResultResolveInput) (Event, error)
}

type ResultResolveInput struct {
	ToolBatch *ToolCallBatch
}

type DefaultResultResolver struct{}

func (DefaultResultResolver) Resolve(result ExecutionResult, input ResultResolveInput) (Event, error) {
	switch r := result.(type) {
	case ModelReplied:
		calls := r.Response.ToolCalls
		if len(calls) > 0 {
			return ToolCallsReceived{
				Content:          r.Response.Content,
				Calls:            calls,
				ReasoningContent: r.Response.ReasoningContent,
			}, nil
		}
		return AssistantMessageReceived{Response: r.Response}, nil
	case ToolFinished:
		return ToolResultReceived{Call: r.Call, Result: r.Result}, nil
	case ToolQueueChecked:
		if input.ToolBatch == nil || input.ToolBatch.Index >= len(input.ToolBatch.Calls) {
			return ToolBatchFinished{}, nil
		}
		return ToolCallAvailable{Call: input.ToolBatch.Calls[input.ToolBatch.Index]}, nil
	default:
		return nil, fmt.Errorf("unknown effect result type: %T", r)
	}
}
