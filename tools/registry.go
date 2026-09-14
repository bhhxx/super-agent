package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"super-agent/runtime/protocol"
)

type Tool interface {
	Spec() protocol.ToolSpec
	Run(ctx context.Context, call protocol.ToolCall) (string, error)
}

type Registry struct {
	mu         sync.RWMutex
	order      []string
	tools      map[string]Tool
	checkpoint func(protocol.ToolCall) error
	observer   func(context.Context, string, protocol.ToolCall, error) error
}

func (r *Registry) SetToolObserver(observer func(context.Context, string, protocol.ToolCall, error) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observer = observer
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

// Add atomically adds dynamically discovered tools. No tool is added when a
// name is empty, duplicated in the batch, or already registered.
func (r *Registry) Add(items ...Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.add(items)
}

func (r *Registry) add(items []Tool) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == nil {
			return errors.New("cannot register nil tool")
		}
		name := item.Spec().Name
		if name == "" {
			return errors.New("cannot register tool with empty name")
		}
		if _, exists := r.tools[name]; exists {
			return fmt.Errorf("tool %q is already registered", name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("tool %q is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	for _, item := range items {
		name := item.Spec().Name
		r.order = append(r.order, name)
		r.tools[name] = item
	}
	return nil
}

func (r *Registry) Remove(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	remove := make(map[string]struct{}, len(names))
	for _, name := range names {
		remove[name] = struct{}{}
		delete(r.tools, name)
	}
	order := r.order[:0]
	for _, name := range r.order {
		if _, exists := remove[name]; !exists {
			order = append(order, name)
		}
	}
	r.order = order
}

// Replace atomically removes old names and registers a replacement batch.
func (r *Registry) Replace(removeNames []string, items ...Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	remove := make(map[string]struct{}, len(removeNames))
	for _, name := range removeNames {
		remove[name] = struct{}{}
	}
	existing := make(map[string]Tool, len(r.tools))
	for name, tool := range r.tools {
		if _, removed := remove[name]; !removed {
			existing[name] = tool
		}
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == nil || item.Spec().Name == "" {
			return errors.New("replacement contains invalid tool")
		}
		name := item.Spec().Name
		if _, found := existing[name]; found {
			return fmt.Errorf("tool %q is already registered", name)
		}
		if _, found := seen[name]; found {
			return fmt.Errorf("tool %q is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	order := r.order[:0]
	for _, name := range r.order {
		if _, removed := remove[name]; !removed {
			order = append(order, name)
		} else {
			delete(r.tools, name)
		}
	}
	for _, item := range items {
		name := item.Spec().Name
		order = append(order, name)
		r.tools[name] = item
	}
	r.order = order
	return nil
}

func (r *Registry) SetCheckpointCallback(callback func(protocol.ToolCall) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkpoint = callback
}

func DefaultRegistry(workspace WorkspaceContext) *Registry {
	return registryWithRunner(nil, workspace)
}

func RegistryForWorkspace(workspace WorkspaceContext) *Registry {
	return DefaultRegistry(workspace)
}

func SandboxedRegistry(config SandboxConfig, workspace WorkspaceContext) (*Registry, error) {
	if workspace == nil {
		return nil, errors.New("workspace is not configured")
	}
	config.Workspace = workspace.GetPrimaryRoot()
	runner, err := newCommandRunner(config, workspace)
	if err != nil {
		return nil, err
	}
	return registryWithRunner(runner, workspace), nil
}

func registryWithRunner(runner *commandRunner, workspace WorkspaceContext) *Registry {
	return NewRegistry(
		ReadFileTool{workspace: workspace},
		ListFilesTool{workspace: workspace},
		SearchTool{workspace: workspace},
		ApplyPatchTool{workspace: workspace},
		WriteFileTool{workspace: workspace},
		RunCommandTool{runner: runner, workspace: workspace},
		GoTestTool{runner: runner, workspace: workspace},
		FormatTool{runner: runner, workspace: workspace},
		GitStatusTool{runner: runner, workspace: workspace},
		GitDiffTool{runner: runner, workspace: workspace},
		BashTool{runner: runner, workspace: workspace},
		WebSearchTool{},
		BrowserFetchTool{},
	)
}

func (r *Registry) Specs() []protocol.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]protocol.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		specs = append(specs, r.tools[name].Spec())
	}
	return specs
}

func (r *Registry) Run(ctx context.Context, call protocol.ToolCall) (result string, err error) {
	r.mu.RLock()
	observer := r.observer
	r.mu.RUnlock()
	if observer != nil {
		if err := observer(ctx, "pre_tool", call, nil); err != nil {
			return "", err
		}
		defer func() { err = errors.Join(err, observer(ctx, "post_tool", call, err)) }()
	}
	return r.RunDirect(ctx, call)
}

// RunDirect is used by lifecycle hooks to avoid recursively invoking hooks.
func (r *Registry) RunDirect(ctx context.Context, call protocol.ToolCall) (result string, err error) {
	r.mu.RLock()
	tool, ok := r.tools[call.Name]
	checkpoint := r.checkpoint
	r.mu.RUnlock()
	if !ok {
		return "", errors.New("unknown tool: " + call.Name)
	}
	if tool.Spec().Risky && checkpoint != nil {
		if err := checkpoint(call); err != nil {
			return "", err
		}
	}
	return tool.Run(ctx, call)
}
