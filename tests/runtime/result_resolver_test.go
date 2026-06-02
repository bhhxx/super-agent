package runtime_test

import (
	"testing"

	. "super-agent/runtime"
)

func TestDefaultResultResolverUsesOnlyToolBatchInput(t *testing.T) {
	resolver := DefaultResultResolver{}
	event, err := resolver.Resolve(ToolQueueChecked{}, ResultResolveInput{
		ToolBatch: &ToolCallBatch{
			ID:    "batch-1",
			Calls: []ToolCall{{ID: "call-1", Name: "bash", Input: "pwd"}},
		},
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	next, ok := event.(ToolCallAvailable)
	if !ok {
		t.Fatalf("event = %T, want ToolCallAvailable", event)
	}
	if next.Call.ID != "call-1" {
		t.Fatalf("call = %+v, want call-1", next.Call)
	}
}
