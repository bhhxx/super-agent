package app

import (
	"os"
	"path/filepath"

	"super-agent/app/instructions"
	"super-agent/llm"
	"super-agent/runtime"
	"super-agent/store"
	"super-agent/tools"
	"super-agent/workspace"
)

func NewSession(cfg Config) (*runtime.Session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	model, err := llm.NewModel(cfg.Provider, cfg.ModelConfig) // 模型调用的封装
	if err != nil {
		return nil, err
	}
	var (
		toolRunner runtime.ToolRunner
		registry   *tools.Registry
	)
	if cfg.NoTools {
		toolRunner = tools.NoTools{}
	} else {
		registry = tools.DefaultRegistry()
		toolRunner = runtime.ToolRunner(registry) // 工具调用的封装
	}
	initial, bundle, err := initialMessages(cwd) //
	if err != nil {
		return nil, err
	}
	engine := runtime.NewEngineWithExecutorAndPolicy(runtime.NewDefaultEffectExecutor(model, toolRunner), runtime.NewPolicy(cfg.PermissionMode, cfg.PermissionRules), initial)
	if cfg.AutoApproveTools {
		engine.EnableAutoApproveTools()
	}
	if err := engine.Ready(); err != nil {
		return nil, err
	}
	st, err := store.OpenDefault()
	if err != nil {
		return nil, err
	}
	repository := store.NewRepository(st)
	session, err := runtime.CreatePersistentSession(engine, repository, workspace.Workspace{}, runtime.SessionMetadata{
		Provider: cfg.Provider, Model: cfg.ModelConfig.Model, CWD: cwd,
		Title: filepath.Base(cwd), InstructionSources: instructionSourcePaths(bundle),
	}, initial)
	if err != nil {
		return nil, err
	}
	if registry != nil {
		registry.SetCheckpointCallback(session.Checkpoint)
	}
	session.ConfigurePermissions(cfg.PermissionMode, cfg.PermissionRules)
	return session, nil
}

func initialMessages(cwd string) ([]runtime.Message, instructions.Bundle, error) {
	bundle, err := instructions.Load(cwd)
	if err != nil {
		return nil, instructions.Bundle{}, err
	}
	content := SystemPrompt // `:=` 是短变量声明，系统提示词
	if bundle.Content != "" {
		content += "\n\n" + bundle.Content // 拼接 AGENTS.md 这类系统提示词
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
