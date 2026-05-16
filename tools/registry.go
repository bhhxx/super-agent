package tools

import (
	"context"
	"errors"

	"super-agent/runtime"
)

type Tool interface {
	Spec() runtime.ToolSpec
	Run(ctx context.Context, call runtime.ToolCall) (string, error)
}

type Registry struct {
	order []string
	tools map[string]Tool
}

func NewRegistry(items ...Tool) *Registry {
	registry := &Registry{
		tools: make(map[string]Tool, len(items)),
	}
	for _, item := range items {
		name := item.Spec().Name
		registry.order = append(registry.order, name)
		registry.tools[name] = item
	}
	return registry
}

func (r *Registry) Specs() []runtime.ToolSpec {
	specs := make([]runtime.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		specs = append(specs, r.tools[name].Spec())
	}
	return specs
}

func (r *Registry) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		return "", errors.New("unknown tool: " + call.Name)
	}
	return tool.Run(ctx, call)
}
