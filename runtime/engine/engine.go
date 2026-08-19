package engine

import (
	"sync"

	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
)

type Engine struct {
	mu        sync.Mutex
	runner    execution.EffectRunner
	resolver  execution.OutcomeResolver
	reducer   machine.Reducer
	runs      execution.RunController
	approvals execution.ApprovalStore
	state     machine.EngineState
	scheduler *execution.EffectScheduler

	// stateObserver is notified after state-changing transitions while
	// effects drain. It runs outside the engine lock so it can read
	// snapshots. session.RunTurn installs a per-turn observer.
	stateObserver func()
}

type policySetter interface {
	SetPolicy(execution.Policy)
}

type policyStore interface {
	SetPermissionPolicy(execution.PermissionMode, execution.PermissionRules)
}

type policySnapshot interface {
	Mode() execution.PermissionMode
	Rules() execution.PermissionRules
}

func NewEngine(model protocol.Model, tools protocol.ToolRunner, initial []protocol.Message) *Engine {
	return NewEngineWithExecutor(execution.NewDefaultEffectExecutor(model, tools), initial)
}

func NewEngineWithExecutor(executor execution.EffectExecutor, initial []protocol.Message) *Engine {
	approvals := execution.NewMemoryApprovalStore()
	policy := execution.NewDefaultPolicy()
	approvals.SetPermissionPolicy(policy.Mode(), policy.Rules())
	return NewEngineWithComponents(
		execution.NewDefaultEffectRunner(executor),
		execution.NewDefaultOutcomeResolver(policy, approvals),
		machine.DefaultReducer{},
		execution.NewDefaultRunController(),
		approvals,
		initial,
	)
}

func NewEngineWithExecutorAndPolicy(executor execution.EffectExecutor, policy execution.Policy, initial []protocol.Message) *Engine {
	approvals := execution.NewMemoryApprovalStore()
	if snapshot, ok := policy.(policySnapshot); ok {
		approvals.SetPermissionPolicy(snapshot.Mode(), snapshot.Rules())
	}
	return NewEngineWithComponents(execution.NewDefaultEffectRunner(executor), execution.NewDefaultOutcomeResolver(policy, approvals), machine.DefaultReducer{}, execution.NewDefaultRunController(), approvals, initial)
}

func NewEngineWithComponents(runner execution.EffectRunner, resolver execution.OutcomeResolver, reducer machine.Reducer, runs execution.RunController, approvals execution.ApprovalStore, initial []protocol.Message) *Engine {
	messages := append([]protocol.Message(nil), initial...)
	return &Engine{
		runner:    runner,
		resolver:  resolver,
		reducer:   reducer,
		runs:      runs,
		approvals: approvals,
		scheduler: execution.NewEffectScheduler(),
		state: machine.EngineState{
			State:    machine.StateInitializing,
			Messages: messages,
		},
	}
}

// SetStateObserver registers a callback fired after state-changing
// transitions while effects drain. The callback runs outside the engine
// lock so it can read snapshots.
func (e *Engine) SetStateObserver(observer func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stateObserver = observer
}

func (e *Engine) notifyStateObserver() {
	e.mu.Lock()
	observer := e.stateObserver
	e.mu.Unlock()
	if observer != nil {
		observer()
	}
}
