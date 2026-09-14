package runtime_test

import (
	"errors"
	"reflect"
	"testing"

	. "super-agent/runtime"
)

// transitionCase describes one (State, Event) -> TransitionResult expectation.
// Counts catch missing or extra outputs. Type lists assert exact order.
type transitionCase struct {
	name                   string
	state                  State
	event                  Event
	wantState              State
	wantErr                bool
	runtimeDataChangeCount int
	clearExistingActions   bool
	scheduledActionCount   int
	runtimeDataChangeTypes []RuntimeDataChange
	scheduledActionTypes   []ScheduledAction
}

func sampleToolCall() ToolCall {
	return ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
}

func sampleToolCalls() []ToolCall {
	return []ToolCall{
		{ID: "call-1", Name: "first", Input: "a"},
		{ID: "call-2", Name: "second", Input: "b"},
	}
}

func transitionSnapshot(state State, event Event) MachineSnapshot {
	engineState := RuntimeData{State: state}
	call := sampleToolCall()
	switch ev := event.(type) {
	case ApprovalGranted:
		call = ev.Call
	case ApprovalAlwaysGranted:
		call = ev.Call
	case ApprovalDenied:
		call = ev.Call
	case ToolResultReceived:
		call = ev.Call
	case ToolCallNeedsApproval:
		call = ev.Call
	case ToolCallReadyToRun:
		call = ev.Call
	case ToolCallDenied:
		call = ev.Call
	}
	switch state {
	case StateAdvancingQueue:
		engineState.ToolBatch = &ToolCallBatch{Calls: []ToolCall{call}}
		if _, ok := event.(ToolBatchFinished); ok {
			engineState.ToolBatch.Index = 1
		}
	case StateWaitingApproval:
		request := PermissionRequest{}
		engineState.PendingTool = &call
		engineState.PendingPermission = &request
		engineState.ToolBatch = &ToolCallBatch{Calls: []ToolCall{call}, Index: 1}
	case StateRunningTool:
		engineState.CurrentTool = &call
		engineState.ToolBatch = &ToolCallBatch{Calls: []ToolCall{call}, Index: 1}
	}
	snapshot, err := SnapshotFrom(engineState)
	if err != nil {
		panic(err)
	}
	return snapshot
}

