package workspace_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	agentruntime "super-agent/runtime"
	"super-agent/workspace"
)

func TestWorkspaceSpecRoundTripsThroughValidatedRuntime(t *testing.T) {
	primary := t.TempDir()
	readOnly := t.TempDir()
	context, err := workspace.NewContext(primary, primary, []workspace.Root{
		{Path: primary, Access: workspace.AccessReadWrite},
		{Path: readOnly, Access: workspace.AccessRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeWorkspace := workspace.New(context)
	spec := runtimeWorkspace.Spec()
	if err := runtimeWorkspace.Activate(spec); err != nil {
		t.Fatal(err)
	}
	if runtimeWorkspace.GetCWD() != spec.CWD || !runtimeWorkspace.CanWrite(filepath.Join(primary, "new")) {
		t.Fatal("primary read-write workspace did not survive activation")
	}
	if !runtimeWorkspace.CanRead(filepath.Join(readOnly, "file")) || runtimeWorkspace.CanWrite(filepath.Join(readOnly, "file")) {
		t.Fatal("additional read-only access did not survive activation")
	}
}

func TestWorkspaceActivationRejectsInvalidSavedPathsWithoutChangingCurrentContext(t *testing.T) {
	current := t.TempDir()
	context, err := workspace.NewDefaultContext(current)
	if err != nil {
		t.Fatal(err)
	}
	runtimeWorkspace := workspace.New(context)

	missing := filepath.Join(t.TempDir(), "missing")
	invalid := agentruntime.WorkspaceSpec{PrimaryRoot: missing, CWD: missing, Roots: []agentruntime.WorkspaceRootSpec{{Path: missing, Access: agentruntime.WorkspaceAccessReadWrite}}}
	if err := runtimeWorkspace.Activate(invalid); err == nil {
		t.Fatal("missing saved primary root was accepted")
	}
	if runtimeWorkspace.GetCWD() != context.GetCWD() {
		t.Fatal("failed activation changed the current workspace")
	}

	outsideCWD := t.TempDir()
	invalid = agentruntime.WorkspaceSpec{PrimaryRoot: current, CWD: outsideCWD, Roots: []agentruntime.WorkspaceRootSpec{{Path: current, Access: agentruntime.WorkspaceAccessReadWrite}}}
	if err := runtimeWorkspace.Activate(invalid); err == nil || !strings.Contains(err.Error(), "cwd is not readable") {
		t.Fatalf("outside cwd error = %v", err)
	}
}

func TestWorkspaceActivationRejectsMissingAdditionalRoot(t *testing.T) {
	primary := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	context, _ := workspace.NewDefaultContext(primary)
	runtimeWorkspace := workspace.New(context)
	spec := agentruntime.WorkspaceSpec{PrimaryRoot: primary, CWD: primary, Roots: []agentruntime.WorkspaceRootSpec{
		{Path: primary, Access: agentruntime.WorkspaceAccessReadWrite},
		{Path: missing, Access: agentruntime.WorkspaceAccessRead},
	}}
	if err := runtimeWorkspace.Activate(spec); err == nil {
		t.Fatal("missing additional root was accepted")
	}
}

func TestWorkspaceActivationRejectsRootReplacedByEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	parent := t.TempDir()
	savedRoot := filepath.Join(parent, "project")
	if err := os.Mkdir(savedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	savedContext, err := workspace.NewDefaultContext(savedRoot)
	if err != nil {
		t.Fatal(err)
	}
	spec := workspace.New(savedContext).Spec()
	if err := os.Remove(savedRoot); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, savedRoot); err != nil {
		t.Fatal(err)
	}
	currentContext, _ := workspace.NewDefaultContext(t.TempDir())
	if err := workspace.New(currentContext).Activate(spec); err == nil || !strings.Contains(err.Error(), "resolves to a different path") {
		t.Fatalf("replaced root error = %v", err)
	}
}

func TestWorkspaceCanonicalizeUpgradesLegacyNonCanonicalPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	context, err := workspace.NewDefaultContext(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeWorkspace := workspace.New(context)

	// Legacy metadata may hold a path that is not itself canonical, such as a
	// symlinked cwd. Canonicalize upgrades it; Validate then accepts the result.
	legacy := agentruntime.WorkspaceSpec{
		PrimaryRoot: link, CWD: link,
		Roots: []agentruntime.WorkspaceRootSpec{{Path: link, Access: agentruntime.WorkspaceAccessReadWrite}},
	}
	upgraded, err := runtimeWorkspace.Canonicalize(legacy)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.PrimaryRoot != canonical || upgraded.CWD != canonical || len(upgraded.Roots) != 1 || upgraded.Roots[0].Path != canonical {
		t.Fatalf("canonicalize = %+v, want %q", upgraded, canonical)
	}
	again, err := runtimeWorkspace.Canonicalize(upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(upgraded, again) {
		t.Fatalf("canonicalize is not idempotent: %+v then %+v", upgraded, again)
	}
	if err := runtimeWorkspace.Validate(upgraded); err != nil {
		t.Fatalf("canonical result failed strict validation: %v", err)
	}
}
