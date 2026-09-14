package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"super-agent/runtime/protocol"
)

const (
	defaultCommandTimeout = 30 * time.Second
	maxCommandTimeout     = 120 * time.Second
	defaultOutputBytes    = 20000
	// maxOutputBytes bounds memory the same way maxCommandTimeout bounds time.
	// Without it the model can pass an arbitrary limit and buffer a command's
	// entire output.
	maxOutputBytes = 200000
)

type RunCommandTool struct {
	runner    *commandRunner
	workspace WorkspaceContext
}
type GoTestTool struct {
	runner    *commandRunner
	workspace WorkspaceContext
}
type FormatTool struct {
	runner    *commandRunner
	workspace WorkspaceContext
}
type GitStatusTool struct {
	runner    *commandRunner
	workspace WorkspaceContext
}
type GitDiffTool struct {
	runner    *commandRunner
	workspace WorkspaceContext
}

func (RunCommandTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
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

func (t RunCommandTool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
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
	cwd, err := commandCWD(t.workspace, args.CWD)
	if err != nil {
		return "", err
	}
	output, err := runnerOrDefault(t.runner).runShell(ctx, cwd, args.TimeoutSeconds, args.MaxOutputBytes, args.Command)
	if err != nil && !args.ContinueOnError {
		return output, err
	}
	return output, nil
}

func (GoTestTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "go_test",
		Description: "Run go test for workspace packages.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"packages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"cwd":      map[string]any{"type": "string"},
		}, nil),
	}
}

func (t GoTestTool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
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
	cwd, err := commandCWD(t.workspace, args.CWD)
	if err != nil {
		return "", err
	}
	for _, pkg := range args.Packages {
		if strings.HasPrefix(pkg, "-") {
			return "", errors.New("package paths must not start with '-': " + pkg)
		}
	}
	cmdArgs := append([]string{"test"}, args.Packages...)
	return runnerOrDefault(t.runner).runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "go", cmdArgs...)
}

func (FormatTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "format",
		Description: "Run gofmt -w on workspace Go files.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, []string{"files"}),
	}
}

func (t FormatTool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
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
		path, _, err := resolveWritable(t.workspace, file)
		if err != nil {
			return "", err
		}
		files = append(files, path)
	}
	cwd, err := commandCWD(t.workspace, "")
	if err != nil {
		return "", err
	}
	if _, err := runnerOrDefault(t.runner).runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "gofmt", append([]string{"-w"}, files...)...); err != nil {
		return "", err
	}
	return "formatted " + strconv.Itoa(len(files)) + plural(len(files), " file", " files"), nil
}

func (GitStatusTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "git_status",
		Description: "Show the branch and short git status for the workspace.",
		Parameters:  objectSchema(map[string]any{}, nil),
	}
}

func (t GitStatusTool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
	if call.Input != "" && call.Input != "{}" {
		var args map[string]any
		if err := json.Unmarshal([]byte(call.Input), &args); err != nil {
			return "", errors.New("invalid JSON input")
		}
	}
	cwd, err := commandCWD(t.workspace, "")
	if err != nil {
		return "", err
	}
	return runnerOrDefault(t.runner).runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "git", "status", "--short", "--branch")
}

func (GitDiffTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "git_diff",
		Description: "Show git diff for optional workspace paths.",
		Parameters: objectSchema(map[string]any{
			"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, nil),
	}
}

func (t GitDiffTool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
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
		_, rel, err := resolveReadable(t.workspace, path)
		if err != nil {
			return "", err
		}
		cmdArgs = append(cmdArgs, rel)
	}
	cwd, err := commandCWD(t.workspace, "")
	if err != nil {
		return "", err
	}
	return runnerOrDefault(t.runner).runExec(ctx, cwd, defaultCommandTimeout, defaultOutputBytes, "git", cmdArgs...)
}

// commandCWD resolves a command tool's cwd argument inside the workspace,
// defaulting to the workspace cwd when the caller passes none.
func commandCWD(workspace WorkspaceContext, cwd string) (string, error) {
	if workspace == nil {
		return "", errors.New("workspace is not configured")
	}
	if cwd == "" {
		cwd = workspace.GetCWD()
	}
	path, _, err := resolveReadable(workspace, cwd)
	return path, err
}

func (r *commandRunner) runShell(ctx context.Context, cwd string, timeoutSeconds int, maxBytes int, command string) (string, error) {
	return r.runExec(ctx, cwd, commandTimeout(timeoutSeconds), outputLimit(maxBytes), "bash", "-lc", command)
}

func (r *commandRunner) runExec(ctx context.Context, cwd string, timeout time.Duration, maxBytes int, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if r.sandbox != nil {
		var err error
		workspaceRoot := ""
		if r.workspace != nil {
			workspaceRoot = r.workspace.GetPrimaryRoot()
		}
		name, args, cwd, err = r.sandbox.wrap(workspaceRoot, cwd, name, args)
		if err != nil {
			return "", err
		}
	}
	cmd := exec.CommandContext(runCtx, name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = childEnv()
	isolateProcessGroup(cmd)

	// stdout and stderr share one capped writer, so the cap applies to their
	// combined output and neither stream can fill a pipe buffer and block the
	// child.
	sink := &cappedBuffer{limit: maxBytes}
	cmd.Stdout = sink
	cmd.Stderr = sink

	err := cmd.Run()
	result := sink.String()
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
	if maxBytes > maxOutputBytes {
		return maxOutputBytes
	}
	return maxBytes
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
