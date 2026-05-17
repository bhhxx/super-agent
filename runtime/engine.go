package runtime

import (
	"context"
	"errors"
	"strconv"
	"sync"
)

type Engine struct {
	mu          sync.Mutex
	runner      EffectRunner
	resolver    ResultResolver
	classifier  EventClassifier
	reducer     Reducer
	runs        RunController
	approvals   ApprovalStore
	state       EngineState
	effectQueue []QueuedEffect
	nextEffect  int64
}

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	return NewEngineWithExecutor(NewDefaultEffectExecutor(model, tools), initial)
}

func NewEngineWithExecutor(executor EffectExecutor, initial []Message) *Engine {
	approvals := NewMemoryApprovalStore()
	return NewEngineWithComponents(
		NewDefaultEffectRunner(executor),
		DefaultResultResolver{},
		NewDefaultEventClassifier(NewDefaultPolicy(), approvals),
		DefaultReducer{},
		NewDefaultRunController(),
		approvals,
		initial,
	)
}

func NewEngineWithExecutorAndPolicy(executor EffectExecutor, policy Policy, initial []Message) *Engine {
	approvals := NewMemoryApprovalStore()
	return NewEngineWithComponents(NewDefaultEffectRunner(executor), DefaultResultResolver{}, NewDefaultEventClassifier(policy, approvals), DefaultReducer{}, NewDefaultRunController(), approvals, initial)
}

func NewEngineWithComponents(runner EffectRunner, resolver ResultResolver, classifier EventClassifier, reducer Reducer, runs RunController, approvals ApprovalStore, initial []Message) *Engine {
	messages := append([]Message(nil), initial...)
	return &Engine{
		runner:     runner,
		resolver:   resolver,
		classifier: classifier,
		reducer:    reducer,
		runs:       runs,
		approvals:  approvals,
		state: EngineState{
			State:    StateInitializing,
			Messages: messages,
		},
	}
}

func (e *Engine) EnableAutoApproveTools() {
	e.approvals.SetAutoApproveTools(true)
}

func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state.State
}

func (e *Engine) Messages() []Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Message(nil), e.state.Messages...)
}

func (e *Engine) PendingTool() (ToolCall, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state.PendingTool == nil {
		return ToolCall{}, false
	}
	return *e.state.PendingTool, true
}

func (e *Engine) QueuedToolCalls() []ToolCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]ToolCall(nil), e.state.QueuedToolCalls...)
}

func (e *Engine) snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot := Snapshot{
		State:      e.state.State,
		Messages:   append([]Message(nil), e.state.Messages...),
		IsBusy:     e.state.State == StateWaitingLLM || e.state.State == StateRunningTool || e.state.State == StateAdvancingQueue,
		NeedsInput: e.state.State == StateWaitingApproval,
	}
	if e.state.PendingTool != nil {
		call := *e.state.PendingTool
		snapshot.PendingTool = &call
	}
	return snapshot
}

func (e *Engine) Ready() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dispatchLocked(EngineReady{})
}

func (e *Engine) Approve(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state.State != StateWaitingApproval || e.state.PendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.state.PendingTool
	err := e.dispatchLocked(ApprovalGranted{Call: call})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	runCtx, ok := e.runs.CurrentContext()
	if !ok {
		return errors.New("no active run context")
	}
	return e.runPendingEffects(runCtx, chunkFunc)
}

func (e *Engine) ApproveAlways(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state.State != StateWaitingApproval || e.state.PendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.state.PendingTool
	e.mu.Unlock()

	e.approvals.AllowAlways(NewApprovalKey(call))

	e.mu.Lock()
	err := e.dispatchLocked(ApprovalAlwaysGranted{Call: call})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	runCtx, ok := e.runs.CurrentContext()
	if !ok {
		return errors.New("no active run context")
	}
	return e.runPendingEffects(runCtx, chunkFunc)
}

