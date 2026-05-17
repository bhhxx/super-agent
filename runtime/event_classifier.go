package runtime

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

func (c *DefaultEventClassifier) Classify(event Event, input EventClassifyInput) (Event, error) {
	switch ev := event.(type) {
	case ToolCallsReceived:
		if len(ev.Calls) == 0 {
			return nil, errors.New("empty tool calls")
		}
		if len(input.ToolSpecs) == 0 {
			return nil, errors.New("model returned tool call while tools are disabled")
		}
		if c.shouldApprove(ev.Calls[0], input.ToolSpecs) {
			return ToolCallBatchFirstReadyToRun{
				Content:          ev.Content,
				Calls:            ev.Calls,
				ReasoningContent: ev.ReasoningContent,
			}, nil
		}
		return ToolCallBatchFirstNeedsApproval{
			Content:          ev.Content,
			Calls:            ev.Calls,
			ReasoningContent: ev.ReasoningContent,
		}, nil
	case NextToolCallAvailable:
		if c.shouldApprove(ev.Call, input.ToolSpecs) {
			return QueuedToolCallReadyToRun{Call: ev.Call}, nil
		}
		return QueuedToolCallNeedsApproval{Call: ev.Call}, nil
	default:
		return event, nil
	}
}

func (c *DefaultEventClassifier) shouldApprove(call ToolCall, specs []ToolSpec) bool {
	if c.approvals.AutoApproveTools() || c.approvals.IsAlwaysAllowed(NewApprovalKey(call)) {
		return true
	}
	return c.policy.ClassifyToolCall(call, ToolPolicyInput{ToolSpecs: specs}) == DecisionRunDirectly
}
