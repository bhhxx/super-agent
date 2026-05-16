package runtime

import (
	"context"
	"errors"
)

type Engine struct {
	executor         EffectExecutor
	state            State
	messages         []Message
	pendingTool      *ToolCall
	pendingToolQueue []ToolCall
	pendingEffects   []Effect
	alwaysAllow      map[string]bool
	YOLOMode         bool
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
	e.YOLOMode = true
}

func (e *Engine) State() State {
	return e.state
}

func (e *Engine) Messages() []Message {
	return append([]Message(nil), e.messages...)
}

func (e *Engine) PendingTool() (ToolCall, bool) {
	if e.pendingTool == nil {
		return ToolCall{}, false
	}
	return *e.pendingTool, true
}

func (e *Engine) Ready() {
	if e.state == StateInitializing {
		e.state = StateIdle
	}
}

func (e *Engine) Approve(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	return e.runEvent(ctx, ApprovalGranted{Call: call}, chunkFunc)
}

func (e *Engine) ApproveAlways(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	e.alwaysAllow[call.Name] = true
	return e.runEvent(ctx, ApprovalGranted{Call: call}, chunkFunc)
}

func (e *Engine) Deny(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	next, ok := e.nextQueuedTool()
	return e.runEvent(ctx, ApprovalDenied{Call: call, NextCall: next, NextNeedsApproval: ok && e.needsApproval(*next)}, chunkFunc)
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
	for len(e.pendingEffects) > 0 {
		effect := e.pendingEffects[0]
		e.pendingEffects = e.pendingEffects[1:]
		if err := e.runEffect(ctx, effect, chunkFunc); err != nil {
			if errors.Is(err, context.Canceled) {
				_ = e.dispatch(CancelRequested{})
			} else {
				_ = e.dispatch(ErrorOccurred{})
			}
			return err
		}
	}
	return nil
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
	return call.Risky && !e.alwaysAllow[call.Name] && !e.YOLOMode
}

func (e *Engine) nextQueuedTool() (*ToolCall, bool) {
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
