package workspace_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"super-agent/workspace"
)

func TestContextAllowsRelativeAndAbsolutePathsInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	ctx := newContext(t, root, workspace.Root{Path: root, Access: workspace.AccessReadWrite})
	relative, err := ctx.ResolvePath("src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, _ := filepath.EvalSymlinks(root)
	want := filepath.Join(canonicalRoot, "src", "index.ts")
	if relative != want || !ctx.CanRead("src/index.ts") || !ctx.CanWrite(want) {
		t.Fatalf("resolved = %q read=%v write=%v, want %q and both allowed", relative, ctx.CanRead("src/index.ts"), ctx.CanWrite(want), want)
	}
}

func TestContextRejectsTraversalPrefixCollisionAndOutsidePath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	secret := filepath.Join(parent, "project-secret")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := newContext(t, root, workspace.Root{Path: root, Access: workspace.AccessReadWrite})
	for _, path := range []string{"../project-secret/file", filepath.Join(secret, "file"), t.TempDir()} {
		if ctx.CanRead(path) || ctx.CanWrite(path) {
			t.Errorf("path %q unexpectedly allowed", path)
		}
	}
}

func TestContextHonorsReadOnlyReadWriteAndAdditionalRoots(t *testing.T) {
	primary := t.TempDir()
	readOnly := t.TempDir()
	additional := t.TempDir()
	ctx := newContext(t, primary,
		workspace.Root{Path: primary, Access: workspace.AccessReadWrite},
		workspace.Root{Path: readOnly, Access: workspace.AccessRead},
		workspace.Root{Path: additional, Access: workspace.AccessReadWrite},
	)
	if !ctx.CanRead(filepath.Join(readOnly, "file")) || ctx.CanWrite(filepath.Join(readOnly, "file")) {
		t.Fatal("read-only root must allow reads and reject writes")
	}
	if !ctx.CanRead(filepath.Join(additional, "file")) || !ctx.CanWrite(filepath.Join(additional, "file")) {
		t.Fatal("additional read-write root must allow reads and writes")
	}
	canonicalPrimary, _ := filepath.EvalSymlinks(primary)
	if got := ctx.GetPrimaryRoot(); got != canonicalPrimary {
		t.Fatalf("primary root = %q, want %q", got, canonicalPrimary)
	}
	if got := ctx.GetCWD(); got != canonicalPrimary {
		t.Fatalf("cwd = %q, want %q", got, canonicalPrimary)
	}
	if got := len(ctx.GetRoots()); got != 3 {
		t.Fatalf("roots = %d, want 3", got)
	}
}

func TestContextRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	ctx := newContext(t, root, workspace.Root{Path: root, Access: workspace.AccessReadWrite})
	if ctx.CanRead("link/secret") || ctx.CanWrite("link/new-file") {
		t.Fatal("symlink escape unexpectedly allowed")
	}
}

func newContext(t *testing.T, primary string, roots ...workspace.Root) *workspace.Context {
	t.Helper()
	ctx, err := workspace.NewContext(primary, primary, roots)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
