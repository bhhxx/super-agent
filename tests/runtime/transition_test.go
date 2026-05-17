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
	mutationType  Mutation // asserted when mutationCount == 1
	effectType    Effect   // asserted when effectCount == 1
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
			mutationType: AppendUserMessage{}, effectType: CallModel{},
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
			mutationType:  AppendAssistantMessage{},
		},
		{
			name: "AssistantMessageReceived/rejects_when_not_WaitingLLM", state: StateIdle,
			event:   AssistantMessageReceived{Response: ModelResponse{Content: "hi"}},
			wantErr: true,
		},

		// --- ToolCallBatchFirstNeedsApproval ---
		{
			name: "ToolCallBatchFirstNeedsApproval/WaitingLLM->WaitingApproval", state: StateWaitingLLM,
			event: ToolCallBatchFirstNeedsApproval{
				Content: "thinking", Calls: sampleToolCalls(), ReasoningContent: "reasoning",
			},
			wantState:     StateWaitingApproval,
			mutationCount: 3, // AppendAssistantMessage + SetQueuedToolCalls + SetPendingTool
			effectCount:   0,
		},
		{
			name: "ToolCallBatchFirstNeedsApproval/rejects_when_not_WaitingLLM", state: StateIdle,
			event:   ToolCallBatchFirstNeedsApproval{Calls: sampleToolCalls()},
			wantErr: true,
		},

		// --- ToolCallBatchFirstReadyToRun ---
		{
			name: "ToolCallBatchFirstReadyToRun/WaitingLLM->RunningTool", state: StateWaitingLLM,
			event: ToolCallBatchFirstReadyToRun{
				Content: "thinking", Calls: sampleToolCalls(), ReasoningContent: "reasoning",
			},
			wantState:     StateRunningTool,
			mutationCount: 2, // AppendAssistantMessage + SetQueuedToolCalls
			effectCount:   1,
			effectType:    RunTool{},
		},
		{
			name: "ToolCallBatchFirstReadyToRun/rejects_when_not_WaitingLLM", state: StateIdle,
			event:   ToolCallBatchFirstReadyToRun{Calls: sampleToolCalls()},
			wantErr: true,
		},

		// --- ApprovalGranted ---
		{
			name: "ApprovalGranted/WaitingApproval->RunningTool", state: StateWaitingApproval,
			event:         ApprovalGranted{Call: sampleToolCall()},
			wantState:     StateRunningTool,
			mutationCount: 1, mutationType: ClearPendingTool{},
			effectCount: 1, effectType: RunTool{},
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
			mutationCount: 2, // ClearPendingTool + AddAlwaysAllow
			effectCount:   1, effectType: RunTool{},
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
			effectCount:   1, effectType: ProcessNextToolCall{},
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
			mutationCount: 1, mutationType: AppendToolResult{},
			effectCount: 1, effectType: ProcessNextToolCall{},
		},
		{
			name: "ToolResultReceived/rejects_when_not_RunningTool", state: StateIdle,
			event:   ToolResultReceived{Call: sampleToolCall(), Result: "ok"},
			wantErr: true,
		},

		// --- NoMoreToolCalls ---
		{
			name: "NoMoreToolCalls/AdvancingQueue->WaitingLLM", state: StateAdvancingQueue,
			event:       NoMoreToolCalls{},
			wantState:   StateWaitingLLM,
			effectCount: 1, effectType: CallModel{},
		},
		{
			name: "NoMoreToolCalls/rejects_when_not_AdvancingQueue", state: StateIdle,
			event: NoMoreToolCalls{}, wantErr: true,
		},

		// --- QueuedToolCallNeedsApproval ---
		{
			name: "QueuedToolCallNeedsApproval/AdvancingQueue->WaitingApproval", state: StateAdvancingQueue,
			event:         QueuedToolCallNeedsApproval{Call: sampleToolCall()},
			wantState:     StateWaitingApproval,
			mutationCount: 2, // SetPendingTool + PopQueuedToolCall
		},
		{
			name: "QueuedToolCallNeedsApproval/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   QueuedToolCallNeedsApproval{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- QueuedToolCallReadyToRun ---
		{
			name: "QueuedToolCallReadyToRun/AdvancingQueue->RunningTool", state: StateAdvancingQueue,
			event:         QueuedToolCallReadyToRun{Call: sampleToolCall()},
			wantState:     StateRunningTool,
			mutationCount: 1, mutationType: PopQueuedToolCall{},
			effectCount: 1, effectType: RunTool{},
		},
		{
			name: "QueuedToolCallReadyToRun/rejects_when_not_AdvancingQueue", state: StateIdle,
			event:   QueuedToolCallReadyToRun{Call: sampleToolCall()},
			wantErr: true,
		},

		// --- ErrorOccurred ---
		{
			name: "ErrorOccurred/WaitingLLM->Idle", state: StateWaitingLLM,
			event:         ErrorOccurred{Err: errors.New("boom")},
			wantState:     StateIdle,
			mutationCount: 3, // ClearPendingTool + ClearQueuedToolCalls + ClearPendingEffects
		},
		{
			name: "ErrorOccurred/RunningTool->Idle", state: StateRunningTool,
			event:         ErrorOccurred{Err: errors.New("boom")},
			wantState:     StateIdle,
			mutationCount: 3,
		},
		{
			name: "ErrorOccurred/AdvancingQueue->Idle", state: StateAdvancingQueue,
			event:         ErrorOccurred{Err: errors.New("boom")},
			wantState:     StateIdle,
			mutationCount: 3,
		},

		// --- CancelRequested ---
		{
			name: "CancelRequested/WaitingLLM->Idle", state: StateWaitingLLM,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 3,
		},
		{
			name: "CancelRequested/WaitingApproval->Idle", state: StateWaitingApproval,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 3,
		},
		{
			name: "CancelRequested/RunningTool->Idle", state: StateRunningTool,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 3,
		},
		{
			name: "CancelRequested/AdvancingQueue->Idle", state: StateAdvancingQueue,
			event: CancelRequested{}, wantState: StateIdle,
			mutationCount: 3,
		},

		// --- ResetRequested ---
		{
			name: "ResetRequested/Idle->Idle", state: StateIdle,
			event: ResetRequested{}, wantState: StateIdle,
			mutationCount: 1, mutationType: ResetContext{},
		},
		{
			name: "ResetRequested/WaitingLLM->Idle", state: StateWaitingLLM,
			event: ResetRequested{}, wantState: StateIdle,
			mutationCount: 1, mutationType: ResetContext{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Transition(tc.state, tc.event)
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
			if tc.mutationCount == 1 && tc.mutationType != nil {
				if reflect.TypeOf(result.Mutations[0]) != reflect.TypeOf(tc.mutationType) {
					t.Fatalf("mutation[0] = %T, want %T", result.Mutations[0], tc.mutationType)
				}
			}
			if tc.effectCount == 1 && tc.effectType != nil {
				if reflect.TypeOf(result.Effects[0]) != reflect.TypeOf(tc.effectType) {
					t.Fatalf("effect[0] = %T, want %T", result.Effects[0], tc.effectType)
				}
			}
		})
	}
}
