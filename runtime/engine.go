package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Engine struct {
	mu          sync.Mutex
	executor    EffectExecutor
	policy      Policy
	state       EngineState
	effectQueue []Effect
}

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	return NewEngineWithExecutor(NewDefaultEffectExecutor(model, tools), initial)
}

func NewEngineWithExecutor(executor EffectExecutor, initial []Message) *Engine {
	messages := append([]Message(nil), initial...)
	return &Engine{
		executor: executor,
		policy:   NewDefaultPolicy(),
		state: EngineState{
			State:    StateInitializing,
			Messages: messages,
		},
	}
}

func (e *Engine) EnableAutoApproveTools() {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.dispatchLocked(AutoApproveToolsRequested{Enabled: true})
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

func (e *Engine) Ready() {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.dispatchLocked(EngineReady{})
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
	return e.runPendingEffects(ctx, chunkFunc)
}

func (e *Engine) ApproveAlways(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state.State != StateWaitingApproval || e.state.PendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.state.PendingTool
	err := e.dispatchLocked(ApprovalAlwaysGranted{Call: call})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	return e.runPendingEffects(ctx, chunkFunc)
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
	return e.runPendingEffects(ctx, chunkFunc)
}

func (e *Engine) Cancel() error {
	return e.dispatch(CancelRequested{})
}

func (e *Engine) Reset() {
	_ = e.dispatch(ResetRequested{})
}

func (e *Engine) dispatchEventThenRunEffects(ctx context.Context, event Event, chunkFunc func(StreamChunk), afterDispatch func()) error {
	if err := e.dispatch(event); err != nil {
		return err
	}
	afterDispatch()
	return e.runPendingEffects(ctx, chunkFunc)
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
	e.state.State = decision.NextState
	for _, m := range decision.Mutations {
		if err := e.applyMutation(m); err != nil {
			return err
		}
	}
	e.effectQueue = append(e.effectQueue, decision.Effects...)
	return nil
}

func (e *Engine) applyMutation(mutation Mutation) error {
	switch m := mutation.(type) {
	case AppendUserMessage:
		e.state.Messages = append(e.state.Messages, Message{Role: RoleUser, Content: m.Content})
	case AppendAssistantMessage:
		e.state.Messages = append(e.state.Messages, m.Message)
	case AppendToolResult:
		e.state.Messages = append(e.state.Messages, Message{Role: RoleTool, Content: m.Result, ToolCallID: m.Call.ID, ToolName: m.Call.Name})
	case SetPendingTool:
		call := m.Call
		e.state.PendingTool = &call
	case SetQueuedToolCalls:
		e.state.QueuedToolCalls = append([]ToolCall(nil), m.Calls...)
	case PopQueuedToolCall:
		if len(e.state.QueuedToolCalls) > 0 {
			e.state.QueuedToolCalls[0] = ToolCall{}
			e.state.QueuedToolCalls = e.state.QueuedToolCalls[1:]
		}
	case ClearPendingTool:
		e.state.PendingTool = nil
	case ClearQueuedToolCalls:
		e.state.QueuedToolCalls = nil
	case ClearPendingEffects:
		e.effectQueue = nil
	case ResetContext:
		e.state.Messages = nil
		e.state.PendingTool = nil
		e.state.QueuedToolCalls = nil
		e.effectQueue = nil
	case AddAlwaysAllow:
		if p, ok := e.policy.(*DefaultPolicy); ok {
			p.AddAlwaysAllow(m.Key)
		}
	case SetAutoApproveTools:
		if p, ok := e.policy.(*DefaultPolicy); ok {
			p.SetAutoApproveTools(m.Enabled)
		}
	default:
		return fmt.Errorf("unknown mutation type: %T", m)
	}
	return nil
}

func (e *Engine) runPendingEffects(ctx context.Context, chunkFunc func(StreamChunk)) error {
	for {
		e.mu.Lock()
		if len(e.effectQueue) == 0 {
			e.mu.Unlock()
			return nil
		}
		effect := e.effectQueue[0]
		e.effectQueue = e.effectQueue[1:]
		e.mu.Unlock()
		if err := e.executeEffect(ctx, effect, chunkFunc); err != nil {
			if errors.Is(err, context.Canceled) {
				_ = e.dispatch(CancelRequested{})
			} else {
				_ = e.dispatch(ErrorOccurred{Err: err})
			}
			return err
		}
	}
}

func (e *Engine) executeEffect(ctx context.Context, effect Effect, chunkFunc func(StreamChunk)) error {
	result, err := e.executor.Execute(ctx, effect, ExecutionInput{
		Messages:  e.Messages(),
		ToolSpecs: e.toolSpecs(),
	}, chunkFunc)
	if err != nil {
		return err
	}
	e.mu.Lock()
	event := resolveResult(result, e.state.QueuedToolCalls)
	e.setPolicySpecs(e.toolSpecs())
	event = e.policy.Reclassify(event)
	err = e.dispatchLocked(event)
	e.mu.Unlock()
	return err
}

func resolveResult(result ExecutionResult, queue []ToolCall) Event {
	switch r := result.(type) {
	case ModelReplied:
		calls := r.Response.ToolCalls
		if len(calls) > 0 {
			return ToolCallsReceived{
				Content:          r.Response.Content,
				Calls:            calls,
				ReasoningContent: r.Response.ReasoningContent,
			}
		}
		return AssistantMessageReceived{Response: r.Response}
	case ToolFinished:
		return ToolResultReceived{Call: r.Call, Result: r.Result}
	case ToolQueueChecked:
		if len(queue) == 0 {
			return NoMoreToolCalls{}
		}
		return NextToolCallAvailable{Call: queue[0]}
	default:
		return ErrorOccurred{Err: fmt.Errorf("unknown effect result type: %T", r)}
	}
}

func (e *Engine) toolSpecs() []ToolSpec {
	if provider, ok := e.executor.(interface{ ToolSpecs() []ToolSpec }); ok {
		return provider.ToolSpecs()
	}
	return nil
}

func (e *Engine) setPolicySpecs(specs []ToolSpec) {
	if p, ok := e.policy.(*DefaultPolicy); ok {
		p.Specs = specs
	}
}
