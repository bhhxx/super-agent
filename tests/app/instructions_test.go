package app_test

import (
	"os"
	"path/filepath"
	"testing"

	. "super-agent/app"
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
