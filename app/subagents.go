package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"super-agent/llm"
	"super-agent/runtime"
	"super-agent/store"
	"super-agent/tools"
	"super-agent/workspace"
)

const maxSubagentDepth = 3

type subagentDepthKey struct{}

type subagentTool struct {
	parent     *runtime.Session
	repository *store.Repository
	profiles   map[string]AgentProfile
	providers  map[string]llm.ProviderConfig
	sandbox    tools.SandboxConfig
	rules      runtime.PermissionRules
	base       string
	workspace  *workspace.Workspace
	sequence   *atomic.Uint64
}

type subagentInput struct {
	Prompt   string `json:"prompt"`
	Agent    string `json:"agent"`
	Worktree bool   `json:"worktree"`
}

func (t *subagentTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name: "delegate", Description: "Run a task in a child agent and return its final result.", Risky: true,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"prompt": map[string]any{"type": "string"}, "agent": map[string]any{"type": "string"}, "worktree": map[string]any{"type": "boolean"},
		}, "required": []string{"prompt"}},
	}
}

func (t *subagentTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	depth, _ := ctx.Value(subagentDepthKey{}).(int)
	if depth >= maxSubagentDepth {
		return "", errors.New("maximum subagent depth reached")
	}
	var input subagentInput
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return "", fmt.Errorf("decode delegate input: %w", err)
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" {
		return "", errors.New("delegate prompt is required")
	}
	name := firstNonEmpty(strings.TrimSpace(input.Agent), "build")
	profile, ok := t.profiles[name]
	if !ok {
		return "", errors.New("unknown subagent profile: " + name)
	}
	modelConfig := t.providers[profile.Provider]
	if profile.Model != "" {
		modelConfig.Model = profile.Model
	}
	model, err := llm.NewModel(profile.Provider, modelConfig)
	if err != nil {
		return "", err
	}
	cwd := t.base
	if input.Worktree {
		var err error
		cwd, err = t.createWorktree(ctx)
		if err != nil {
			return "", err
		}
	}
	initial, bundle, err := initialMessagesWithAgent(cwd, profile)
	if err != nil {
		return "", err
	}
	memories, err := t.repository.LoadMemory()
	if err != nil {
		return "", err
	}
	if len(memories) > 0 {
		initial = append(initial, runtime.Message{Role: runtime.RoleSystem, Content: "Cross-session memory:\n- " + strings.Join(memories, "\n- ")})
	}
	sandbox := t.sandbox
	sandbox.Workspace = cwd
	childWorkspace, err := workspace.NewDefaultContext(cwd)
	if err != nil {
		return "", err
	}
	childWorkspaceRuntime := workspace.New(childWorkspace)
	registry, err := tools.SandboxedRegistry(sandbox, childWorkspaceRuntime)
	if err != nil {
		return "", err
	}
	childDelegate := *t
	childDelegate.base = cwd
	childDelegate.workspace = childWorkspaceRuntime
	if err := registry.Add(&childDelegate); err != nil {
		return "", err
	}
	filtered := &filteredToolRunner{runner: registry}
	filtered.setAllowed(profile.Tools)
	engine := runtime.NewEngineWithExecutorAndPolicy(runtime.NewDefaultScheduledActionExecutor(model, filtered), runtime.NewPolicy(profile.PermissionMode, t.rules), initial)
	if err := engine.Ready(); err != nil {
		return "", err
	}
	parentMeta := t.parent.Metadata()
	child, err := runtime.CreatePersistentSession(engine, t.repository, childWorkspaceRuntime, runtime.SessionMetadata{
		ParentID: t.parent.Metadata().ID, Provider: profile.Provider, Model: profile.Model, CWD: cwd,
		Title: "subagent: " + truncate(input.Prompt, 48), InstructionSources: instructionSourcePaths(bundle),
		ProjectID: parentMeta.ProjectID, ConfigRoot: cwd,
	}, initial)
	if err != nil {
		return "", err
	}
	defer child.Close()
	notifications := make(chan runtime.SessionNotification, 100)
	approvals := make(chan runtime.ApprovalDecision, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for notification := range notifications {
			if _, ok := notification.(runtime.ToolApprovalRequested); ok {
				approvals <- runtime.DenyApproval
			}
		}
	}()
	err = child.RunTurn(context.WithValue(ctx, subagentDepthKey{}, depth+1), input.Prompt, notifications, approvals)
	<-done
	if err != nil {
		return "", err
	}
	result := finalAssistantContent(child.Snapshot().Messages)
	if result == "" {
		return "", errors.New("subagent returned no assistant result")
	}
	return fmt.Sprintf("Child session: %s\nWorkspace: %s\n\n%s", child.Metadata().ID, cwd, result), nil
}

func (t *subagentTool) createWorktree(ctx context.Context) (string, error) {
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), t.sequence.Add(1))
	if t.workspace == nil {
		return "", errors.New("workspace context is required")
	}
	path, err := t.workspace.ResolvePath(filepath.Join(".super-agent", "worktrees", id))
	if err != nil {
		return "", err
	}
	if !t.workspace.CanWrite(path) {
		return "", errors.New("worktree path is outside writable workspace roots")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	command := fmt.Sprintf("git worktree add --detach %q HEAD", path)
	input, _ := json.Marshal(map[string]any{"command": command, "cwd": t.base, "timeout_seconds": 60})
	registry, err := tools.SandboxedRegistry(t.sandbox, t.workspace)
	if err != nil {
		return "", err
	}
	if _, err := registry.Run(ctx, runtime.ToolCall{Name: "run_command", Input: string(input)}); err != nil {
		return "", fmt.Errorf("create worktree: %w", err)
	}
	return path, nil
}

func finalAssistantContent(messages []runtime.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == runtime.RoleAssistant {
			return strings.TrimSpace(messages[index].Content)
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
