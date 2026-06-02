package tools_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"super-agent/runtime"
	. "super-agent/tools"
)

func TestDefaultRegistryExposesSecondPriorityTools(t *testing.T) {
	specs := DefaultRegistry().Specs()
	names := map[string]bool{}
	for _, spec := range specs {
		names[spec.Name] = true
	}
	for _, name := range []string{"run_command", "go_test", "format", "git_status", "git_diff"} {
		if !names[name] {
			t.Fatalf("tool %q missing from specs %+v", name, specs)
		}
	}
}

func TestRunCommandUsesWorkspaceCWDAndTruncatesOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "nested/name.txt", "hello")

	got, err := DefaultRegistry().Run(context.Background(), runtime.ToolCall{
		Name:  "run_command",
		Input: `{"command":"printf \"%s:\" \"$(basename \"$PWD\")\" && cat name.txt && printf abcdefghijklmnopqrstuvwxyz","cwd":"nested","max_output_bytes":20}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(got, "nested:hello") {
		t.Fatalf("result = %q, want truncated cwd output", got)
	}
}

func TestRunCommandRejectsCWDOutsideWorkspace(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := DefaultRegistry().Run(context.Background(), runtime.ToolCall{
		Name:  "run_command",
		Input: `{"command":"pwd","cwd":".."}`,
	})
	if err == nil || !strings.Contains(err.Error(), "outside working directory") {
		t.Fatalf("err = %v, want outside working directory", err)
	}
}

func TestRunCommandUsesOpenSandboxBackendWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	cli := filepath.Join(dir, "osb")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\nprintf sandbox-output\n"
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	tool := RunCommandTool{Config: Config{
		OpenSandboxID:  "sbx-1",
		OpenSandboxCLI: cli,
		OpenSandboxCWD: "/workspace",
	}}

	out, err := tool.Run(context.Background(), runtime.ToolCall{Name: "run_command", Input: `{"command":"pwd","timeout_seconds":5}`})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out != "sandbox-output" {
		t.Fatalf("out = %q, want sandbox-output", out)
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "command\nrun\nsbx-1\n-o\nraw\n--workdir\n/workspace\n--timeout\n5s\n--\nbash\n-lc\npwd\n"
	if string(args) != want {
		t.Fatalf("args = %q, want %q", string(args), want)
	}
}

func TestRunCommandUsesOpenSandboxCLIArguments(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	uv := filepath.Join(dir, "uv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\nprintf sandbox-output\n"
	if err := os.WriteFile(uv, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	tool := RunCommandTool{Config: Config{
		OpenSandboxID:  "sbx-1",
		OpenSandboxCLI: uv + " --directory /home/bhhxx/OpenSandbox/cli run osb",
		OpenSandboxCWD: "/workspace",
	}}

	out, err := tool.Run(context.Background(), runtime.ToolCall{Name: "run_command", Input: `{"command":"pwd","timeout_seconds":5}`})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out != "sandbox-output" {
		t.Fatalf("out = %q, want sandbox-output", out)
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "--directory\n/home/bhhxx/OpenSandbox/cli\nrun\nosb\ncommand\nrun\nsbx-1\n-o\nraw\n--workdir\n/workspace\n--timeout\n5s\n--\nbash\n-lc\npwd\n"
	if string(args) != want {
		t.Fatalf("args = %q, want %q", string(args), want)
	}
}

func TestGoTestRunsPackages(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "go.mod", "module example.com/x\n\ngo 1.24\n")
	mustWrite(t, "main.go", "package main\n")

	got, err := DefaultRegistry().Run(context.Background(), runtime.ToolCall{
		Name:  "go_test",
		Input: `{"packages":["./..."]}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(got, "no test files") {
		t.Fatalf("result = %q, want go test output", got)
	}
}

func TestFormatRunsGofmtOnWorkspaceFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "main.go", "package main\nfunc main(){println(\"hi\")}\n")

	got, err := DefaultRegistry().Run(context.Background(), runtime.ToolCall{
		Name:  "format",
		Input: `{"files":["main.go"]}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "formatted 1 file" {
		t.Fatalf("result = %q, want formatted message", got)
	}
	if content := mustRead(t, "main.go"); !strings.Contains(content, "func main() {") {
		t.Fatalf("content = %q, want gofmt output", content)
	}
}

func TestGitStatusAndDiffAreReadOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	runGit(t, "init")
	mustWrite(t, "tracked.txt", "before\n")
	runGit(t, "add", "tracked.txt")
	mustWrite(t, "tracked.txt", "after\n")
	mustWrite(t, "new.txt", "new\n")

	status, err := DefaultRegistry().Run(context.Background(), runtime.ToolCall{
		Name:  "git_status",
		Input: `{}`,
	})
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !strings.Contains(status, "AM tracked.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("status = %q, want tracked and untracked files", status)
	}

	diff, err := DefaultRegistry().Run(context.Background(), runtime.ToolCall{
		Name:  "git_diff",
		Input: `{"paths":["tracked.txt"]}`,
	})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}
	if !strings.Contains(diff, "-before") || !strings.Contains(diff, "+after") {
		t.Fatalf("diff = %q, want tracked file diff", diff)
	}
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
