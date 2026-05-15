package runtime_test

import (
	"context"
	"testing"

	. "super-agent/runtime"
)

type scriptedModel struct {
	responses []ModelResponse
	calls     []Message
}

func (m *scriptedModel) Next(_ context.Context, messages []Message, _ []ToolSpec, _ func(StreamChunk)) (ModelResponse, error) {
	m.calls = append(m.calls, messages[len(messages)-1])
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

type fakeTool struct {
	results map[string]string
	calls   []ToolCall
}

func (t *fakeTool) Run(_ context.Context, call ToolCall) (string, error) {
	t.calls = append(t.calls, call)
	return t.results[call.Name], nil
}

func (t *fakeTool) Specs() []ToolSpec {
	return nil
}

func TestNewEngineStartsInitializingAndReadyEntersIdle(t *testing.T) {
	engine := NewEngine(&scriptedModel{}, &fakeTool{}, nil)

	if engine.State() != StateInitializing {
		t.Fatalf("state = %s, want %s", engine.State(), StateInitializing)
	}
	engine.Ready()
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
}

func TestSubmitUserMessageProducesFinalAnswer(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{FinalAnswer: "hello", ReasoningContent: "thinking"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	engine.Ready()

	if err := engine.SubmitUserMessage(context.Background(), "hi", nil); err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}

	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if got := engine.Messages()[1]; got.Role != RoleAssistant || got.Content != "hello" || got.ReasoningContent != "thinking" {
		t.Fatalf("assistant message = %+v", got)
	}
}

func TestToolCallFeedsResultBackToModel(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCall: &ToolCall{Name: "bash", Input: "printf pong"}},
		{FinalAnswer: "tool said pong"},
	}}
	tools := &fakeTool{results: map[string]string{"bash": "pong"}}
	engine := NewEngine(model, tools, nil)
	engine.Ready()

	if err := engine.SubmitUserMessage(context.Background(), "use bash", nil); err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}

	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if len(tools.calls) != 1 || tools.calls[0].Name != "bash" {
		t.Fatalf("tool calls = %+v", tools.calls)
	}
	lastModelInput := model.calls[1]
	if lastModelInput.Role != RoleTool || lastModelInput.Content != "pong" {
		t.Fatalf("second model input = %+v", lastModelInput)
	}
}

func TestRiskyToolWaitsForShortcutApproval(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCall: &ToolCall{Name: "bash", Input: "rm -rf /", Risky: true}},
		{FinalAnswer: "approved"},
	}}
	tools := &fakeTool{results: map[string]string{"bash": "ok"}}
	engine := NewEngine(model, tools, nil)
	engine.Ready()

	if err := engine.SubmitUserMessage(context.Background(), "danger", nil); err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}
	if engine.State() != StateWaitingApproval {
		t.Fatalf("state = %s, want %s", engine.State(), StateWaitingApproval)
	}
	if _, ok := engine.PendingTool(); !ok {
		t.Fatal("pending tool not found")
	}
	if len(tools.calls) != 0 {
		t.Fatalf("tool ran before approval: %+v", tools.calls)
	}

	if err := engine.Approve(context.Background(), nil); err != nil {
		t.Fatalf("Approve failed: %v", err)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
}

func TestTransitionProducesMutationsAndEffects(t *testing.T) {
	decision, err := Transition(StateIdle, UserMessageSubmitted{Content: "hi"})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if decision.NextState != StateWaitingLLM {
		t.Fatalf("next state = %s, want %s", decision.NextState, StateWaitingLLM)
	}
	if len(decision.Mutations) != 1 {
		t.Fatalf("mutations = %+v, want one", decision.Mutations)
	}
	if _, ok := decision.Mutations[0].(AppendUserMessage); !ok {
		t.Fatalf("mutation = %T, want AppendUserMessage", decision.Mutations[0])
	}
	if len(decision.Effects) != 1 {
		t.Fatalf("effects = %+v, want one", decision.Effects)
	}
	if _, ok := decision.Effects[0].(CallModel); !ok {
		t.Fatalf("effect = %T, want CallModel", decision.Effects[0])
	}
}

func TestApprovalGrantedRunsPendingLocalTool(t *testing.T) {
	call := ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
	decision, err := Transition(StateWaitingApproval, ApprovalGranted{Call: call})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if decision.NextState != StateRunningTool {
		t.Fatalf("next state = %s, want %s", decision.NextState, StateRunningTool)
	}
	if len(decision.Mutations) != 1 {
		t.Fatalf("mutations = %+v, want one", decision.Mutations)
	}
	if _, ok := decision.Mutations[0].(ClearPendingTool); !ok {
		t.Fatalf("mutation = %T, want ClearPendingTool", decision.Mutations[0])
	}
	if len(decision.Effects) != 1 {
		t.Fatalf("effects = %+v, want one", decision.Effects)
	}
	effect, ok := decision.Effects[0].(RunTool)
	if !ok {
		t.Fatalf("effect = %T, want RunTool", decision.Effects[0])
	}
	if effect.Call.Name != call.Name {
		t.Fatalf("tool call = %+v, want %+v", effect.Call, call)
	}
}

func TestCancelRequestedReturnsRuntimeToIdle(t *testing.T) {
	decision, err := Transition(StateWaitingLLM, CancelRequested{})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if decision.NextState != StateIdle {
		t.Fatalf("next state = %s, want %s", decision.NextState, StateIdle)
	}
	if len(decision.Effects) != 0 {
		t.Fatalf("effects = %+v, want none", decision.Effects)
	}
}

func TestCancelClearsPendingToolAndEffects(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCall: &ToolCall{Name: "bash", Input: "rm -rf /", Risky: true}},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	engine.Ready()

	if err := engine.SubmitUserMessage(context.Background(), "danger", nil); err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}
	if engine.State() != StateWaitingApproval {
		t.Fatalf("state = %s, want %s", engine.State(), StateWaitingApproval)
	}

	if err := engine.Cancel(); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if _, ok := engine.PendingTool(); ok {
		t.Fatal("pending tool still exists")
	}
}
