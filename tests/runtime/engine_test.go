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

func runSession(t *testing.T, engine *Engine, content string) {
	t.Helper()
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ConfirmationAction, 1)
	if err := session.Run(context.Background(), content, events, approvals); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	for range events {
	}
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

func TestSessionRunProducesFinalAnswer(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{FinalAnswer: "hello", ReasoningContent: "thinking"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	engine.Ready()

	runSession(t, engine, "hi")

	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if got := engine.Messages()[1]; got.Role != RoleAssistant || got.Content != "hello" || got.ReasoningContent != "thinking" {
		t.Fatalf("assistant message = %+v", got)
	}
}

type recordingExecutor struct {
	effects []Effect
}

func (x *recordingExecutor) Execute(_ context.Context, effect Effect, _ EffectContext, _ func(StreamChunk)) (Event, error) {
	x.effects = append(x.effects, effect)
	return AssistantMessageReceived{Response: ModelResponse{FinalAnswer: "from executor"}}, nil
}

func TestEngineRunsEffectsThroughInjectedExecutor(t *testing.T) {
	executor := &recordingExecutor{}
	engine := NewEngineWithExecutor(executor, nil)
	engine.Ready()

	runSession(t, engine, "hi")

	if len(executor.effects) != 1 {
		t.Fatalf("effects = %+v, want one", executor.effects)
	}
	if _, ok := executor.effects[0].(CallModel); !ok {
		t.Fatalf("effect = %T, want CallModel", executor.effects[0])
	}
	if got := engine.Messages()[1]; got.Content != "from executor" {
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

	runSession(t, engine, "use bash")

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

func TestMultipleToolCallsFeedAllResultsBackToModel(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{
			{ID: "call-1", Name: "first"},
			{ID: "call-2", Name: "second"},
		}},
		{FinalAnswer: "done"},
	}}
	tools := &fakeTool{results: map[string]string{"first": "one", "second": "two"}}
	engine := NewEngine(model, tools, nil)
	engine.Ready()

	runSession(t, engine, "use tools")

	if len(tools.calls) != 2 {
		t.Fatalf("tool calls = %+v, want two", tools.calls)
	}
	if tools.calls[0].Name != "first" || tools.calls[1].Name != "second" {
		t.Fatalf("tool calls = %+v, want first then second", tools.calls)
	}
	messages := engine.Messages()
	if len(messages) < 4 {
		t.Fatalf("messages = %+v, want assistant plus two tool results", messages)
	}
	if got := messages[1]; got.Role != RoleAssistant || len(got.ToolCalls) != 2 {
		t.Fatalf("assistant tool calls = %+v", got)
	}
	if messages[2].Role != RoleTool || messages[2].Content != "one" || messages[2].ToolCallID != "call-1" {
		t.Fatalf("first tool result = %+v", messages[2])
	}
	if messages[3].Role != RoleTool || messages[3].Content != "two" || messages[3].ToolCallID != "call-2" {
		t.Fatalf("second tool result = %+v", messages[3])
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

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ConfirmationAction, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.Run(context.Background(), "danger", events, approvals)
	}()
	waitForApproval(t, events, approvals, func() {
		if engine.State() != StateWaitingApproval {
			t.Fatalf("state = %s, want %s", engine.State(), StateWaitingApproval)
		}
		if _, ok := engine.PendingTool(); !ok {
			t.Fatal("pending tool not found")
		}
		if len(tools.calls) != 0 {
			t.Fatalf("tool ran before approval: %+v", tools.calls)
		}
	})
	if engine.State() != StateWaitingApproval {
		t.Fatalf("state = %s, want %s", engine.State(), StateWaitingApproval)
	}
	if _, ok := engine.PendingTool(); !ok {
		t.Fatal("pending tool not found")
	}
	if len(tools.calls) != 0 {
		t.Fatalf("tool ran before approval: %+v", tools.calls)
	}

	approvals <- ConfirmOnce
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
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

	session := NewSession(engine)
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan SessionEvent, 20)
	approvals := make(chan ConfirmationAction, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.Run(ctx, "danger", events, approvals)
	}()
	waitForApproval(t, events, approvals, nil)
	if engine.State() != StateWaitingApproval {
		t.Fatalf("state = %s, want %s", engine.State(), StateWaitingApproval)
	}

	if err := session.Cancel(); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	cancel()
	for range events {
	}
	if err := <-done; err == nil {
		t.Fatal("Run succeeded after cancellation")
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if _, ok := engine.PendingTool(); ok {
		t.Fatal("pending tool still exists")
	}
}

func waitForApproval(t *testing.T, events <-chan SessionEvent, approvals chan<- ConfirmationAction, check func()) {
	t.Helper()
	for ev := range events {
		if ev.State == StateWaitingApproval && ev.ToolCall != nil {
			if check != nil {
				check()
			}
			return
		}
	}
	t.Fatal("approval event not emitted")
}

func TestSessionRunStartsAndFinishesRun(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{FinalAnswer: "hello"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	engine.Ready()
	session := NewSession(engine)
	events := make(chan SessionEvent, 10)
	approvals := make(chan ConfirmationAction, 1)

	if err := session.Run(context.Background(), "hi", events, approvals); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	for range events {
	}
	if session.Snapshot().State != StateIdle {
		t.Fatalf("state = %s, want %s", session.Snapshot().State, StateIdle)
	}
}

func TestSessionRunEmitsStateAndFinalMessage(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{FinalAnswer: "hello", ReasoningContent: "thinking"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	engine.Ready()
	session := NewSession(engine)
	events := make(chan SessionEvent, 10)
	approvals := make(chan ConfirmationAction, 1)

	if err := session.Run(context.Background(), "hi", events, approvals); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var states []State
	var final *Message
	for ev := range events {
		if ev.State != "" {
			states = append(states, ev.State)
		}
		if ev.Message != nil && ev.Message.Role == RoleAssistant {
			final = ev.Message
		}
	}

	if len(states) < 2 || states[0] != StateWaitingLLM || states[len(states)-1] != StateIdle {
		t.Fatalf("states = %+v, want WaitingLLM ... Idle", states)
	}
	if final == nil || final.Content != "hello" || final.ReasoningContent != "thinking" {
		t.Fatalf("final = %+v", final)
	}
}

func TestSessionRunWaitsForApprovalChannel(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCall: &ToolCall{Name: "bash", Input: "printf ok", Risky: true}},
		{FinalAnswer: "done"},
	}}
	tools := &fakeTool{results: map[string]string{"bash": "ok"}}
	engine := NewEngine(model, tools, nil)
	engine.Ready()
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ConfirmationAction, 1)

	done := make(chan error, 1)
	go func() {
		done <- session.Run(context.Background(), "run bash", events, approvals)
	}()

	var sawApproval bool
	for ev := range events {
		if ev.State == StateWaitingApproval && ev.ToolCall != nil {
			sawApproval = true
			approvals <- ConfirmOnce
		}
	}

	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !sawApproval {
		t.Fatal("approval event not emitted")
	}
	if len(tools.calls) != 1 {
		t.Fatalf("tool calls = %+v, want one", tools.calls)
	}
}
