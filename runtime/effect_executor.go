package runtime

import (
	"context"
	"errors"
)

type EffectContext struct {
	Messages      []Message
	ToolSpecs     []ToolSpec
	NeedsApproval func(ToolCall) bool
}

type EffectExecutor interface {
	Execute(ctx context.Context, effect Effect, env EffectContext, chunkFunc func(StreamChunk)) (Event, error)
}

type DefaultEffectExecutor struct {
	model Model
	tools ToolRunner
}

func NewDefaultEffectExecutor(model Model, tools ToolRunner) *DefaultEffectExecutor {
	return &DefaultEffectExecutor{model: model, tools: tools}
}

func (x *DefaultEffectExecutor) ToolSpecs() []ToolSpec {
	return x.tools.Specs()
}

func (x *DefaultEffectExecutor) Execute(ctx context.Context, effect Effect, env EffectContext, chunkFunc func(StreamChunk)) (Event, error) {
	switch fx := effect.(type) {
	case CallModel:
		resp, err := x.model.Next(ctx, env.Messages, env.ToolSpecs, chunkFunc)
		if err != nil {
			return nil, err
		}
		calls := markRiskyToolCalls(responseToolCalls(resp), env.ToolSpecs)
		if len(calls) > 0 {
			return ToolCallsRequested{
				FinalAnswer:      resp.FinalAnswer,
				Calls:            calls,
				ReasoningContent: resp.ReasoningContent,
				NeedsApproval:    env.NeedsApproval != nil && env.NeedsApproval(calls[0]),
			}, nil
		}
		return AssistantMessageReceived{Response: resp}, nil
	case RunTool:
		result, err := x.tools.Run(ctx, fx.Call)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			return ToolResultReceived{Call: fx.Call, Result: "Error: " + err.Error()}, nil
		}
		return ToolResultReceived{Call: fx.Call, Result: result}, nil
	default:
		return nil, errors.New("unknown effect")
	}
}

func responseToolCalls(resp ModelResponse) []ToolCall {
	if len(resp.ToolCalls) > 0 {
		return append([]ToolCall(nil), resp.ToolCalls...)
	}
	return nil
}

func markRiskyToolCalls(calls []ToolCall, specs []ToolSpec) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	risky := make(map[string]bool, len(specs))
	for _, spec := range specs {
		risky[spec.Name] = spec.Risky
	}
	for i := range calls {
		calls[i].Risky = risky[calls[i].Name]
	}
	return calls
}
