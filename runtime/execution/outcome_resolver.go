package execution

import (
	"errors"
	"fmt"
)

type OutcomeResolver interface {
	Resolve(result ExecutionResult, input OutcomeResolveInput) (Event, error)
}

type OutcomeResolveInput struct {
	ToolBatch *ToolCallBatch
	ToolSpecs []ToolSpec
}

type DefaultOutcomeResolver struct {
	policy    Policy
	approvals ApprovalStore
}

func NewDefaultOutcomeResolver(policy Policy, approvals ApprovalStore) *DefaultOutcomeResolver {
	return &DefaultOutcomeResolver{policy: policy, approvals: approvals}
}

func (r *DefaultOutcomeResolver) SetPolicy(policy Policy) {
	r.policy = policy
}

func (r *DefaultOutcomeResolver) Resolve(result ExecutionResult, input OutcomeResolveInput) (Event, error) {
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
	default:
		return nil, fmt.Errorf("unknown effect result type: %T", result)
	}
}

func (r *DefaultOutcomeResolver) resolveToolCall(call ToolCall, specs []ToolSpec) (Event, error) {
	decision := r.decision(call, specs)
	if decision == DecisionDenied {
		req := r.policy.PermissionRequest(call, ToolPolicyInput{ToolSpecs: specs})
		return nil, errors.New("tool denied by permission policy: " + req.Reason)
	}
	if decision == DecisionRunDirectly {
		return ToolCallReadyToRun{Call: call}, nil
	}
	return ToolCallNeedsApproval{
		Call:    call,
		Request: r.policy.PermissionRequest(call, ToolPolicyInput{ToolSpecs: specs}),
	}, nil
}

func (r *DefaultOutcomeResolver) decision(call ToolCall, specs []ToolSpec) ToolDecision {
	if r.approvals.AutoApproveTools() || r.approvals.IsAlwaysAllowed(NewApprovalKey(call)) {
		return DecisionRunDirectly
	}
	return r.policy.ClassifyToolCall(call, ToolPolicyInput{ToolSpecs: specs})
}
