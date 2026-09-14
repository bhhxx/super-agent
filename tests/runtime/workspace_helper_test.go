package runtime_test

import (
	"testing"

	"super-agent/workspace"
)

func configuredWorkspace(t *testing.T, root string) *workspace.Workspace {
	t.Helper()
	context, err := workspace.NewDefaultContext(root)
	if err != nil {
		t.Fatal(err)
	}
	return workspace.New(context)
}
