package runtime

import "super-agent/runtime/engine"

type Engine = engine.Engine

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	return engine.NewEngine(model, tools, initial)
}
func NewEngineWithExecutor(executor EffectExecutor, initial []Message) *Engine {
	return engine.NewEngineWithExecutor(executor, initial)
}
func NewEngineWithExecutorAndPolicy(executor EffectExecutor, policy Policy, initial []Message) *Engine {
	return engine.NewEngineWithExecutorAndPolicy(executor, policy, initial)
}
func NewEngineWithComponents(runner EffectRunner, resolver OutcomeResolver, reducer Reducer, runs RunController, approvals ApprovalStore, initial []Message) *Engine {
	return engine.NewEngineWithComponents(runner, resolver, reducer, runs, approvals, initial)
}
