package runtime

import (
	"context"
	"errors"
	"sync"
)

type Engine struct {
	mu               sync.Mutex
	executor         EffectExecutor
	state            State
	messages         []Message
	pendingTool      *ToolCall
	pendingToolQueue []ToolCall
	pendingEffects   []Effect
	alwaysAllow      map[string]bool
	YOLOMode         bool
}

func toolCallKey(call ToolCall) string {
	return call.Name + ":" + call.Input
}

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	return NewEngineWithExecutor(NewDefaultEffectExecutor(model, tools), initial)
}

func NewEngineWithExecutor(executor EffectExecutor, initial []Message) *Engine {
	messages := append([]Message(nil), initial...)
	return &Engine{
		executor:    executor,
		state:       StateInitializing,
		messages:    messages,
		alwaysAllow: make(map[string]bool),
	}
}

func (e *Engine) EnableYOLO() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.YOLOMode = true
}

func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *Engine) Messages() []Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Message(nil), e.messages...)
}

func (e *Engine) PendingTool() (ToolCall, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pendingTool == nil {
		return ToolCall{}, false
	}
	return *e.pendingTool, true
}

func (e *Engine) snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot := Snapshot{
		State:    e.state,
		Messages: append([]Message(nil), e.messages...),
	}
	if e.pendingTool != nil {
		call := *e.pendingTool
		snapshot.PendingTool = &call
	}
	return snapshot
}

func (e *Engine) Ready() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == StateInitializing {
		e.state = StateIdle
	}
}

func (e *Engine) Approve(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	err := e.dispatchLocked(ApprovalGranted{Call: call})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	return e.runPendingEffects(ctx, chunkFunc)
}

func (e *Engine) ApproveAlways(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	e.alwaysAllow[toolCallKey(call)] = true
	err := e.dispatchLocked(ApprovalGranted{Call: call})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	return e.runPendingEffects(ctx, chunkFunc)
}

func (e *Engine) Deny(ctx context.Context, chunkFunc func(StreamChunk)) error {
	e.mu.Lock()
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		e.mu.Unlock()
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	next, ok := e.nextQueuedToolLocked()
	err := e.dispatchLocked(ApprovalDenied{Call: call, NextCall: next, NextNeedsApproval: ok && e.needsApprovalLocked(*next)})
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

func (e *Engine) runEvent(ctx context.Context, event Event, chunkFunc func(StreamChunk)) error {
	if err := e.dispatch(event); err != nil {
		return err
	}
	return e.runPendingEffects(ctx, chunkFunc)
}

func (e *Engine) runEventWithBeforeEffects(ctx context.Context, event Event, chunkFunc func(StreamChunk), beforeEffects func()) error {
	if err := e.dispatch(event); err != nil {
		return err
	}
	beforeEffects()
	return e.runPendingEffects(ctx, chunkFunc)
}

func (e *Engine) dispatch(event Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dispatchLocked(event)
}

func (e *Engine) dispatchLocked(event Event) error {
	decision, err := Transition(e.state, event)
	if err != nil {
		return err
	}
	e.state = decision.NextState
	for _, mutation := range decision.Mutations {
		e.applyMutation(mutation)
	}
	e.pendingEffects = append(e.pendingEffects, decision.Effects...)
	return nil
}

func (e *Engine) applyMutation(mutation Mutation) {
	switch m := mutation.(type) {
	case AppendUserMessage:
		e.messages = append(e.messages, Message{Role: RoleUser, Content: m.Content})
	case AppendAssistantMessage:
		e.messages = append(e.messages, m.Message)
	case AppendToolResult:
		e.messages = append(e.messages, Message{Role: RoleTool, Content: m.Result, ToolCallID: m.Call.ID})
	case SetPendingTool:
		call := m.Call
		e.pendingTool = &call
	case SetPendingToolQueue:
		e.pendingToolQueue = append([]ToolCall(nil), m.Calls...)
	case ClearPendingTool:
		e.pendingTool = nil
	case ClearPendingToolQueue:
		e.pendingToolQueue = nil
	case ClearPendingEffects:
		e.pendingEffects = nil
	case ResetContext:
		e.messages = nil
		e.pendingTool = nil
		e.pendingToolQueue = nil
		e.pendingEffects = nil
	}
}

func (e *Engine) runPendingEffects(ctx context.Context, chunkFunc func(StreamChunk)) error {
	for {
		e.mu.Lock()
		if len(e.pendingEffects) == 0 {
			e.mu.Unlock()
			return nil
		}
		effect := e.pendingEffects[0]
		e.pendingEffects = e.pendingEffects[1:]
		e.mu.Unlock()
		if err := e.runEffect(ctx, effect, chunkFunc); err != nil {
			if errors.Is(err, context.Canceled) {
				_ = e.dispatch(CancelRequested{})
			} else {
				_ = e.dispatch(ErrorOccurred{})
			}
			return err
		}
	}
}

func (e *Engine) runEffect(ctx context.Context, effect Effect, chunkFunc func(StreamChunk)) error {
	event, err := e.executor.Execute(ctx, effect, EffectContext{
		Messages:       e.Messages(),
		ToolSpecs:      e.toolSpecs(),
		NeedsApproval:  e.needsApproval,
		NextQueuedTool: e.nextQueuedTool,
	}, chunkFunc)
	if err != nil {
		return err
	}
	return e.dispatch(event)
}

func (e *Engine) needsApproval(call ToolCall) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.needsApprovalLocked(call)
}

func (e *Engine) needsApprovalLocked(call ToolCall) bool {
	return call.Risky && !e.alwaysAllow[toolCallKey(call)] && !e.YOLOMode
}

func (e *Engine) nextQueuedTool() (*ToolCall, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nextQueuedToolLocked()
}

func (e *Engine) nextQueuedToolLocked() (*ToolCall, bool) {
	if len(e.pendingToolQueue) == 0 {
		return nil, false
	}
	call := e.pendingToolQueue[0]
	e.pendingToolQueue = e.pendingToolQueue[1:]
	return &call, true
}

func responseToolCalls(resp ModelResponse) []ToolCall {
	if len(resp.ToolCalls) > 0 {
		return append([]ToolCall(nil), resp.ToolCalls...)
	}
	return nil
}

func (e *Engine) toolSpecs() []ToolSpec {
	if provider, ok := e.executor.(interface{ ToolSpecs() []ToolSpec }); ok {
		return provider.ToolSpecs()
	}
	return nil
}
