package runtime

import "context"

type QueuedEffect struct {
	RunID    RunID
	EffectID EffectID
	Effect   Effect
}

type EffectOutcome struct {
	RunID    RunID
	EffectID EffectID
	Result   ExecutionResult
}

type EffectRunner interface {
	Run(ctx context.Context, effect QueuedEffect, input ExecutionInput, chunkFunc func(StreamChunk)) (EffectOutcome, error)
	ToolSpecs() []ToolSpec
}

type DefaultEffectRunner struct {
	executor EffectExecutor
}

func NewDefaultEffectRunner(executor EffectExecutor) *DefaultEffectRunner {
	return &DefaultEffectRunner{executor: executor}
}

func (r *DefaultEffectRunner) ToolSpecs() []ToolSpec {
	if provider, ok := r.executor.(interface{ ToolSpecs() []ToolSpec }); ok {
		return provider.ToolSpecs()
	}
	return nil
}

func (r *DefaultEffectRunner) Run(ctx context.Context, effect QueuedEffect, input ExecutionInput, chunkFunc func(StreamChunk)) (EffectOutcome, error) {
	result, err := r.executor.Execute(ctx, effect.Effect, input, chunkFunc)
	if err != nil {
		return EffectOutcome{}, err
	}
	return EffectOutcome{
		RunID:    effect.RunID,
		EffectID: effect.EffectID,
		Result:   result,
	}, nil
}
