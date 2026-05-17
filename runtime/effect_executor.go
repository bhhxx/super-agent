package runtime

import (
	"context"
	"errors"
)

type ExecutionInput struct {
	Messages  []Message
	ToolSpecs []ToolSpec
}

type EffectExecutor interface {
	Execute(ctx context.Context, effect Effect, env ExecutionInput, chunkFunc func(StreamChunk)) (ExecutionResult, error)
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

func (x *DefaultEffectExecutor) Execute(ctx context.Context, effect Effect, env ExecutionInput, chunkFunc func(StreamChunk)) (ExecutionResult, error) {
	switch fx := effect.(type) {
	case CallModel:
		resp, err := x.model.Next(ctx, env.Messages, env.ToolSpecs, chunkFunc)
		if err != nil {
			return nil, err
		}
		return ModelReplied{Response: resp}, nil
	case RunTool:
		result, err := x.tools.Run(ctx, fx.Call)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			return ToolFinished{Call: fx.Call, Result: "Error: " + err.Error()}, nil
		}
		return ToolFinished{Call: fx.Call, Result: result}, nil
	case ProcessNextToolCall:
		return ToolQueueChecked{}, nil
	default:
		return nil, errors.New("unknown effect")
	}
}
