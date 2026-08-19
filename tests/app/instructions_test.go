package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "super-agent/app"
	"super-agent/app/instructions"
	"super-agent/runtime"
)

func TestLoadProjectInstructionsMergesRootToLeaf(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "pkg", "feature")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "AGENTS.md"), []byte("pkg rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("leaf rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProjectInstructions(nested)
	if err != nil {
		t.Fatalf("LoadProjectInstructions failed: %v", err)
	}
	want := "root rules\n\npkg rules\n\nleaf rules"
	if got != want {
		t.Fatalf("instructions = %q, want %q", got, want)
	}
}

func TestLoadStartsAtGitRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	nested := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "AGENTS.md"), []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("repo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProjectInstructions(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != "repo\n\nnested" {
		t.Fatalf("instructions = %q", got)
	}
}

func TestLoadIncludesUserSpecBeforeProjectInstructions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".superagent"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".superagent", "AGENTS.md"), []byte("user spec"), 0644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("project rules"), 0644); err != nil {
		t.Fatal(err)
	}

	bundle, err := instructions.Load(project)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Content != "user spec\n\nproject rules" {
		t.Fatalf("content = %q", bundle.Content)
	}
	if len(bundle.Sources) != 2 || bundle.Sources[0].Kind != "user-spec" || bundle.Sources[1].Kind != "project" {
		t.Fatalf("sources = %+v", bundle.Sources)
	}
}

func TestLoadUsesClaudeWhenSameDirectoryAgentsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("root claude"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "CLAUDE.md"), []byte("nested claude"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("nested agents"), 0644); err != nil {
		t.Fatal(err)
	}

	bundle, err := instructions.Load(nested)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Content != "root claude\n\nnested agents" {
		t.Fatalf("content = %q", bundle.Content)
	}
	if len(bundle.Sources) != 2 || bundle.Sources[0].Kind != "claude-compat" || bundle.Sources[1].Kind != "project" {
		t.Fatalf("sources = %+v", bundle.Sources)
	}
}

func TestLoadRejectsOversizedInstructionFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	content := strings.Repeat("x", instructions.MaxFileSize+1)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := instructions.Load(dir)
	if err == nil {
		t.Fatal("Load succeeded, want size error")
	}
	if !strings.Contains(err.Error(), "too large") || !strings.Contains(err.Error(), "AGENTS.md") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadProjectInstructionsAllowsMissingAGENTSMD(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	instructions, err := LoadProjectInstructions(t.TempDir())
	if err != nil {
		t.Fatalf("LoadProjectInstructions failed: %v", err)
	}
	if instructions != "" {
		t.Fatalf("instructions = %q, want empty", instructions)
	}
}

func TestNewSessionInjectsSystemPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	session, err := NewSession(Config{
		Provider: "deepseek",
		NoTools:  true,
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	messages := session.Snapshot().Messages
	if len(messages) != 1 || messages[0].Role != runtime.RoleSystem {
		t.Fatalf("messages = %+v, want system prompt", messages)
	}
	if messages[0].Content == "" {
		t.Fatal("system prompt is empty")
	}
	if messages[0].Content == "# Rules\n\n- keep tests focused" {
		t.Fatal("system prompt came from a project file, want built-in prompt")
	}
}

func TestLoadEmptyAgentsFallsBackToClaudeMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("   \n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Project Guidance\n\nrules from claude"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	bundle, err := instructions.Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !strings.Contains(bundle.Content, "rules from claude") {
		t.Fatalf("bundle = %+v, want CLAUDE.md fallback content", bundle)
	}
}
