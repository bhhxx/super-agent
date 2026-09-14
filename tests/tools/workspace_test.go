package tools_test

import (
	"os"
	"testing"

	"super-agent/workspace"
)

func testWorkspace(t *testing.T) *workspace.Context {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	context, err := workspace.NewDefaultContext(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return context
}
