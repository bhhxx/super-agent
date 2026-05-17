package app

import (
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
	engine := runtime.NewEngine(model, toolRunner, nil)
	if cfg.AutoApproveTools {
		engine.EnableAutoApproveTools()
	}
	if err := engine.Ready(); err != nil {
		return nil, err
	}
	return runtime.NewSession(engine), nil
}
