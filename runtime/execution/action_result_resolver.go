package execution

import (
	"errors"
	"fmt"
)

type ActionResultResolver interface {
	Resolve(result ScheduledActionResult, input ActionResultInput) (Event, error)
}

type ActionResultInput struct {
	ToolBatch *ToolCallBatch
	ToolSpecs []ToolSpec
}

type DefaultActionResultResolver struct {
	policy    Policy
	approvals ApprovalStore
}

func NewDefaultActionResultResolver(policy Policy, approvals ApprovalStore) *DefaultActionResultResolver {
	return &DefaultActionResultResolver{policy: policy, approvals: approvals}
}

func (r *DefaultActionResultResolver) SetPolicy(policy Policy) {
	r.policy = policy
}

func (r *DefaultActionResultResolver) Resolve(result ScheduledActionResult, input ActionResultInput) (Event, error) {
	switch result := result.(type) {
	case ModelReplied:
		if len(result.Response.ToolCalls) == 0 {
			return AssistantMessageReceived{Response: result.Response}, nil
		}
		if len(input.ToolSpecs) == 0 {
			return nil, errors.New("model returned tool call while tools are disabled")
		}
		return ToolBatchReceived{
			Content:          result.Response.Content,
			Calls:            result.Response.ToolCalls,
			ReasoningContent: result.Response.ReasoningContent,
		}, nil
	case ToolFinished:
		return ToolResultReceived{Call: result.Call, Result: result.Result}, nil
	case ToolQueueChecked:
		if input.ToolBatch == nil || input.ToolBatch.Index >= len(input.ToolBatch.Calls) {
			return ToolBatchFinished{}, nil
		}
		return r.resolveToolCall(input.ToolBatch.Calls[input.ToolBatch.Index], input.ToolSpecs)
	case ApprovalReceived:
		switch result.Decision {
		case ApproveOnce:
			return ApprovalGranted{Call: result.Call}, nil
		case ApproveAlways:
			return ApprovalAlwaysGranted{Call: result.Call}, nil
		case DenyApproval:
			return ApprovalDenied{Call: result.Call}, nil
		default:
			return nil, errors.New("unknown approval decision")
		}
	default:
		return nil, fmt.Errorf("unknown action result type: %T", result)
	}
}

func (r *DefaultActionResultResolver) resolveToolCall(call ToolCall, specs []ToolSpec) (Event, error) {
	decision := r.decision(call, specs)
	if decision == DecisionDenied {
		req := r.policy.PermissionRequest(call, ToolPolicyInput{ToolSpecs: specs})
		return ToolCallDenied{Call: call, Reason: req.Reason}, nil
	}
	if decision == DecisionRunDirectly {
		return ToolCallReadyToRun{Call: call}, nil
	}
	return ToolCallNeedsApproval{
		Call:    call,
		Request: r.policy.PermissionRequest(call, ToolPolicyInput{ToolSpecs: specs}),
	}, nil
}

func (r *DefaultActionResultResolver) decision(call ToolCall, specs []ToolSpec) ToolDecision {
	if r.approvals.IsAlwaysAllowed(NewApprovalKey(call)) {
		return DecisionRunDirectly
	}
	return r.policy.ClassifyToolCall(call, ToolPolicyInput{ToolSpecs: specs})
}
