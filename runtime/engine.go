package runtime

import (
	"context"
	"errors"
)

type Engine struct {
	model          Model
	tools          ToolRunner
	state          State
	messages       []Message
	pendingTool    *ToolCall
	pendingEffects []Effect
	alwaysAllow    map[string]bool
	YOLOMode       bool
}

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	messages := append([]Message(nil), initial...)
	return &Engine{
		model:       model,
		tools:       tools,
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

func (e *Engine) SubmitUserMessage(ctx context.Context, content string, chunkFunc func(StreamChunk)) error {
	if err := e.BeginUserMessage(content); err != nil {
		return err
	}
	return e.Continue(ctx, chunkFunc)
}

func (e *Engine) BeginUserMessage(content string) error {
	return e.dispatch(UserMessageSubmitted{Content: content})
}

func (e *Engine) Continue(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingLLM || len(e.pendingEffects) == 0 {
		return errors.New("runtime is not waiting for llm")
	}
	return e.drainEffects(ctx, chunkFunc)
}

func (e *Engine) Approve(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	if err := e.dispatch(ApprovalGranted{Call: call}); err != nil {
		return err
	}
	return e.drainEffects(ctx, chunkFunc)
}

func (e *Engine) ApproveAlways(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	e.alwaysAllow[call.Name] = true
	if err := e.dispatch(ApprovalGranted{Call: call}); err != nil {
		return err
	}
	return e.drainEffects(ctx, chunkFunc)
}

func (e *Engine) Deny(ctx context.Context, chunkFunc func(StreamChunk)) error {
	if e.state != StateWaitingApproval || e.pendingTool == nil {
		return errors.New("no tool is waiting for approval")
	}
	call := *e.pendingTool
	if err := e.dispatch(ApprovalDenied{Call: call}); err != nil {
		return err
	}
	return e.drainEffects(ctx, chunkFunc)
}

func (e *Engine) Cancel() error {
	return e.dispatch(CancelRequested{})
}

func (e *Engine) Reset() {
	_ = e.dispatch(ResetRequested{})
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
	case ClearPendingTool:
		e.pendingTool = nil
	case ClearPendingEffects:
		e.pendingEffects = nil
	case ResetContext:
		e.messages = nil
		e.pendingTool = nil
		e.pendingEffects = nil
	}
}

func (e *Engine) drainEffects(ctx context.Context, chunkFunc func(StreamChunk)) error {
	for len(e.pendingEffects) > 0 {
		effect := e.pendingEffects[0]
		e.pendingEffects = e.pendingEffects[1:]
		if err := e.executeEffect(ctx, effect, chunkFunc); err != nil {
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

func (e *Engine) executeEffect(ctx context.Context, effect Effect, chunkFunc func(StreamChunk)) error {
	switch fx := effect.(type) {
	case CallModel:
		resp, err := e.model.Next(ctx, e.Messages(), e.tools.Specs(), chunkFunc)
		if err != nil {
			return err
		}
		if resp.ToolCall != nil {
			call := *resp.ToolCall
			return e.dispatch(ToolCallRequested{
				Call:             call,
				ReasoningContent: resp.ReasoningContent,
				NeedsApproval:    call.Risky && !e.alwaysAllow[call.Name] && !e.YOLOMode,
			})
		}
		return e.dispatch(AssistantMessageReceived{Response: resp})
	case RunTool:
		result, err := e.tools.Run(ctx, fx.Call)
		if err != nil {
			return err
		}
		return e.dispatch(ToolResultReceived{Call: fx.Call, Result: result})
	default:
		return errors.New("unknown effect")
	}
}
