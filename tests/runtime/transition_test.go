package runtime_test

import (
	"errors"
	"reflect"
	"testing"

	. "super-agent/runtime"
)

// transitionCase describes one (State, Event) -> TransitionResult expectation.
// mutationCount/effectCount avoid brittle type-assertion lists while still
// catching missing or extra outputs. mutationType/effectType assert the first
// item's concrete type when there is exactly one.
type transitionCase struct {
	name          string
	state         State
	event         Event
	wantState     State
	wantErr       bool
	mutationCount int
	effectCount   int
	mutationTypes []Mutation
	effectTypes   []Effect
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
	engineState := EngineState{State: state}
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
			mutationCount: 1, effectCount: 1,
			mutationTypes: []Mutation{AppendUserMessage{}},
			effectTypes:   []Effect{CallModel{}},
		},
		{
			name: "UserMessageSubmitted/rejects_when_not_idle", state: StateWaitingLLM,
			event: UserMessageSubmitted{Content: "hi"}, wantErr: true,
		},

		// --- AssistantMessageReceived ---
		{
			name: "AssistantMessageReceived/WaitingLLM->Idle", state: StateWaitingLLM,
			event:         AssistantMessageReceived{Response: ModelResponse{Content: "hi"}},
			wantState:     StateIdle,
			mutationCount: 1,
			mutationTypes: []Mutation{AppendAssistantMessage{}},
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
			wantState:     StateAdvancingQueue,
			mutationCount: 2, // AppendAssistantMessage + SetToolCallBatch
			effectCount:   1,
			mutationTypes: []Mutation{AppendAssistantMessage{}, SetToolCallBatch{}},
			effectTypes:   []Effect{ProcessNextToolCall{}},
		},
		{
			name: "ToolBatchReceived/rejects_when_not_WaitingLLM", state: StateIdle,
			event:   ToolBatchReceived{Calls: sampleToolCalls()},
			wantErr: true,
		},

		// --- ToolBatchFinished ---
		{
			name: "ToolBatchFinished/AdvancingQueue->WaitingLLM", state: StateAdvancingQueue,
			event:         ToolBatchFinished{},
			wantState:     StateWaitingLLM,
			mutationCount: 1,
			effectCount:   1,
			mutationTypes: []Mutation{ClearToolCallBatch{}},
			effectTypes:   []Effect{CallModel{}},
		},
		{
			name: "ToolBatchFinished/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolBatchFinished{},
			wantErr: true,
		},

		// --- ApprovalGranted ---
		{
			name: "ApprovalGranted/WaitingApproval->RunningTool", state: StateWaitingApproval,
			event:         ApprovalGranted{Call: sampleToolCall()},
			wantState:     StateRunningTool,
			mutationCount: 2,
			effectCount:   1,
			mutationTypes: []Mutation{SetCurrentTool{}, ClearPendingTool{}},
			effectTypes:   []Effect{RunTool{}},
		},
		{
			name: "ApprovalGranted/rejects_when_not_WaitingApproval", state: StateIdle,
			event:   ApprovalGranted{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ApprovalAlwaysGranted ---
		{
			name: "ApprovalAlwaysGranted/WaitingApproval->RunningTool", state: StateWaitingApproval,
			event:         ApprovalAlwaysGranted{Call: sampleToolCall()},
			wantState:     StateRunningTool,
			mutationCount: 2,
			effectCount:   1,
			mutationTypes: []Mutation{SetCurrentTool{}, ClearPendingTool{}},
			effectTypes:   []Effect{RunTool{}},
		},
		{
			name: "ApprovalAlwaysGranted/rejects_when_not_WaitingApproval", state: StateIdle,
			event:   ApprovalAlwaysGranted{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ApprovalDenied ---
		{
			name: "ApprovalDenied/WaitingApproval->AdvancingQueue", state: StateWaitingApproval,
			event:         ApprovalDenied{Call: sampleToolCall()},
			wantState:     StateAdvancingQueue,
			mutationCount: 2, // ClearPendingTool + AppendToolResult
			effectCount:   1,
			mutationTypes: []Mutation{ClearPendingTool{}, AppendToolResult{}},
			effectTypes:   []Effect{ProcessNextToolCall{}},
		},
		{
			name: "ApprovalDenied/rejects_when_not_WaitingApproval", state: StateIdle,
			event:   ApprovalDenied{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ToolResultReceived ---
		{
			name: "ToolResultReceived/RunningTool->AdvancingQueue", state: StateRunningTool,
			event:         ToolResultReceived{Call: sampleToolCall(), Result: "ok"},
			wantState:     StateAdvancingQueue,
			mutationCount: 2,
			effectCount:   1,
			mutationTypes: []Mutation{AppendToolResult{}, ClearCurrentTool{}},
			effectTypes:   []Effect{ProcessNextToolCall{}},
		},
		{
			name: "ToolResultReceived/rejects_when_not_RunningTool", state: StateIdle,
			event:   ToolResultReceived{Call: sampleToolCall(), Result: "ok"},
			wantErr: true,
		},

		// --- ToolCallNeedsApproval ---
		{
			name: "ToolCallNeedsApproval/AdvancingQueue->WaitingApproval", state: StateAdvancingQueue,
			event:         ToolCallNeedsApproval{Call: sampleToolCall()},
			wantState:     StateWaitingApproval,
			mutationCount: 2, // SetPendingTool + AdvanceToolCallBatch
			mutationTypes: []Mutation{SetPendingTool{}, AdvanceToolCallBatch{}},
		},
		{
			name: "ToolCallNeedsApproval/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolCallNeedsApproval{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ToolCallReadyToRun ---
		{
			name: "ToolCallReadyToRun/AdvancingQueue->RunningTool", state: StateAdvancingQueue,
			event:         ToolCallReadyToRun{Call: sampleToolCall()},
			wantState:     StateRunningTool,
			mutationCount: 2,
			effectCount:   1,
			mutationTypes: []Mutation{AdvanceToolCallBatch{}, SetCurrentTool{}},
			effectTypes:   []Effect{RunTool{}},
		},
		{
			name: "ToolCallReadyToRun/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   ToolCallReadyToRun{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ErrorOccurred ---
		{
			name: "ErrorOccurred/WaitingLLM->Idle", state: StateWaitingLLM,
			event:         ErrorOccurred{Err: errors.New("boom")},
			wantState:     StateIdle,
			mutationCount: 6,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},
		{
			name: "ErrorOccurred/RunningTool->Idle", state: StateRunningTool,
			event:         ErrorOccurred{Err: errors.New("boom")},
			wantState:     StateIdle,
			mutationCount: 6,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},
		{
			name: "ErrorOccurred/AdvancingQueue->Idle", state: StateAdvancingQueue,
			event:         ErrorOccurred{Err: errors.New("boom")},
			wantState:     StateIdle,
			mutationCount: 6,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, AppendToolResult{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},

		// --- CancelRequested ---
		{
			name: "CancelRequested/WaitingLLM->Idle", state: StateWaitingLLM,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 5,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},
		{
			name: "CancelRequested/WaitingApproval->Idle", state: StateWaitingApproval,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 5,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},
		{
			name: "CancelRequested/RunningTool->Idle", state: StateRunningTool,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 5,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},
		{
			name: "CancelRequested/AdvancingQueue->Idle", state: StateAdvancingQueue,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 5,
			mutationTypes: []Mutation{FlushStreamingAssistant{}, ClearPendingTool{}, ClearCurrentTool{}, ClearToolCallBatch{}, ClearPendingEffects{}},
		},

		// --- ResetRequested ---
		{
			name: "ResetRequested/Idle->Idle", state: StateIdle,
			event: ResetRequested{}, wantState: StateIdle,
			mutationCount: 1,
			mutationTypes: []Mutation{ResetContext{}},
		},
		{
			name: "ResetRequested/WaitingLLM->Idle", state: StateWaitingLLM,
			event: ResetRequested{}, wantState: StateIdle,
			mutationCount: 1,
			mutationTypes: []Mutation{ResetContext{}},
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
			if len(result.Mutations) != tc.mutationCount {
				t.Fatalf("mutations = %d (%+v), want %d", len(result.Mutations), result.Mutations, tc.mutationCount)
			}
			if len(result.Effects) != tc.effectCount {
				t.Fatalf("effects = %d (%+v), want %d", len(result.Effects), result.Effects, tc.effectCount)
			}
			for i, want := range tc.mutationTypes {
				if reflect.TypeOf(result.Mutations[i]) != reflect.TypeOf(want) {
					t.Fatalf("mutation[%d] = %T, want %T", i, result.Mutations[i], want)
				}
			}
			for i, want := range tc.effectTypes {
				if reflect.TypeOf(result.Effects[i]) != reflect.TypeOf(want) {
					t.Fatalf("effect[%d] = %T, want %T", i, result.Effects[i], want)
				}
			}
		})
	}
}
