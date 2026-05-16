package tools_test

import (
	"context"
	"strings"
	"testing"

	"super-agent/runtime"
	. "super-agent/tools"
)

type fakeTool struct {
	spec runtime.ToolSpec
	got  []runtime.ToolCall
}

func (t *fakeTool) Spec() runtime.ToolSpec {
	return t.spec
}

func (t *fakeTool) Run(_ context.Context, call runtime.ToolCall) (string, error) {
	t.got = append(t.got, call)
	return "ran " + call.Name, nil
}

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

func TestBashToolsExposeOnlyBashTool(t *testing.T) {
	specs := NewBashTools().Specs()
	if len(specs) != 1 {
		t.Fatalf("specs = %+v, want only bash", specs)
	}
	if specs[0].Name != "bash" {
		t.Fatalf("tool = %q, want bash", specs[0].Name)
	}
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

func TestBashToolsReturnsFailedCommandOutputWithoutError(t *testing.T) {
	got, err := NewBashTools().Run(context.Background(), runtime.ToolCall{
		Name:  "bash",
		Input: `{"command":"printf before; exit 7"}`,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "exit status 7") {
		t.Fatalf("result = %q, want command output and exit status", got)
	}
}

func TestNoToolsExposesNoSpecs(t *testing.T) {
	if specs := (NoTools{}).Specs(); len(specs) != 0 {
		t.Fatalf("specs = %+v, want none", specs)
	}
}

func TestToolRegistryAggregatesSpecsAndDispatchesByName(t *testing.T) {
	first := &fakeTool{spec: runtime.ToolSpec{Name: "first"}}
	second := &fakeTool{spec: runtime.ToolSpec{Name: "second"}}
	registry := NewRegistry(first, second)

	specs := registry.Specs()
	if len(specs) != 2 || specs[0].Name != "first" || specs[1].Name != "second" {
		t.Fatalf("specs = %+v, want first then second", specs)
	}

	got, err := registry.Run(context.Background(), runtime.ToolCall{Name: "second"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got != "ran second" {
		t.Fatalf("result = %q, want ran second", got)
	}
	if len(first.got) != 0 {
		t.Fatalf("first tool calls = %+v, want none", first.got)
	}
	if len(second.got) != 1 || second.got[0].Name != "second" {
		t.Fatalf("second tool calls = %+v, want one second call", second.got)
	}
}

func TestToolRegistryRejectsUnknownTool(t *testing.T) {
	registry := NewRegistry(&fakeTool{spec: runtime.ToolSpec{Name: "known"}})

	_, err := registry.Run(context.Background(), runtime.ToolCall{Name: "missing"})
	if err == nil || !strings.Contains(err.Error(), "unknown tool: missing") {
		t.Fatalf("err = %v, want unknown tool error", err)
	}
}
