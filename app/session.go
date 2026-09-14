package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"super-agent/app/instructions"
	"super-agent/llm"
	"super-agent/runtime"
	"super-agent/runtime/telemetry"
	"super-agent/store"
	"super-agent/tools"
	lsptools "super-agent/tools/lsp"
	mcptools "super-agent/tools/mcp"
	"super-agent/workspace"
)

func NewSession(cfg Config) (*runtime.Session, error) {
	session, _, err := NewSessionWithMCP(cfg)
	return session, err
}

func NewSessionWithMCP(cfg Config) (*runtime.Session, *MCPController, error) {
	session, mcp, _, err := NewSessionWithExtensions(cfg)
	return session, mcp, err
}

func NewSessionWithExtensions(cfg Config) (*runtime.Session, *MCPController, *AgentController, error) {
	workspaceContext, err := contextForConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	cwd := workspaceContext.GetCWD()
	configRoot := firstNonEmpty(cfg.ConfigRoot, cfg.Project.Root, cwd)
	workspaceRuntime := workspace.New(workspaceContext)
	cfg.Sandbox.Workspace = workspaceContext.GetPrimaryRoot()
	if err := telemetry.Configure(cfg.TelemetryPath); err != nil {
		return nil, nil, nil, err
	}
	telemetryOwned := true
	defer func() {
		if telemetryOwned {
			_ = telemetry.Close()
		}
	}()
	providers := cfg.ProviderConfigs
	if len(providers) == 0 {
		providers = map[string]llm.ProviderConfig{cfg.Provider: cfg.ModelConfig}
	}
	profiles, err := buildAgentProfiles(cfg, providers)
	if err != nil {
		return nil, nil, nil, err
	}
	agentName := firstNonEmpty(cfg.Agent, "build")
	profile, ok := profiles[agentName]
	if !ok {
		return nil, nil, nil, fmt.Errorf("unknown configured agent: %s", agentName)
	}
	providerConfig := providers[profile.Provider]
	if profile.Model != "" {
		providerConfig.Model = profile.Model
	}
	model, err := llm.NewModel(profile.Provider, providerConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	router := &routedModel{model: model}
	var (
		toolRunner runtime.ToolRunner
		registry   *tools.Registry
		extension  io.Closer
		lspCloser  io.Closer
		controller *MCPController
		toolFilter *filteredToolRunner
	)
	defer func() {
		if extension != nil {
			_ = extension.Close()
		}
		if lspCloser != nil {
			_ = lspCloser.Close()
		}
	}()
	if cfg.NoTools {
		toolRunner = tools.NoTools{}
	} else {
		registry, err = tools.SandboxedRegistry(cfg.Sandbox, workspaceRuntime)
		if err != nil {
			return nil, nil, nil, err
		}
		manager, connectErr := mcptools.Connect(context.Background(), cfg.MCPServers)
		if connectErr != nil {
			return nil, nil, nil, connectErr
		}
		if addErr := registry.Add(manager.Tools()...); addErr != nil {
			_ = manager.Close()
			return nil, nil, nil, addErr
		}
		settingsPath, pathErr := SettingsPath()
		if pathErr != nil {
			_ = manager.Close()
			return nil, nil, nil, pathErr
		}
		controller = NewMCPController(manager, registry, settingsPath, cwd, settingsMap(cfg.MCPServers))
		// The runtime session owns extension process lifetime after creation.
		extension = manager
		if len(cfg.LSPServers) > 0 {
			lspManager, connectErr := lsptools.Connect(context.Background(), workspaceRuntime, cfg.LSPServers)
			if connectErr != nil {
				return nil, nil, nil, connectErr
			}
			for _, lspTool := range lspManager.Tools() {
				if addErr := registry.Add(lspTool); addErr != nil {
					_ = lspManager.Close()
					return nil, nil, nil, addErr
				}
			}
			lspCloser = lspManager
		}
		toolFilter = &filteredToolRunner{runner: registry}
		toolFilter.setAllowed(profile.Tools)
		toolRunner = toolFilter
	}
	initial, bundle, err := initialMessagesWithAgent(configRoot, profile)
	if err != nil {
		return nil, nil, nil, err
	}
	st, err := store.OpenDefault()
	if err != nil {
		return nil, nil, nil, err
	}
	repository := store.NewRepository(st)
	memories, err := repository.LoadMemory()
	if err != nil {
		return nil, nil, nil, err
	}
	if len(memories) > 0 {
		memory := runtime.Message{Role: runtime.RoleSystem, Content: "Cross-session memory:\n- " + strings.Join(memories, "\n- ")}
		initial = append(initial, memory)
	}
	engine := runtime.NewEngineWithExecutorAndPolicy(runtime.NewDefaultScheduledActionExecutor(router, toolRunner), runtime.NewPolicy(profile.PermissionMode, cfg.PermissionRules), initial)
	if err := engine.Ready(); err != nil {
		return nil, nil, nil, err
	}
	session, err := runtime.CreatePersistentSession(engine, repository, workspaceRuntime, runtime.SessionMetadata{
		Provider: profile.Provider, Model: profile.Model, CWD: cwd,
		Title: filepath.Base(cwd), InstructionSources: instructionSourcePaths(bundle),
		ProjectID: cfg.Project.ID, ConfigRoot: configRoot,
	}, initial)
	if err != nil {
		return nil, nil, nil, err
	}
	if registry != nil {
		registry.SetCheckpointCallback(session.Checkpoint)
		delegate := &subagentTool{parent: session, repository: repository, profiles: profiles, providers: providers, sandbox: cfg.Sandbox, rules: cfg.PermissionRules, base: cwd, workspace: workspaceRuntime, sequence: &atomic.Uint64{}}
		if err := registry.Add(delegate); err != nil {
			_ = session.Close()
			return nil, nil, nil, err
		}
	}
	session.ConfigurePermissions(profile.PermissionMode, cfg.PermissionRules)
	if extension != nil {
		session.AddCloser(extension)
		extension = nil
	}
	if lspCloser != nil {
		session.AddCloser(lspCloser)
		lspCloser = nil
	}
	session.AddCloser(closerFunc(telemetry.Close))
	telemetryOwned = false
	workflows := &WorkflowController{registry: registry, extensions: cfg.Extensions}
	if registry != nil {
		registry.SetToolObserver(func(ctx context.Context, event string, _ runtime.ToolCall, _ error) error {
			return workflows.RunHook(ctx, event)
		})
	}
	if err := workflows.RunHooks(context.Background(), "session_start", "startup"); err != nil {
		_ = session.Close()
		return nil, nil, nil, err
	}
	agents := &AgentController{session: session, model: router, profiles: profiles, providers: providers, workflows: workflows, tools: toolFilter, base: cwd, current: profile.Name}
	return session, controller, agents, nil
}

func contextForConfig(cfg Config) (*workspace.Context, error) {
	if cfg.Workspace != nil {
		return cfg.Workspace, nil
	}
	return nil, errors.New("workspace context is required")
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func settingsMap(configs []mcptools.ServerConfig) map[string]MCPServerSettings {
	result := make(map[string]MCPServerSettings, len(configs))
	for _, config := range configs {
		result[config.Name] = MCPServerSettings{
			Command: config.Command, Args: config.Args, Env: config.Env, CWD: config.CWD,
			ConnectTimeoutSeconds: int(config.ConnectTimeout / time.Second),
			CallTimeoutSeconds:    int(config.CallTimeout / time.Second),
		}
	}
	return result
}

// initialMessages loads the instruction bundle for cwd and merges it into the
// system message. The bundle is returned so session metadata can record where
// the instructions came from.
func initialMessages(cwd string) ([]runtime.Message, instructions.Bundle, error) {
	bundle, err := instructions.Load(cwd)
	if err != nil {
		return nil, instructions.Bundle{}, err
	}
	content := SystemPrompt
	if bundle.Content != "" {
		content += "\n\n" + bundle.Content
	}
	return []runtime.Message{{Role: runtime.RoleSystem, Content: content}}, bundle, nil
}

func instructionSourcePaths(bundle instructions.Bundle) []string {
	paths := make([]string, 0, len(bundle.Sources))
	for _, source := range bundle.Sources {
		paths = append(paths, source.Path)
	}
	return paths
}
