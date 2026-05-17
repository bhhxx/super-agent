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

type ApprovalConfigurator interface {
	AddAlwaysAllow(key string)
	SetAutoApproveTools(enabled bool)
}

type DefaultPolicy struct {
	AlwaysAllow      map[string]bool
	AutoApproveTools bool
}

func NewDefaultPolicy() *DefaultPolicy {
	return &DefaultPolicy{AlwaysAllow: make(map[string]bool)}
}

func (p *DefaultPolicy) ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision {
	if p.needsApproval(call, input.ToolSpecs) {
		return DecisionNeedsApproval
	}
	return DecisionRunDirectly
}

func (p *DefaultPolicy) AddAlwaysAllow(key string) {
	if p.AlwaysAllow == nil {
		p.AlwaysAllow = make(map[string]bool)
	}
	p.AlwaysAllow[key] = true
}

func (p *DefaultPolicy) SetAutoApproveTools(enabled bool) {
	p.AutoApproveTools = enabled
}

func (p *DefaultPolicy) needsApproval(call ToolCall, specs []ToolSpec) bool {
	risky := isRiskyTool(call.Name, specs)
	return risky && !p.AlwaysAllow[toolCallKey(call)] && !p.AutoApproveTools
}

func isRiskyTool(name string, specs []ToolSpec) bool {
	for _, s := range specs {
		if s.Name == name {
			return s.Risky
		}
	}
	return true
}
