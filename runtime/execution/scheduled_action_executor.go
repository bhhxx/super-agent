package execution

import (
	"context"
	"errors"
)

type ScheduledActionInput struct {
	Messages       []Message
	ToolSpecs      []ToolSpec
	ApprovalWaiter ApprovalWaiter
}

// ErrApprovalDismissed reports that the approval waiter gave up waiting — the
// user dismissed the prompt or the interface went away. The engine treats it
// as a cancellation. Any other approval error is a real fault and takes the
// error path, which answers the outstanding tool calls.
var ErrApprovalDismissed = errors.New("approval dismissed")

type ApprovalWaiter interface {
	WaitApproval(context.Context, ToolCall, PermissionRequest) (ApprovalDecision, error)
}

type ApprovalWaitFunc func(context.Context, ToolCall, PermissionRequest) (ApprovalDecision, error)

func (f ApprovalWaitFunc) WaitApproval(ctx context.Context, call ToolCall, request PermissionRequest) (ApprovalDecision, error) {
	return f(ctx, call, request)
}

type ScheduledActionExecutor interface {
	Execute(ctx context.Context, action ScheduledAction, env ScheduledActionInput, chunkFunc func(StreamChunk)) (ScheduledActionResult, error)
}

type DefaultScheduledActionExecutor struct {
	model Model
	tools ToolRunner
}

func NewDefaultScheduledActionExecutor(model Model, tools ToolRunner) *DefaultScheduledActionExecutor {
	return &DefaultScheduledActionExecutor{model: model, tools: tools}
}

func (x *DefaultScheduledActionExecutor) ToolSpecs() []ToolSpec {
	return x.tools.Specs()
}

func (x *DefaultScheduledActionExecutor) Execute(ctx context.Context, action ScheduledAction, env ScheduledActionInput, chunkFunc func(StreamChunk)) (ScheduledActionResult, error) {
	switch fx := action.(type) {
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
	case CheckToolQueue:
		return ToolQueueChecked{}, nil
	case AwaitApproval:
		if env.ApprovalWaiter == nil {
			return nil, errors.New("approval waiter is not configured")
		}
		decision, err := env.ApprovalWaiter.WaitApproval(ctx, fx.Call, fx.Request)
		if err != nil {
			return nil, err
		}
		return ApprovalReceived{Call: fx.Call, Decision: decision}, nil
	default:
		return nil, errors.New("unknown action")
	}
}
