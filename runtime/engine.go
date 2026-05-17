package runtime

import (
	"context"
	"errors"
	"sync"
)

type Engine struct {
	mu       sync.Mutex
	executor EffectExecutor
	state    EngineState
}

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	return NewEngineWithExecutor(NewDefaultEffectExecutor(model, tools), initial)
}

func NewEngineWithExecutor(executor EffectExecutor, initial []Message) *Engine {
	messages := append([]Message(nil), initial...)
	return &Engine{
		executor: executor,
		state: EngineState{
			State:       StateInitializing,
			Messages:    messages,
			AlwaysAllow: make(map[string]bool),
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
		e.applyMutation(m)
	}
	e.state.PendingEffects = append(e.state.PendingEffects, decision.Effects...)
	return nil
}

func (e *Engine) applyMutation(mutation Mutation) {
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
		e.state.PendingEffects = nil
	case ResetContext:
		// Reset conversation context but preserve user preferences.
		e.state.Messages = nil
		e.state.PendingTool = nil
		e.state.QueuedToolCalls = nil
		e.state.PendingEffects = nil
	case AddAlwaysAllow:
		if e.state.AlwaysAllow == nil {
			e.state.AlwaysAllow = make(map[string]bool)
		}
		e.state.AlwaysAllow[m.Key] = true
	case SetAutoApproveTools:
		e.state.AutoApproveTools = m.Enabled
	}
}

func (e *Engine) runPendingEffects(ctx context.Context, chunkFunc func(StreamChunk)) error {
	for {
		e.mu.Lock()
		if len(e.state.PendingEffects) == 0 {
			e.mu.Unlock()
			return nil
		}
		effect := e.state.PendingEffects[0]
		e.state.PendingEffects = e.state.PendingEffects[1:]
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
	event := e.resolveResultLocked(result)
	err = e.dispatchLocked(event)
	e.mu.Unlock()
	return err
}

func (e *Engine) resolveResultLocked(result ExecutionResult) Event {
	switch r := result.(type) {
	case ModelReplied:
		calls := MarkRiskyToolCalls(r.Response.ToolCalls, e.toolSpecs())
		if len(calls) > 0 {
			first := calls[0]
			if e.needsApproval(first) {
				return ToolCallsBlockedForApproval{
					Content:          r.Response.Content,
					Calls:            calls,
					ReasoningContent: r.Response.ReasoningContent,
				}
			}
			return ToolCallsApprovedToRun{
				Content:          r.Response.Content,
				Calls:            calls,
				ReasoningContent: r.Response.ReasoningContent,
			}
		}
		return AssistantMessageReceived{Response: r.Response}
	case ToolFinished:
		return ToolResultReceived{Call: r.Call, Result: r.Result}
	case ToolQueueChecked:
		if len(e.state.QueuedToolCalls) == 0 {
			return NoMoreToolCalls{}
		}
		call := e.state.QueuedToolCalls[0]
		if e.needsApproval(call) {
			return NextToolCallNeedsApproval{Call: call}
		}
		return NextToolCallReadyToRun{Call: call}
	default:
		return ErrorOccurred{Err: errors.New("unknown effect result type")}
	}
}

func (e *Engine) needsApproval(call ToolCall) bool {
	return call.Risky && !e.state.AlwaysAllow[toolCallKey(call)] && !e.state.AutoApproveTools
}

func (e *Engine) toolSpecs() []ToolSpec {
	if provider, ok := e.executor.(interface{ ToolSpecs() []ToolSpec }); ok {
		return provider.ToolSpecs()
	}
	return nil
}
