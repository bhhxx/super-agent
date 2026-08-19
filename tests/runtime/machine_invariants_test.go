package runtime_test

import (
	"context"
	"errors"
	"testing"

	. "super-agent/runtime"
)

func TestSnapshotFromRejectsInvalidMachineContext(t *testing.T) {
	cases := []EngineState{
		{State: StateAdvancingQueue},
		{State: StateWaitingApproval},
		{State: StateRunningTool},
		{State: StateIdle, ToolBatch: &ToolCallBatch{}},
	}
	for _, state := range cases {
		_, err := SnapshotFrom(state)
		var invariant InvariantViolationError
		if !errors.As(err, &invariant) {
			t.Fatalf("SnapshotFrom(%s) error = %v, want InvariantViolationError", state.State, err)
		}
	}
}

func TestTransitionClassifiesStateMismatchAsUnexpectedEvent(t *testing.T) {
	event := ToolResultReceived{Call: ToolCall{ID: "call-1"}, Result: "ok"}
	_, err := Transition(transitionSnapshot(StateIdle, event), event)
	var unexpected UnexpectedEventError
	if !errors.As(err, &unexpected) {
		t.Fatalf("error = %v, want UnexpectedEventError", err)
	}
}

func TestTransitionRejectsApprovalForDifferentCall(t *testing.T) {
	pending := ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
	request := PermissionRequest{}
	state := EngineState{
		State:             StateWaitingApproval,
		PendingTool:       &pending,
		PendingPermission: &request,
		ToolBatch:         &ToolCallBatch{Calls: []ToolCall{pending}, Index: 1},
	}
	snapshot, err := SnapshotFrom(state)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Transition(snapshot, ApprovalGranted{Call: ToolCall{ID: "call-2", Name: "bash", Input: "pwd"}})
	var protocol ProtocolViolationError
	if !errors.As(err, &protocol) {
		t.Fatalf("error = %v, want ProtocolViolationError", err)
	}
}

func TestTransitionRejectsResultForDifferentCurrentCall(t *testing.T) {
	current := ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
	state := EngineState{
		State:       StateRunningTool,
		CurrentTool: &current,
		ToolBatch:   &ToolCallBatch{Calls: []ToolCall{current}, Index: 1},
	}
	snapshot, err := SnapshotFrom(state)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Transition(snapshot, ToolResultReceived{Call: ToolCall{ID: "call-2"}, Result: "ok"})
	var protocol ProtocolViolationError
	if !errors.As(err, &protocol) {
		t.Fatalf("error = %v, want ProtocolViolationError", err)
	}
}

func TestTransitionRejectsBatchFinishedBeforeQueueEmpty(t *testing.T) {
	call := ToolCall{ID: "call-1"}
	state := EngineState{State: StateAdvancingQueue, ToolBatch: &ToolCallBatch{Calls: []ToolCall{call}}}
	snapshot, err := SnapshotFrom(state)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Transition(snapshot, ToolBatchFinished{})
	var protocol ProtocolViolationError
	if !errors.As(err, &protocol) {
		t.Fatalf("error = %v, want ProtocolViolationError", err)
	}
}

func TestReducerDoesNotMutateOriginalStateWhenValidationFails(t *testing.T) {
	call := ToolCall{ID: "call-1"}
	original := EngineState{
		State:     StateAdvancingQueue,
		ToolBatch: &ToolCallBatch{Calls: []ToolCall{call}},
	}
	_, err := (DefaultReducer{}).Reduce(original, TransitionResult{
		NextState: StateRunningTool,
		Mutations: []Mutation{
			AdvanceToolCallBatch{},
			ClearPendingEffects{},
		},
	})
	var invariant InvariantViolationError
	if !errors.As(err, &invariant) {
		t.Fatalf("error = %v, want InvariantViolationError", err)
	}
	if original.State != StateAdvancingQueue || original.ToolBatch.Index != 0 || original.CurrentTool != nil {
		t.Fatalf("original state mutated after failed reduction: %+v", original)
	}
}

func TestReducerDescribesSchedulerMutationAfterValidReduction(t *testing.T) {
	original := EngineState{State: StateWaitingLLM}
	reduction, err := (DefaultReducer{}).Reduce(original, TransitionResult{
		NextState: StateIdle,
		Mutations: []Mutation{ClearPendingEffects{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reduction.EffectOps) != 1 {
		t.Fatalf("effect ops = %+v, want one clear operation", reduction.EffectOps)
	}
	if _, ok := reduction.EffectOps[0].(ClearPendingEffectsOp); !ok {
		t.Fatalf("effect op = %T, want ClearPendingEffectsOp", reduction.EffectOps[0])
	}
	if original.State != StateWaitingLLM {
		t.Fatalf("original state = %s, want WaitingLLM", original.State)
	}
}

type invalidReducer struct{}

func (invalidReducer) Reduce(state EngineState, result TransitionResult) (Reduction, error) {
	if state.State == StateInitializing {
		return (DefaultReducer{}).Reduce(state, result)
	}
	return Reduction{State: EngineState{
		State:     StateIdle,
		ToolBatch: &ToolCallBatch{},
	}}, nil
}

func TestEngineDoesNotCommitInvalidCustomReduction(t *testing.T) {
	approvals := NewMemoryApprovalStore()
	runs := NewDefaultRunController()
	engine := NewEngineWithComponents(
		NewDefaultEffectRunner(NewDefaultEffectExecutor(nil, nil)),
		NewDefaultOutcomeResolver(NewDefaultPolicy(), approvals),
		invalidReducer{},
		runs,
		approvals,
		nil,
	)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	err := engine.DispatchEventThenRunEffects(context.Background(), UserMessageSubmitted{Content: "hi"}, nil, func() {})
	var invariant InvariantViolationError
	if !errors.As(err, &invariant) {
		t.Fatalf("error = %v, want InvariantViolationError", err)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want unchanged Idle", engine.State())
	}
	if _, ok := runs.CurrentContext(); ok {
		t.Fatal("failed reduction left an active run context")
	}
}

func TestToolFlowPreservesMachineInvariants(t *testing.T) {
	call := ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
	state := EngineState{State: StateWaitingLLM}
	events := []Event{
		ToolBatchReceived{Calls: []ToolCall{call}},
		ToolCallNeedsApproval{Call: call, Request: PermissionRequest{ToolName: "bash"}},
		ApprovalGranted{Call: call},
		ToolResultReceived{Call: call, Result: "ok"},
		ToolBatchFinished{},
	}
	wantStates := []State{
		StateAdvancingQueue,
		StateWaitingApproval,
		StateRunningTool,
		StateAdvancingQueue,
		StateWaitingLLM,
	}
	for i, event := range events {
		snapshot, err := SnapshotFrom(state)
		if err != nil {
			t.Fatalf("step %d snapshot: %v", i, err)
		}
		result, err := Transition(snapshot, event)
		if err != nil {
			t.Fatalf("step %d transition: %v", i, err)
		}
		reduction, err := (DefaultReducer{}).Reduce(state, result)
		if err != nil {
			t.Fatalf("step %d reduction: %v", i, err)
		}
		state = reduction.State
		if state.State != wantStates[i] {
			t.Fatalf("step %d state = %s, want %s", i, state.State, wantStates[i])
		}
	}
}
