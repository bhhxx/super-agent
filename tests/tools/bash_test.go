package tools_test

import (
	"context"
	"testing"

	"super-agent/runtime"
	. "super-agent/tools"
)

func TestBashToolsExposeRiskyBashTool(t *testing.T) {
	specs := NewBashTools().Specs()
	for _, spec := range specs {
		if spec.Name == "bash" {
			if !spec.Risky {
				t.Fatal("bash tool is not risky")
			}
			return
		}
	}
	t.Fatal("bash tool not found")
}

func TestBashToolsRunCommand(t *testing.T) {
	got, err := NewBashTools().Run(context.Background(), runtime.ToolCall{
		Name:  "bash",
		Input: `{"command":"printf hello"}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "hello" {
		t.Fatalf("result = %q, want hello", got)
	}
}

func TestNoToolsExposesNoSpecs(t *testing.T) {
	if specs := (NoTools{}).Specs(); len(specs) != 0 {
		t.Fatalf("specs = %+v, want none", specs)
	}
}
