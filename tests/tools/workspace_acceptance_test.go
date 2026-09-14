package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	agentruntime "super-agent/runtime"
	"super-agent/tools"
	"super-agent/workspace"
)

func TestWorkspaceFileToolsBlackBoxAcceptance(t *testing.T) {
	parent := t.TempDir()
	project := filepath.Join(parent, "project")
	projectSecret := filepath.Join(parent, "project-secret")
	outside := filepath.Join(parent, "outside")
	for _, directory := range []string{filepath.Join(project, "src"), filepath.Join(project, "allowed"), projectSecret, outside} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "src", "a.txt"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectSecret, "foo"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaceContext, err := workspace.NewDefaultContext(project)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.DefaultRegistry(workspaceContext)

	mustToolSucceed(t, registry, "read_file", map[string]any{"path": "src/a.txt"})
	mustToolSucceed(t, registry, "write_file", map[string]any{"path": "src/a.txt", "content": "after"})
	mustToolSucceed(t, registry, "write_file", map[string]any{"path": "allowed/new.txt", "content": "new"})
	mustToolSucceed(t, registry, "read_file", map[string]any{"path": filepath.Join(project, "src", "a.txt")})

	mustToolFail(t, registry, "read_file", map[string]any{"path": "../secret.txt"})
	mustToolFail(t, registry, "write_file", map[string]any{"path": "../secret.txt", "content": "no"})
	mustToolFail(t, registry, "read_file", map[string]any{"path": filepath.Join(projectSecret, "foo")})
	mustToolFail(t, registry, "write_file", map[string]any{"path": "foo/../../outside/planted.txt", "content": "no"})
	mustToolFail(t, registry, "read_file", map[string]any{"path": filepath.Join(outside, "missing")})

	if runtime.GOOS != "windows" {
		outsideFile := filepath.Join(outside, "external.txt")
		if err := os.WriteFile(outsideFile, []byte("external"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideFile, filepath.Join(project, "file-link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(project, "dir-link")); err != nil {
			t.Fatal(err)
		}
		mustToolFail(t, registry, "read_file", map[string]any{"path": "file-link"})
		mustToolFail(t, registry, "search", map[string]any{"path": "dir-link", "query": "external"})
	}
}

func TestAdditionalReadOnlyRootBlackBoxAcceptance(t *testing.T) {
	project := t.TempDir()
	common := t.TempDir()
	path := filepath.Join(common, "shared.txt")
	if err := os.WriteFile(path, []byte("shared needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceContext, err := workspace.NewContext(project, project, []workspace.Root{
		{Path: project, Access: workspace.AccessReadWrite},
		{Path: common, Access: workspace.AccessRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.DefaultRegistry(workspaceContext)
	mustToolSucceed(t, registry, "read_file", map[string]any{"path": path})
	mustToolSucceed(t, registry, "search", map[string]any{"path": common, "query": "needle"})
	mustToolFail(t, registry, "write_file", map[string]any{"path": path, "content": "changed"})
	mustToolFail(t, registry, "apply_patch", map[string]any{"path": path, "old_text": "shared", "new_text": "changed"})
}

func TestCommandToolsUseWorkspaceCWDNotProcessCWD(t *testing.T) {
	processCWD := t.TempDir()
	project := t.TempDir()
	t.Chdir(processCWD)
	if err := os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\nfunc main(){println(\"x\")}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init")
	git.Dir = project
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	workspaceContext, err := workspace.NewDefaultContext(project)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.DefaultRegistry(workspaceContext)
	for _, name := range []string{"run_command", "bash"} {
		result := mustToolSucceed(t, registry, name, map[string]any{"command": "pwd"})
		if strings.TrimSpace(result) != workspaceContext.GetCWD() {
			t.Fatalf("%s cwd = %q, want %q", name, strings.TrimSpace(result), workspaceContext.GetCWD())
		}
	}
	status := mustToolSucceed(t, registry, "git_status", map[string]any{})
	if !strings.Contains(status, "No commits yet") && !strings.Contains(status, "Initial commit") {
		t.Fatalf("git_status did not run in workspace repository: %q", status)
	}
	mustToolSucceed(t, registry, "format", map[string]any{"files": []string{"main.go"}})
	formatted, err := os.ReadFile(filepath.Join(project, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(formatted), "func main() {") {
		t.Fatalf("format did not update workspace file: %q", formatted)
	}
}

func mustToolSucceed(t *testing.T, registry *tools.Registry, name string, input any) string {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Run(context.Background(), agentruntime.ToolCall{Name: name, Input: string(encoded)})
	if err != nil {
		t.Fatalf("%s(%s) failed: %v", name, encoded, err)
	}
	return result
}

func mustToolFail(t *testing.T, registry *tools.Registry, name string, input any) {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := registry.Run(context.Background(), agentruntime.ToolCall{Name: name, Input: string(encoded)}); err == nil {
		t.Fatalf("%s(%s) succeeded unexpectedly: %q", name, encoded, result)
	}
}
