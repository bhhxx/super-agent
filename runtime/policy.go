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

type DefaultPolicy struct{}

func NewDefaultPolicy() *DefaultPolicy {
	return &DefaultPolicy{}
}

func (p *DefaultPolicy) ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision {
	if p.needsApproval(call, input.ToolSpecs) {
		return DecisionNeedsApproval
	}
	return DecisionRunDirectly
}

func (p *DefaultPolicy) needsApproval(call ToolCall, specs []ToolSpec) bool {
	return isRiskyTool(call.Name, specs)
}

func isRiskyTool(name string, specs []ToolSpec) bool {
	for _, s := range specs {
		if s.Name == name {
			return s.Risky
		}
	}
	return true
}
