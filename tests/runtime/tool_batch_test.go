package runtime_test

import (
	"testing"

	. "super-agent/runtime"
)

func TestToolBatchReceivedAdvancesThroughUnifiedApprovalEvents(t *testing.T) {
	calls := []ToolCall{
		{ID: "call-1", Name: "bash", Input: "one"},
		{ID: "call-2", Name: "bash", Input: "two"},
	}

	startEvent := ToolBatchReceived{
		Content: "need tools",
		Calls:   calls,
	}
	start, err := Transition(transitionSnapshot(StateWaitingLLM, startEvent), startEvent)
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if start.NextState != StateAdvancingQueue {
		t.Fatalf("state = %s, want %s", start.NextState, StateAdvancingQueue)
	}
	if len(start.Effects) != 1 {
		t.Fatalf("effects = %+v, want one ProcessNextToolCall", start.Effects)
	}
	if _, ok := start.Effects[0].(ProcessNextToolCall); !ok {
		t.Fatalf("effect = %T, want ProcessNextToolCall", start.Effects[0])
	}

	firstEvent := ToolCallNeedsApproval{Call: calls[0]}
	first, err := Transition(transitionSnapshot(StateAdvancingQueue, firstEvent), firstEvent)
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if first.NextState != StateWaitingApproval {
		t.Fatalf("state = %s, want %s", first.NextState, StateWaitingApproval)
	}

	secondEvent := ToolCallNeedsApproval{Call: calls[1]}
	second, err := Transition(transitionSnapshot(StateAdvancingQueue, secondEvent), secondEvent)
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if second.NextState != StateWaitingApproval {
		t.Fatalf("state = %s, want %s", second.NextState, StateWaitingApproval)
	}
}

func TestSnapshotIncludesPendingToolBatchProgress(t *testing.T) {
	batch := ToolCallBatch{
		ID: "batch-1",
		Calls: []ToolCall{
			{ID: "call-1", Name: "bash", Input: "one"},
			{ID: "call-2", Name: "bash", Input: "two"},
		},
		Index: 2,
	}

	snapshot := Snapshot{
		PendingTool:           &batch.Calls[1],
		PendingToolBatchID:    batch.ID,
		PendingToolBatchIndex: batch.Index,
		PendingToolBatchTotal: len(batch.Calls),
	}

	if snapshot.PendingToolBatchIndex != 2 || snapshot.PendingToolBatchTotal != 2 {
		t.Fatalf("batch progress = %d/%d, want 2/2", snapshot.PendingToolBatchIndex, snapshot.PendingToolBatchTotal)
	}
}
