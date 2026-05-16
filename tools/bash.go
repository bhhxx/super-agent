package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

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
	if command == "" {
		return "", errors.New("invalid bash command input: must be JSON with 'command' field")
	}
	output, err := exec.CommandContext(ctx, "bash", "-lc", command).CombinedOutput()
	if err == nil {
		return string(output), nil
	}
	if ctx.Err() != nil {
		return string(output), ctx.Err()
	}
	if _, ok := err.(*exec.ExitError); ok {
		return failedCommandResult(output, err), nil
	}
	return string(output), err
}

func bashCommand(input string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(input), &args); err == nil {
		return args.Command
	}
	return ""
}

func failedCommandResult(output []byte, err error) string {
	result := string(output)
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result + err.Error()
}