func TestTransitionTable(t *testing.T) {
	cases := []transitionCase{
		// --- EngineReady ---
		{
			name: "EngineReady/Initializing->Idle", state: StateInitializing,
			event: EngineReady{}, wantState: StateIdle,
		},

		// --- UserMessageSubmitted ---
		{
			name: "UserMessageSubmitted/Idle->WaitingLLM", state: StateIdle,
			event: UserMessageSubmitted{Content: "hi"}, wantState: StateWaitingLLM,
			runtimeDataChangeCount: 1, scheduledActionCount: 1,
			runtimeDataChangeTypes: []RuntimeDataChange{AppendUserMessage{}},
			scheduledActionTypes:   []ScheduledAction{CallModel{}},
		},
		{
			name: "UserMessageSubmitted/rejects_when_not_idle", state: StateWaitingLLM,
			event: UserMessageSubmitted{Content: "hi"}, wantErr: true,
		},

		// --- AssistantMessageReceived ---
		{
			name: "AssistantMessageReceived/WaitingLLM->Idle", state: StateWaitingLLM,
			event:                  AssistantMessageReceived{Response: ModelResponse{Content: "hi"}},
			wantState:              StateIdle,
			runtimeDataChangeCount: 1,
			runtimeDataChangeTypes: []RuntimeDataChange{AppendAssistantMessage{}},
		},
		{
			name: "AssistantMessageReceived/rejects_when_not_WaitingLLM", state: StateIdle,
			event:   AssistantMessageReceived{Response: ModelResponse{Content: "hi"}},
			wantErr: true,
		},

		// --- ToolBatchReceived ---
		{
			name: "ToolBatchReceived/WaitingLLM->AdvancingQueue", state: StateWaitingLLM,
			event: ToolBatchReceived{
				Content: "thinking", Calls: sampleToolCalls(), ReasoningContent: "reasoning",
			},
			wantState:              StateAdvancingQueue,
			runtimeDataChangeCount: 2, // AppendAssistantMessage + SetToolCallBatch
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{AppendAssistantMessage{}, SetToolCallBatch{}},
			scheduledActionTypes:   []ScheduledAction{CheckToolQueue{}},
		},
		{
			name: "ToolBatchReceived/rejects_when_not_WaitingLLM", state: StateIdle,
			event:   ToolBatchReceived{Calls: sampleToolCalls()},
			wantErr: true,
		},

		// --- ToolBatchFinished ---
		{
			name: "ToolBatchFinished/AdvancingQueue->WaitingLLM", state: StateAdvancingQueue,
			event:                  ToolBatchFinished{},
			wantState:              StateWaitingLLM,
			runtimeDataChangeCount: 1,
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{ClearToolCallBatch{}},
			scheduledActionTypes:   []ScheduledAction{CallModel{}},
		},
		{
			name: "ToolBatchFinished/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolBatchFinished{},
			wantErr: true,
		},

		// --- ApprovalGranted ---
		{
			name: "ApprovalGranted/WaitingApproval->RunningTool", state: StateWaitingApproval,
			event:                  ApprovalGranted{Call: sampleToolCall()},
			wantState:              StateRunningTool,
			runtimeDataChangeCount: 2,
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{SetCurrentTool{}, ClearPendingTool{}},
			scheduledActionTypes:   []ScheduledAction{RunTool{}},
		},
		{
			name: "ApprovalGranted/rejects_when_not_WaitingApproval", state: StateIdle,
			event:   ApprovalGranted{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ApprovalAlwaysGranted ---
		{
			name: "ApprovalAlwaysGranted/WaitingApproval->RunningTool", state: StateWaitingApproval,
			event:                  ApprovalAlwaysGranted{Call: sampleToolCall()},
			wantState:              StateRunningTool,
			runtimeDataChangeCount: 2,
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{SetCurrentTool{}, ClearPendingTool{}},
			scheduledActionTypes:   []ScheduledAction{RunTool{}},
		},
		{
			name: "ApprovalAlwaysGranted/rejects_when_not_WaitingApproval", state: StateIdle,
			event:   ApprovalAlwaysGranted{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ApprovalDenied ---
		{
			name: "ApprovalDenied/WaitingApproval->AdvancingQueue", state: StateWaitingApproval,
			event:                  ApprovalDenied{Call: sampleToolCall()},
			wantState:              StateAdvancingQueue,
			runtimeDataChangeCount: 2, // ClearPendingTool + AppendToolResult
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{ClearPendingTool{}, AppendToolResult{}},
			scheduledActionTypes:   []ScheduledAction{CheckToolQueue{}},
		},
		{
			name: "ApprovalDenied/rejects_when_not_WaitingApproval", state: StateIdle,
			event:   ApprovalDenied{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ToolResultReceived ---
		{
			name: "ToolResultReceived/RunningTool->AdvancingQueue", state: StateRunningTool,
			event:                  ToolResultReceived{Call: sampleToolCall(), Result: "ok"},
			wantState:              StateAdvancingQueue,
			runtimeDataChangeCount: 2,
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{AppendToolResult{}, ClearCurrentTool{}},
			scheduledActionTypes:   []ScheduledAction{CheckToolQueue{}},
		},
		{
			name: "ToolResultReceived/rejects_when_not_RunningTool", state: StateIdle,
			event:   ToolResultReceived{Call: sampleToolCall(), Result: "ok"},
			wantErr: true,
		},

		// --- ToolCallNeedsApproval ---
		{
			name: "ToolCallNeedsApproval/AdvancingQueue->WaitingApproval", state: StateAdvancingQueue,
			event:                  ToolCallNeedsApproval{Call: sampleToolCall()},
			wantState:              StateWaitingApproval,
			runtimeDataChangeCount: 2, // SetPendingTool + AdvanceToolCallBatch
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{SetPendingTool{}, AdvanceToolCallBatch{}},
			scheduledActionTypes:   []ScheduledAction{AwaitApproval{}},
		},
		{
			name: "ToolCallNeedsApproval/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolCallNeedsApproval{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ToolCallReadyToRun ---
		{
			name: "ToolCallReadyToRun/AdvancingQueue->RunningTool", state: StateAdvancingQueue,
			event:                  ToolCallReadyToRun{Call: sampleToolCall()},
			wantState:              StateRunningTool,
			runtimeDataChangeCount: 2,
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{AdvanceToolCallBatch{}, SetCurrentTool{}},
			scheduledActionTypes:   []ScheduledAction{RunTool{}},
		},
		{
			name: "ToolCallReadyToRun/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolCallReadyToRun{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ToolCallDenied ---
		{
			name: "ToolCallDenied/AdvancingQueue->AdvancingQueue", state: StateAdvancingQueue,
			event:                  ToolCallDenied{Call: sampleToolCall(), Reason: "plan mode"},
			wantState:              StateAdvancingQueue,
			runtimeDataChangeCount: 2,
			scheduledActionCount:   1,
			runtimeDataChangeTypes: []RuntimeDataChange{AdvanceToolCallBatch{}, AppendToolResult{}},
			scheduledActionTypes:   []ScheduledAction{CheckToolQueue{}},
		},
		{
			name: "ToolCallDenied/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolCallDenied{Call: sampleToolCall(), Reason: "plan mode"},
			wantErr: true,
		},

		// --- ErrorOccurred ---
		{
			// No tool call reached the transcript, so none may be answered: a
			// tool result without a matching tool call is rejected by the
			// provider on the next request.
			name: "ErrorOccurred/WaitingLLM->Idle", state: StateWaitingLLM,
			event:                  ErrorOccurred{Err: errors.New("boom")},
			wantState:              StateIdle,
			runtimeDataChangeCount: 4, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},
		{
			name: "ErrorOccurred/RunningTool->Idle", state: StateRunningTool,
			event:                  ErrorOccurred{Err: errors.New("boom")},
			wantState:              StateIdle,
			runtimeDataChangeCount: 5, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},
		{
			name: "ErrorOccurred/AdvancingQueue->Idle", state: StateAdvancingQueue,
			event:                  ErrorOccurred{Err: errors.New("boom")},
			wantState:              StateIdle,
			runtimeDataChangeCount: 5, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},

		// --- CancelRequested ---
		{
			name: "CancelRequested/WaitingLLM->Idle", state: StateWaitingLLM,
			event: CancelRequested{}, wantState: StateIdle,
			runtimeDataChangeCount: 4, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},
		{
			// Cancelling while a call awaits approval must still answer that
			// call, exactly like the error path: an unanswered tool call makes
			// the persisted transcript unresumable.
			name: "CancelRequested/WaitingApproval->Idle", state: StateWaitingApproval,
			event: CancelRequested{}, wantState: StateIdle,
			runtimeDataChangeCount: 5, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},
		{
			name: "CancelRequested/RunningTool->Idle", state: StateRunningTool,
			event: CancelRequested{}, wantState: StateIdle,
			runtimeDataChangeCount: 5, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},
		{
			name: "CancelRequested/AdvancingQueue->Idle", state: StateAdvancingQueue,
			event: CancelRequested{}, wantState: StateIdle,
			runtimeDataChangeCount: 5, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}},
		},

		// --- ResetRequested ---
		{
			name: "ResetRequested/Idle->Idle", state: StateIdle,
			event: ResetRequested{}, wantState: StateIdle,
			runtimeDataChangeCount: 1, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{ResetConversation{}},
		},
		{
			name: "ResetRequested/WaitingLLM->Idle", state: StateWaitingLLM,
			event: ResetRequested{}, wantState: StateIdle,
			runtimeDataChangeCount: 1, clearExistingActions: true,
			runtimeDataChangeTypes: []RuntimeDataChange{ResetConversation{}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Transition(transitionSnapshot(tc.state, tc.event), tc.event)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.NextState != tc.wantState {
				t.Fatalf("nextState = %s, want %s", result.NextState, tc.wantState)
			}
			if len(result.RuntimeDataChanges) != tc.runtimeDataChangeCount {
				t.Fatalf("runtime data changes = %d (%+v), want %d", len(result.RuntimeDataChanges), result.RuntimeDataChanges, tc.runtimeDataChangeCount)
			}
			if result.ActionPlan.ClearExisting != tc.clearExistingActions {
				t.Fatalf("clear existing actions = %t, want %t", result.ActionPlan.ClearExisting, tc.clearExistingActions)
			}
			if len(result.ActionPlan.Schedule) != tc.scheduledActionCount {
				t.Fatalf("scheduled actions = %d (%+v), want %d", len(result.ActionPlan.Schedule), result.ActionPlan.Schedule, tc.scheduledActionCount)
			}
			for i, want := range tc.runtimeDataChangeTypes {
				if reflect.TypeOf(result.RuntimeDataChanges[i]) != reflect.TypeOf(want) {
					t.Fatalf("runtime data change[%d] = %T, want %T", i, result.RuntimeDataChanges[i], want)
				}
			}
			for i, want := range tc.scheduledActionTypes {
				if reflect.TypeOf(result.ActionPlan.Schedule[i]) != reflect.TypeOf(want) {
					t.Fatalf("scheduled action[%d] = %T, want %T", i, result.ActionPlan.Schedule[i], want)
				}
			}
		})
	}
}
