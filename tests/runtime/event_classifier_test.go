package runtime_test

import (
	"testing"

	. "super-agent/runtime"
)

func TestDefaultEventClassifierTurnsRiskyToolIntoApprovalEvent(t *testing.T) {
	store := NewMemoryApprovalStore()
	classifier := NewDefaultEventClassifier(NewDefaultPolicy(), store)
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
	classifier := NewDefaultEventClassifier(NewDefaultPolicy(), store)
	_, err := classifier.Classify(ToolCallsReceived{
		Calls: []ToolCall{{ID: "call-1", Name: "bash", Input: "pwd"}},
	}, EventClassifyInput{})
	if err == nil {
		t.Fatal("Classify succeeded with no tool specs")
	}
}

func TestDefaultPolicyDoesNotReadApprovalStore(t *testing.T) {
	policy := NewDefaultPolicy()

	decision := policy.ClassifyToolCall(ToolCall{Name: "bash", Input: "pwd"}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})

	if decision != DecisionNeedsApproval {
		t.Fatalf("decision = %v, want needs approval", decision)
	}
}
