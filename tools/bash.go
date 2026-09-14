package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"

	"super-agent/runtime/protocol"
)

type BashTool struct {
	runner    *commandRunner
	workspace WorkspaceContext
}

func (BashTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
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

func (t BashTool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
	command := bashCommand(call.Input)
	if command == "" {
		return "", errors.New("invalid bash command input: must be JSON with 'command' field")
	}
	// Routed through runExec so this tool gets the same command timeout, output
	// cap, process group, and environment scrubbing as every other command tool.
	cwd, err := commandCWD(t.workspace, "")
	if err != nil {
		return "", err
	}
	output, err := runnerOrDefault(t.runner).runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "bash", "-lc", command)
	if err == nil {
		return output, nil
	}
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	if _, ok := err.(*exec.ExitError); ok {
		// A non-zero exit status is a normal result rather than a tool failure:
		// the output and status already read as a diagnosis for the model.
		return output, nil
	}
	return output, err
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
