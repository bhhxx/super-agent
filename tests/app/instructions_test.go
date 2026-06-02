package app_test

import (
	"os"
	"path/filepath"
	"testing"

	. "super-agent/app"
	"super-agent/runtime"
)

func TestLoadProjectInstructionsReadsAGENTSMDFromWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Rules\n\n- keep tests focused\n"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	instructions, err := LoadProjectInstructions(dir)
	if err != nil {
		t.Fatalf("LoadProjectInstructions failed: %v", err)
	}
	if instructions != "# Rules\n\n- keep tests focused\n" {
		t.Fatalf("instructions = %q", instructions)
	}
}

func TestLoadProjectInstructionsAllowsMissingAGENTSMD(t *testing.T) {
	instructions, err := LoadProjectInstructions(t.TempDir())
	if err != nil {
		t.Fatalf("LoadProjectInstructions failed: %v", err)
	}
	if instructions != "" {
		t.Fatalf("instructions = %q, want empty", instructions)
	}
}

func TestNewSessionInjectsSystemPrompt(t *testing.T) {
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
