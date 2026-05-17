package runtime

import "errors"

type Policy interface {
	Reclassify(event Event) Event
}

type DefaultPolicy struct {
	AlwaysAllow      map[string]bool
	AutoApproveTools bool
	Specs            []ToolSpec
}

func NewDefaultPolicy() *DefaultPolicy {
	return &DefaultPolicy{AlwaysAllow: make(map[string]bool)}
}

func (p *DefaultPolicy) Reclassify(event Event) Event {
	switch ev := event.(type) {
	case ToolCallsReceived:
		if len(ev.Calls) == 0 {
			return ErrorOccurred{Err: errors.New("empty tool calls")}
		}
		first := ev.Calls[0]
		if p.needsApproval(first) {
			return ToolCallsBlockedForApproval{
				Content:          ev.Content,
				Calls:            ev.Calls,
				ReasoningContent: ev.ReasoningContent,
			}
		}
		return ToolCallsApprovedToRun{
			Content:          ev.Content,
			Calls:            ev.Calls,
			ReasoningContent: ev.ReasoningContent,
		}
	case NextToolCallAvailable:
		if p.needsApproval(ev.Call) {
			return NextToolCallNeedsApproval{Call: ev.Call}
		}
		return NextToolCallReadyToRun{Call: ev.Call}
	default:
		return event
	}
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

func (p *DefaultPolicy) needsApproval(call ToolCall) bool {
	risky := p.isRiskyTool(call.Name)
	return risky && !p.AlwaysAllow[toolCallKey(call)] && !p.AutoApproveTools
}

func (p *DefaultPolicy) isRiskyTool(name string) bool {
	for _, s := range p.Specs {
		if s.Name == name {
			return s.Risky
		}
	}
	return false
}
