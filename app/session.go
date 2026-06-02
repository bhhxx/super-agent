package app

import (
	"os"
	"strings"

	"super-agent/llm"
	"super-agent/runtime"
	"super-agent/tools"
)

func NewSession(cfg Config) (*runtime.Session, error) {
	model, err := llm.NewModel(cfg.Provider)
	if err != nil {
		return nil, err
	}
	toolRunner := runtime.ToolRunner(tools.NewRegistry(tools.BashTool{}))
	if cfg.NoTools {
		toolRunner = tools.NoTools{}
	}
	initial, err := initialMessages()
	if err != nil {
		return nil, err
	}
	engine := runtime.NewEngine(model, toolRunner, initial)
	if cfg.AutoApproveTools {
		engine.EnableAutoApproveTools()
	}
	if err := engine.Ready(); err != nil {
		return nil, err
	}
	return runtime.NewSession(engine), nil
}

func initialMessages() ([]runtime.Message, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	instructions, err := LoadProjectInstructions(cwd)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(SystemPrompt)
	if instructions != "" {
		if content != "" {
			content += "\n\n"
		}
		content += strings.TrimSpace(instructions)
	}
	if content == "" {
		return nil, nil
	}
	return []runtime.Message{{Role: runtime.RoleSystem, Content: content}}, nil
}
