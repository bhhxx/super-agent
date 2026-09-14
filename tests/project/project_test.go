package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"super-agent/project"
)

func TestResolveUsesExplicitDirectory(t *testing.T) {
	explicit := t.TempDir()
	other := t.TempDir()
	got, err := project.Resolve(explicit, other)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(explicit)
	if got.Root != want || got.ID == "" {
		t.Fatalf("project = %+v, want root %q and non-empty id", got, want)
	}
}

func TestResolveFindsNearestGitAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := project.Resolve("", nested)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	if got.Root != want {
		t.Fatalf("root = %q, want %q", got.Root, want)
	}
}

func TestResolveFallsBackToCurrentDirectory(t *testing.T) {
	cwd := t.TempDir()
	got, err := project.Resolve("", cwd)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(cwd)
	if got.Root != want {
		t.Fatalf("root = %q, want %q", got.Root, want)
	}
}
