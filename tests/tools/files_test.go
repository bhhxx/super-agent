package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"super-agent/runtime"
	. "super-agent/tools"
	"super-agent/workspace"
)

func TestFileToolsExposeFirstPriorityTools(t *testing.T) {
	specs := DefaultRegistry(testWorkspace(t)).Specs()
	names := map[string]bool{}
	for _, spec := range specs {
		names[spec.Name] = true
	}
	for _, name := range []string{"read_file", "list_files", "search", "apply_patch", "write_file", "bash"} {
		if !names[name] {
			t.Fatalf("tool %q missing from specs %+v", name, specs)
		}
	}
}

func TestReadFileSupportsLineRange(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "notes.txt", "one\ntwo\nthree\n")

	got, err := DefaultRegistry(testWorkspace(t)).Run(context.Background(), runtime.ToolCall{
		Name:  "read_file",
		Input: `{"path":"notes.txt","start_line":2,"end_line":3}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "2: two\n3: three" {
		t.Fatalf("result = %q, want selected numbered lines", got)
	}
}

func TestReadFileRejectsPathOutsideWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	mustWriteAbs(t, outside, "secret")
	t.Chdir(dir)

	_, err := DefaultRegistry(testWorkspace(t)).Run(context.Background(), runtime.ToolCall{
		Name:  "read_file",
		Input: `{"path":"../secret.txt"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "outside readable workspace roots") {
		t.Fatalf("err = %v, want workspace rejection", err)
	}
}

func TestListFilesReturnsMatchingRelativeFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "a.go", "")
	mustWrite(t, "nested/b.go", "")
	mustWrite(t, "nested/c.txt", "")

	got, err := DefaultRegistry(testWorkspace(t)).Run(context.Background(), runtime.ToolCall{
		Name:  "list_files",
		Input: `{"path":".","pattern":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "a.go\nnested/b.go" {
		t.Fatalf("result = %q, want go files", got)
	}
}

func TestSearchFindsTextWithLineNumbers(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "a.txt", "alpha\nneedle\n")
	mustWrite(t, "nested/b.txt", "needle again\n")

	got, err := DefaultRegistry(testWorkspace(t)).Run(context.Background(), runtime.ToolCall{
		Name:  "search",
		Input: `{"query":"needle","path":"."}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "a.txt:2:needle\nnested/b.txt:1:needle again" {
		t.Fatalf("result = %q, want matches", got)
	}
}

func TestApplyPatchReplacesExpectedText(t *testing.T) {
	t.Chdir(t.TempDir())
	mustWrite(t, "main.go", "package main\n\nfunc main() {}\n")

	got, err := DefaultRegistry(testWorkspace(t)).Run(context.Background(), runtime.ToolCall{
		Name:  "apply_patch",
		Input: `{"path":"main.go","old_text":"func main() {}","new_text":"func main() {\n\tprintln(\"hi\")\n}"}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "patched main.go" {
		t.Fatalf("result = %q, want patched message", got)
	}
	if content := mustRead(t, "main.go"); !strings.Contains(content, "println(\"hi\")") {
		t.Fatalf("content = %q, want replacement", content)
	}
}

func TestWriteFileCreatesParentDirectories(t *testing.T) {
	t.Chdir(t.TempDir())

	got, err := DefaultRegistry(testWorkspace(t)).Run(context.Background(), runtime.ToolCall{
		Name:  "write_file",
		Input: `{"path":"nested/out.txt","content":"hello"}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "wrote nested/out.txt" {
		t.Fatalf("result = %q, want write message", got)
	}
	if content := mustRead(t, "nested/out.txt"); content != "hello" {
		t.Fatalf("content = %q, want hello", content)
	}
}

func TestFileToolsUseInjectedRootAccess(t *testing.T) {
	primary := t.TempDir()
	readOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(readOnly, "shared.txt"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceContext, err := workspace.NewContext(primary, primary, []workspace.Root{
		{Path: primary, Access: workspace.AccessReadWrite},
		{Path: readOnly, Access: workspace.AccessRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := RegistryForWorkspace(workspaceContext)
	path := filepath.Join(readOnly, "shared.txt")
	if _, err := registry.Run(context.Background(), runtime.ToolCall{Name: "read_file", Input: `{"path":` + strconv.Quote(path) + `}`}); err != nil {
		t.Fatalf("read additional root: %v", err)
	}
	if _, err := registry.Run(context.Background(), runtime.ToolCall{Name: "write_file", Input: `{"path":` + strconv.Quote(path) + `,"content":"changed"}`}); err == nil || !strings.Contains(err.Error(), "outside writable workspace roots") {
		t.Fatalf("write read-only root error = %v", err)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	mustWriteAbs(t, filepath.Join(".", path), content)
}

func mustWriteAbs(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
