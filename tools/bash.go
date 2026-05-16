package tools

import (
	"context"
	"encoding/json"
	"os/exec"

	"super-agent/runtime"
)

type BashTool struct{}

func NewBashTools() *Registry {
	return NewRegistry(BashTool{})
}

func (BashTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "bash",
		Description: "Run a bash command after user approval.",
		Risky:       true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		},
	}
}

func (BashTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	command := bashCommand(call.Input)
	output, err := exec.CommandContext(ctx, "bash", "-lc", command).CombinedOutput()
	return string(output), err
}

func bashCommand(input string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(input), &args); err == nil && args.Command != "" {
		return args.Command
	}
	return input
}
