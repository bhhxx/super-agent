package runtime

type ToolDecision int

const (
	DecisionNeedsApproval ToolDecision = iota
	DecisionRunDirectly
)

type ToolPolicyInput struct {
	ToolSpecs []ToolSpec
}

type Policy interface {
	ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision
}

type DefaultPolicy struct {
	approvals ApprovalStore
}

func NewDefaultPolicy(approvals ApprovalStore) *DefaultPolicy {
	return &DefaultPolicy{approvals: approvals}
}

func (p *DefaultPolicy) ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision {
	if p.needsApproval(call, input.ToolSpecs) {
		return DecisionNeedsApproval
	}
	return DecisionRunDirectly
}

func (p *DefaultPolicy) needsApproval(call ToolCall, specs []ToolSpec) bool {
	risky := isRiskyTool(call.Name, specs)
	if !risky {
		return false
	}
	return !p.approvals.AutoApproveTools() && !p.approvals.IsAlwaysAllowed(NewApprovalKey(call))
}

func isRiskyTool(name string, specs []ToolSpec) bool {
	for _, s := range specs {
		if s.Name == name {
			return s.Risky
		}
	}
	return true
}
