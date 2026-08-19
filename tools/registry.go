package tools

import (
	"context"
	"errors"

	runtime "super-agent/runtime/protocol"
)

type Tool interface {
	Spec() runtime.ToolSpec
	Run(ctx context.Context, call runtime.ToolCall) (string, error)
}

type Registry struct {
	order      []string
	tools      map[string]Tool
	checkpoint func(runtime.ToolCall) error
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

func (r *Registry) SetCheckpointCallback(callback func(runtime.ToolCall) error) {
	r.checkpoint = callback
}

func DefaultRegistry() *Registry {
	return NewRegistry(
		ReadFileTool{},
		ListFilesTool{},
		SearchTool{},
		ApplyPatchTool{},
		WriteFileTool{},
		RunCommandTool{},
		GoTestTool{},
		FormatTool{},
		GitStatusTool{},
		GitDiffTool{},
		BashTool{},
	)
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
	if tool.Spec().Risky && r.checkpoint != nil {
		if err := r.checkpoint(call); err != nil {
			return "", err
		}
	}
	return tool.Run(ctx, call)
}