func (e *Engine) Deny(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state.State != StateWaitingApproval || e.state.PendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.state.PendingTool
	err := e.dispatchLocked(ApprovalDenied{Call: call})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	runCtx, ok := e.runs.CurrentContext()
	if !ok {
		return errors.New("no active run context")
	}
	return e.runPendingEffects(runCtx, chunkFunc)
}

func (e *Engine) Cancel() error {
	e.runs.CancelRun()
	return e.dispatch(CancelRequested{})
}

func (e *Engine) Reset() error {
	e.runs.CancelRun()
	e.runs.StartNewGeneration()
	return e.dispatch(ResetRequested{})
}

func (e *Engine) dispatchEventThenRunEffects(ctx context.Context, event Event, chunkFunc func(StreamChunk), afterDispatch func()) error {
	e.mu.Lock()
	decision, err := Transition(e.state.State, event)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	_, runCtx := e.runs.StartRun(ctx)
	if err := e.applyTransitionLocked(decision); err != nil {
		e.mu.Unlock()
		return err
	}
	e.mu.Unlock()
	afterDispatch()
	return e.runPendingEffects(runCtx, chunkFunc)
}

func (e *Engine) dispatch(event Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dispatchLocked(event)
}

func (e *Engine) dispatchLocked(event Event) error {
	decision, err := Transition(e.state.State, event)
	if err != nil {
		return err
	}
	return e.applyTransitionLocked(decision)
}

func (e *Engine) applyTransitionLocked(decision TransitionResult) error {
	e.state.State = decision.NextState
	for _, m := range decision.Mutations {
		e.reducer.Apply(&e.state, &e.effectQueue, m)
	}
	for _, effect := range decision.Effects {
		e.effectQueue = append(e.effectQueue, e.queueEffectLocked(effect))
	}
	return nil
}

func (e *Engine) queueEffectLocked(effect Effect) QueuedEffect {
	e.nextEffect++
	return QueuedEffect{
		RunID:    e.runs.CurrentRunID(),
		EffectID: EffectID("effect-" + strconv.FormatInt(e.nextEffect, 10)),
		Effect:   effect,
	}
}

func (e *Engine) runPendingEffects(ctx context.Context, chunkFunc func(StreamChunk)) error {
	runID := e.runs.CurrentRunID()
	for {
		e.mu.Lock()
		if len(e.effectQueue) == 0 {
			if e.state.State == StateIdle {
				e.runs.FinishRun(runID)
			}
			e.mu.Unlock()
			return nil
		}
		effect := e.effectQueue[0]
		e.effectQueue = e.effectQueue[1:]
		e.mu.Unlock()
		if err := e.executeEffect(ctx, effect, chunkFunc); err != nil {
			if errors.Is(err, context.Canceled) {
				e.runs.CancelRun()
				_ = e.dispatch(CancelRequested{})
			} else {
				_ = e.dispatch(ErrorOccurred{Err: err})
			}
			return err
		}
	}
}

func (e *Engine) executeEffect(ctx context.Context, effect QueuedEffect, chunkFunc func(StreamChunk)) error {
	outcome, err := e.runner.Run(ctx, effect, ExecutionInput{
		Messages:  e.Messages(),
		ToolSpecs: e.toolSpecs(),
	}, chunkFunc)
	if err != nil {
		return err
	}
	if !e.runs.IsCurrent(outcome.RunID) {
		return nil
	}
	toolSpecs := e.toolSpecs()
	e.mu.Lock()
	event, err := e.resolver.Resolve(outcome.Result, ResultResolveInput{
		QueuedToolCalls: append([]ToolCall(nil), e.state.QueuedToolCalls...),
	})
	if err == nil {
		event, err = e.classifier.Classify(event, EventClassifyInput{ToolSpecs: toolSpecs})
	}
	if err != nil {
		dispatchErr := e.dispatchLocked(ErrorOccurred{Err: err})
		e.mu.Unlock()
		if dispatchErr != nil {
			return dispatchErr
		}
		return err
	}
	err = e.dispatchLocked(event)
	e.mu.Unlock()
	return err
}

func (e *Engine) toolSpecs() []ToolSpec {
	return e.runner.ToolSpecs()
}
