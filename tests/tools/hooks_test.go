package tools_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"super-agent/runtime/protocol"
	"super-agent/tools"
)

type observedTool struct{}

func (observedTool) Spec() protocol.ToolSpec                                { return protocol.ToolSpec{Name: "observed"} }
func (observedTool) Run(context.Context, protocol.ToolCall) (string, error) { return "ok", nil }

func TestToolHooksRunBeforeAndAfterInOrder(t *testing.T) {
	registry := tools.NewRegistry(observedTool{})
	var events []string
	registry.SetToolObserver(func(_ context.Context, event string, _ protocol.ToolCall, _ error) error {
		events = append(events, event)
		return nil
	})
	if _, err := registry.Run(context.Background(), protocol.ToolCall{Name: "observed"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"pre_tool", "post_tool"}) {
		t.Fatalf("events = %+v", events)
	}
}

func TestPostToolHookFailureDoesNotClobberToolResult(t *testing.T) {
	registry := tools.NewRegistry(observedTool{})
	registry.SetToolObserver(func(_ context.Context, event string, _ protocol.ToolCall, toolErr error) error {
		if event == "post_tool" {
			return errors.New("hook exploded")
		}
		return nil
	})
	output, err := registry.Run(context.Background(), protocol.ToolCall{Name: "observed"})
	if err != nil {
		t.Fatalf("Run error = %v, want nil: a failing post hook must not turn a successful tool run into an error", err)
	}
	if !strings.Contains(output, "ok") || !strings.Contains(output, "post_tool hook failed") {
		t.Fatalf("output = %q, want tool output plus the hook failure note", output)
	}
}

func TestPreToolHookFailureAbortsTool(t *testing.T) {
	registry := tools.NewRegistry(observedTool{})
	registry.SetToolObserver(func(_ context.Context, event string, _ protocol.ToolCall, _ error) error {
		if event == "pre_tool" {
			return errors.New("blocked by hook")
		}
		return nil
	})
	if _, err := registry.Run(context.Background(), protocol.ToolCall{Name: "observed"}); err == nil {
		t.Fatal("Run succeeded, want the pre_tool hook error to abort the call")
	}
}
