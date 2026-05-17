package runtime_test

import (
	"testing"

	. "super-agent/runtime"
)

func TestDefaultEventClassifierTurnsRiskyToolIntoApprovalEvent(t *testing.T) {
	store := NewMemoryApprovalStore()
	classifier := NewDefaultEventClassifier(NewDefaultPolicy(store), store)
	event, err := classifier.Classify(ToolCallsReceived{
		Calls: []ToolCall{{ID: "call-1", Name: "bash", Input: `{"command":"rm -rf /"}`}},
	}, EventClassifyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if _, ok := event.(ToolCallBatchFirstNeedsApproval); !ok {
		t.Fatalf("event = %T, want ToolCallBatchFirstNeedsApproval", event)
	}
}

func TestDefaultEventClassifierRejectsToolCallsWhenNoToolsAreConfigured(t *testing.T) {
	store := NewMemoryApprovalStore()
	classifier := NewDefaultEventClassifier(NewDefaultPolicy(store), store)
	_, err := classifier.Classify(ToolCallsReceived{
		Calls: []ToolCall{{ID: "call-1", Name: "bash", Input: "pwd"}},
	}, EventClassifyInput{})
	if err == nil {
		t.Fatal("Classify succeeded with no tool specs")
	}
}
