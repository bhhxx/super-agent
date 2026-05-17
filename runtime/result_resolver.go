package runtime

import "fmt"

type ResultResolver interface {
	Resolve(result ExecutionResult, input ResultResolveInput) (Event, error)
}

type ResultResolveInput struct {
	QueuedToolCalls []ToolCall
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
		if len(input.QueuedToolCalls) == 0 {
			return NoMoreToolCalls{}, nil
		}
		return NextToolCallAvailable{Call: input.QueuedToolCalls[0]}, nil
	default:
		return nil, fmt.Errorf("unknown effect result type: %T", r)
	}
}
