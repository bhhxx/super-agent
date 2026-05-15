package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"

	"super-agent/runtime"
)

type BashTools struct{}

func NewBashTools() BashTools {
	return BashTools{}
}

func (BashTools) Specs() []runtime.ToolSpec {
	return []runtime.ToolSpec{
		{
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
		},
		{
			Name:        "update_topic",
			Description: "Updates the current logical phase or strategic intent of the conversation.",
			Risky:       false,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":   map[string]any{"type": "string", "description": "The title of the new topic or chapter."},
					"summary": map[string]any{"type": "string", "description": "A detailed summary of the strategic intent."},
				},
				"required": []string{"title", "summary"},
			},
		},
	}
}

func (BashTools) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	switch call.Name {
	case "update_topic":
		return "Topic updated successfully", nil
	case "bash":
		command := bashCommand(call.Input)
		output, err := exec.CommandContext(ctx, "bash", "-lc", command).CombinedOutput()
		return string(output), err
	default:
		return "", errors.New("unknown tool: " + call.Name)
	}
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
