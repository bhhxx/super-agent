package execution

import "errors"

type EventClassifier interface {
	Classify(event Event, input EventClassifyInput) (Event, error)
}

type EventClassifyInput struct {
	ToolSpecs []ToolSpec
}

type DefaultEventClassifier struct {
	policy    Policy
	approvals ApprovalStore
}

func NewDefaultEventClassifier(policy Policy, approvals ApprovalStore) *DefaultEventClassifier {
	return &DefaultEventClassifier{policy: policy, approvals: approvals}
}

func (c *DefaultEventClassifier) SetPolicy(policy Policy) {
	c.policy = policy
}

func (c *DefaultEventClassifier) Classify(event Event, input EventClassifyInput) (Event, error) {
	switch ev := event.(type) {
	case ToolCallsReceived:
		if len(ev.Calls) == 0 {
			return nil, errors.New("empty tool calls")
		}
		if len(input.ToolSpecs) == 0 {
			return nil, errors.New("model returned tool call while tools are disabled")
		}
		return ToolBatchReceived{
			Content:          ev.Content,
			Calls:            ev.Calls,
			ReasoningContent: ev.ReasoningContent,
		}, nil
	case ToolCallAvailable:
		decision := c.decision(ev.Call, input.ToolSpecs)
		if decision == DecisionDenied {
			req := c.policy.PermissionRequest(ev.Call, ToolPolicyInput{ToolSpecs: input.ToolSpecs})
			return nil, errors.New("tool denied by permission policy: " + req.Reason)
		}
		if decision == DecisionRunDirectly {
			return ToolCallReadyToRun{Call: ev.Call}, nil
		}
		return ToolCallNeedsApproval{Call: ev.Call, Request: c.policy.PermissionRequest(ev.Call, ToolPolicyInput{ToolSpecs: input.ToolSpecs})}, nil
	default:
		return event, nil
	}
}

func (c *DefaultEventClassifier) decision(call ToolCall, specs []ToolSpec) ToolDecision {
	if c.approvals.AutoApproveTools() || c.approvals.IsAlwaysAllowed(NewApprovalKey(call)) {
		return DecisionRunDirectly
	}
	return c.policy.ClassifyToolCall(call, ToolPolicyInput{ToolSpecs: specs})
}
