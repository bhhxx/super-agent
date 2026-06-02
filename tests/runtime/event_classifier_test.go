package runtime_test

import (
	"testing"

	. "super-agent/runtime"
)

func TestDefaultEventClassifierTurnsToolCallsIntoBatchEvent(t *testing.T) {
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
	if _, ok := event.(ToolBatchReceived); !ok {
		t.Fatalf("event = %T, want ToolBatchReceived", event)
	}
}

func TestDefaultEventClassifierTurnsRiskyAvailableToolIntoApprovalEvent(t *testing.T) {
	store := NewMemoryApprovalStore()
	classifier := NewDefaultEventClassifier(NewDefaultPolicy(), store)
	event, err := classifier.Classify(ToolCallAvailable{
		Call: ToolCall{ID: "call-1", Name: "bash", Input: `{"command":"touch build.txt"}`},
	}, EventClassifyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if _, ok := event.(ToolCallNeedsApproval); !ok {
		t.Fatalf("event = %T, want ToolCallNeedsApproval", event)
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

func TestAcceptEditsRunsReadOnlyGitWithoutApproval(t *testing.T) {
	policy := NewPolicy(PermissionModeAcceptEdits, PermissionRules{})

	decision := policy.ClassifyToolCall(ToolCall{Name: "bash", Input: `{"command":"git status --short"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})

	if decision != DecisionRunDirectly {
		t.Fatalf("decision = %v, want run directly", decision)
	}
}

func TestPlanModeDeniesWriteTool(t *testing.T) {
	policy := NewPolicy(PermissionModePlan, PermissionRules{})

	decision := policy.ClassifyToolCall(ToolCall{Name: "write_file", Input: `{"path":"main.go","content":"x"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "write_file", Risky: true}},
	})

	if decision != DecisionDenied {
		t.Fatalf("decision = %v, want denied", decision)
	}
}

func TestPlanModeDeniesShellWrites(t *testing.T) {
	policy := NewPolicy(PermissionModePlan, PermissionRules{})

	decision := policy.ClassifyToolCall(ToolCall{Name: "bash", Input: `{"command":"touch build.txt"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})

	if decision != DecisionDenied {
		t.Fatalf("decision = %v, want denied", decision)
	}
}

func TestDestructiveCommandNeedsApproval(t *testing.T) {
	policy := NewPolicy(PermissionModeAcceptEdits, PermissionRules{})

	decision := policy.ClassifyToolCall(ToolCall{Name: "bash", Input: `{"command":"rm -rf build"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})

	if decision != DecisionNeedsApproval {
		t.Fatalf("decision = %v, want needs approval", decision)
	}
}

func TestProtectedPathDenied(t *testing.T) {
	policy := NewPolicy(PermissionModeBypass, PermissionRules{})

	decision := policy.ClassifyToolCall(ToolCall{Name: "write_file", Input: `{"path":".env","content":"secret"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "write_file", Risky: true}},
	})

	if decision != DecisionDenied {
		t.Fatalf("decision = %v, want denied", decision)
	}
}

func TestNetworkDeniedByDefault(t *testing.T) {
	policy := NewPolicy(PermissionModeAcceptEdits, PermissionRules{})

	decision := policy.ClassifyToolCall(ToolCall{Name: "bash", Input: `{"command":"curl https://example.com"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})

	if decision != DecisionNeedsApproval {
		t.Fatalf("decision = %v, want needs approval", decision)
	}
}

func TestAllowedPathRunsDirectly(t *testing.T) {
	policy := NewPolicy(PermissionModeAsk, PermissionRules{AllowPaths: []string{"generated"}})

	decision := policy.ClassifyToolCall(ToolCall{Name: "write_file", Input: `{"path":"generated/out.txt","content":"x"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "write_file", Risky: true}},
	})

	if decision != DecisionRunDirectly {
		t.Fatalf("decision = %v, want run directly", decision)
	}
}

func TestDeniedEnvIsDenied(t *testing.T) {
	policy := NewPolicy(PermissionModeBypass, PermissionRules{DenyEnv: []string{"AWS_PROFILE"}})

	decision := policy.ClassifyToolCall(ToolCall{Name: "bash", Input: `{"command":"AWS_PROFILE=prod aws s3 ls"}`}, ToolPolicyInput{
		ToolSpecs: []ToolSpec{{Name: "bash", Risky: true}},
	})

	if decision != DecisionDenied {
		t.Fatalf("decision = %v, want denied", decision)
	}
}

func TestApprovalStoreStoresPermissionPolicy(t *testing.T) {
	store := NewMemoryApprovalStore()
	store.SetPermissionPolicy(PermissionModePlan, PermissionRules{AllowTools: []string{"read_file"}})

	if store.PermissionMode() != PermissionModePlan {
		t.Fatalf("mode = %q, want plan", store.PermissionMode())
	}
	rules := store.PermissionRules()
	if len(rules.AllowTools) != 1 || rules.AllowTools[0] != "read_file" {
		t.Fatalf("rules = %+v, want read_file allow rule", rules)
	}
}

func TestEngineRejectsInvalidPermissionMode(t *testing.T) {
	engine := NewEngineWithComponents(
		NewDefaultEffectRunner(NewDefaultEffectExecutor(nil, nil)),
		DefaultResultResolver{},
		NewDefaultEventClassifier(NewDefaultPolicy(), NewMemoryApprovalStore()),
		DefaultReducer{},
		NewDefaultRunController(),
		NewMemoryApprovalStore(),
		nil,
	)

	err := engine.SetPermissionPolicy(PermissionMode("root"), PermissionRules{})

	if err == nil || err.Error() != "invalid permission mode: root" {
		t.Fatalf("err = %v, want invalid permission mode", err)
	}
}
