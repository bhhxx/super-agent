package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"super-agent/runtime"
)

const (
	defaultCommandTimeout = 30 * time.Second
	maxCommandTimeout     = 120 * time.Second
	defaultOutputBytes    = 20000
)

type RunCommandTool struct {
	Config Config
}
type GoTestTool struct{}
type FormatTool struct{}
type GitStatusTool struct{}
type GitDiffTool struct{}

type OpenSandboxCommandRunner struct {
	CLI       string
	SandboxID string
	Workdir   string
}

func (RunCommandTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "run_command",
		Description: "Run a workspace command with cwd, timeout_seconds, and max_output_bytes.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"command":           map[string]any{"type": "string"},
			"cwd":               map[string]any{"type": "string"},
			"timeout_seconds":   map[string]any{"type": "integer"},
			"max_output_bytes":  map[string]any{"type": "integer"},
			"continue_on_error": map[string]any{"type": "boolean"},
		}, []string{"command"}),
	}
}

func (t RunCommandTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Command         string `json:"command"`
		CWD             string `json:"cwd"`
		TimeoutSeconds  int    `json:"timeout_seconds"`
		MaxOutputBytes  int    `json:"max_output_bytes"`
		ContinueOnError bool   `json:"continue_on_error"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	if args.Command == "" {
		return "", errors.New("command is required")
	}
	cwd, err := commandCWD(args.CWD)
	if err != nil {
		return "", err
	}
	if args.CWD == "" {
		args.CWD = "."
	}
	if runner := openSandboxRunner(t.Config, args.CWD); runner != nil {
		output, err := runner.Run(ctx, args.TimeoutSeconds, args.MaxOutputBytes, args.Command)
		if err != nil && !args.ContinueOnError {
			return output, err
		}
		return output, nil
	}
	output, err := runShell(ctx, cwd, args.TimeoutSeconds, args.MaxOutputBytes, args.Command)
	if err != nil && !args.ContinueOnError {
		return output, err
	}
	return output, nil
}

func (GoTestTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "go_test",
		Description: "Run go test for workspace packages.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"packages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"cwd":      map[string]any{"type": "string"},
		}, nil),
	}
}

func (GoTestTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Packages []string `json:"packages"`
		CWD      string   `json:"cwd"`
	}
	if call.Input != "" {
		if err := decodeArgs(call.Input, &args); err != nil {
			return "", err
		}
	}
	if len(args.Packages) == 0 {
		args.Packages = []string{"./..."}
	}
	cwd, err := commandCWD(args.CWD)
	if err != nil {
		return "", err
	}
	cmdArgs := append([]string{"test"}, args.Packages...)
	return runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "go", cmdArgs...)
}

func (FormatTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "format",
		Description: "Run gofmt -w on workspace Go files.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, []string{"files"}),
	}
}

func (FormatTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Files []string `json:"files"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	if len(args.Files) == 0 {
		return "", errors.New("files is required")
	}
	files := make([]string, 0, len(args.Files))
	for _, file := range args.Files {
		path, _, err := workspacePath(file)
		if err != nil {
			return "", err
		}
		files = append(files, path)
	}
	if _, err := runExec(ctx, "", defaultCommandTimeout, defaultOutputBytes, "gofmt", append([]string{"-w"}, files...)...); err != nil {
		return "", err
	}
	return "formatted " + strconv.Itoa(len(files)) + plural(len(files), " file", " files"), nil
}

func (GitStatusTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "git_status",
		Description: "Show git status --short for the workspace.",
		Parameters:  objectSchema(map[string]any{}, nil),
	}
}

func (GitStatusTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	if call.Input != "" && call.Input != "{}" {
		var args map[string]any
		if err := json.Unmarshal([]byte(call.Input), &args); err != nil {
			return "", errors.New("invalid JSON input")
		}
	}
	cwd, err := commandCWD(".")
	if err != nil {
		return "", err
	}
	return runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "git", "status", "--short")
}

func (GitDiffTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "git_diff",
		Description: "Show git diff for optional workspace paths.",
		Parameters: objectSchema(map[string]any{
			"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, nil),
	}
}

func (GitDiffTool) Run(ctx context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Paths []string `json:"paths"`
	}
	if call.Input != "" {
		if err := decodeArgs(call.Input, &args); err != nil {
			return "", err
		}
	}
	cmdArgs := []string{"diff", "--"}
	for _, path := range args.Paths {
		_, rel, err := workspacePath(path)
		if err != nil {
			return "", err
		}
		cmdArgs = append(cmdArgs, rel)
	}
	cwd, err := commandCWD(".")
	if err != nil {
		return "", err
	}
	return runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "git", cmdArgs...)
}

func commandCWD(cwd string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	path, _, err := workspacePath(cwd)
	return path, err
}

func runShell(ctx context.Context, cwd string, timeoutSeconds int, maxBytes int, command string) (string, error) {
	return runExec(ctx, cwd, commandTimeout(timeoutSeconds), outputLimit(maxBytes), "bash", "-lc", command)
}

func openSandboxRunner(cfg Config, cwd string) *OpenSandboxCommandRunner {
	if cfg.OpenSandboxID == "" {
		return nil
	}
	cli := cfg.OpenSandboxCLI
	if cli == "" {
		cli = "osb"
	}
	workdir := cfg.OpenSandboxCWD
	if workdir == "" && cwd != "" && cwd != "." {
		workdir = cwd
	}
	return &OpenSandboxCommandRunner{CLI: cli, SandboxID: cfg.OpenSandboxID, Workdir: workdir}
}

func (r *OpenSandboxCommandRunner) Run(ctx context.Context, timeoutSeconds int, maxBytes int, command string) (string, error) {
	args := []string{"command", "run", r.SandboxID, "-o", "raw"}
	if r.Workdir != "" {
		args = append(args, "--workdir", r.Workdir)
	}
	if timeoutSeconds > 0 {
		args = append(args, "--timeout", strconv.Itoa(timeoutSeconds)+"s")
	}
	args = append(args, "--", "bash", "-lc", command)
	return runExec(ctx, "", commandTimeout(timeoutSeconds), outputLimit(maxBytes), r.CLI, args...)
}

func runExec(ctx context.Context, cwd string, timeout time.Duration, maxBytes int, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	result := trimOutput(string(output), maxBytes)
	if runCtx.Err() != nil {
		return result, runCtx.Err()
	}
	if err != nil {
		if result != "" && !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		result += err.Error()
	}
	return result, err
}

func commandTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultCommandTimeout
	}
	timeout := time.Duration(seconds) * time.Second
	if timeout > maxCommandTimeout {
		return maxCommandTimeout
	}
	return timeout
}

func outputLimit(maxBytes int) int {
	if maxBytes <= 0 {
		return defaultOutputBytes
	}
	return maxBytes
}

func trimOutput(output string, maxBytes int) string {
	if len(output) <= maxBytes {
		return output
	}
	return output[:maxBytes] + "\n... truncated"
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
