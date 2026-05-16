package runtime

import (
	"context"
	"errors"
)

type EffectContext struct {
	Messages       []Message
	ToolSpecs      []ToolSpec
	NeedsApproval  func(ToolCall) bool
	NextQueuedTool func() (*ToolCall, bool)
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
		calls := responseToolCalls(resp)
		if len(calls) > 0 {
			return ToolCallsRequested{
				Calls:            calls,
				ReasoningContent: resp.ReasoningContent,
				NeedsApproval:    env.NeedsApproval != nil && env.NeedsApproval(calls[0]),
			}, nil
		}
		return AssistantMessageReceived{Response: resp}, nil
	case RunTool:
		result, err := x.tools.Run(ctx, fx.Call)
		if err != nil {
			return nil, err
		}
		next, ok := nextQueuedTool(env)
		return ToolResultReceived{
			Call:              fx.Call,
			Result:            result,
			NextCall:          next,
			NextNeedsApproval: ok && env.NeedsApproval != nil && env.NeedsApproval(*next),
		}, nil
	default:
		return nil, errors.New("unknown effect")
	}
}

func nextQueuedTool(env EffectContext) (*ToolCall, bool) {
	if env.NextQueuedTool == nil {
		return nil, false
	}
	return env.NextQueuedTool()
}
