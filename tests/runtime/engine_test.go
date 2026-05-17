package runtime_test

import (
	"context"
	"sync"
	"testing"
	"time"

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
	specs   []ToolSpec
	calls   []ToolCall
}

func (t *fakeTool) Run(_ context.Context, call ToolCall) (string, error) {
	t.calls = append(t.calls, call)
	return t.results[call.Name], nil
}

func (t *fakeTool) Specs() []ToolSpec {
	return t.specs
}

func runSession(t *testing.T, engine *Engine, content string) {
	t.Helper()
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	if err := session.RunTurn(context.Background(), content, events, approvals); err != nil {
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
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
}

func TestReadyReturnsErrorWhenAlreadyReady(t *testing.T) {
	engine := NewEngine(&scriptedModel{}, &fakeTool{}, nil)

	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := engine.Ready(); err == nil {
		t.Fatal("Ready succeeded after engine was already ready")
	}
}

func TestSessionRunProducesContent(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Content: "hello", ReasoningContent: "thinking"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

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

func (x *recordingExecutor) Execute(_ context.Context, effect Effect, _ ExecutionInput, _ func(StreamChunk)) (ExecutionResult, error) {
	x.effects = append(x.effects, effect)
	return ModelReplied{Response: ModelResponse{Content: "from executor"}}, nil
}

type recordingPolicy struct {
	decision ToolDecision
	calls    []ToolCall
	specs    [][]ToolSpec
}

func (p *recordingPolicy) ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision {
	p.calls = append(p.calls, call)
	p.specs = append(p.specs, append([]ToolSpec(nil), input.ToolSpecs...))
	return p.decision
}

type blockingApprovalStore struct {
	blockAllow chan struct{}
	allowing   chan struct{}
	allowed    map[ApprovalKey]bool
}

func newBlockingApprovalStore() *blockingApprovalStore {
	return &blockingApprovalStore{
		blockAllow: make(chan struct{}),
		allowing:   make(chan struct{}),
		allowed:    make(map[ApprovalKey]bool),
	}
}

func (s *blockingApprovalStore) AllowAlways(key ApprovalKey) {
	close(s.allowing)
	<-s.blockAllow
	s.allowed[key] = true
}

func (s *blockingApprovalStore) IsAlwaysAllowed(key ApprovalKey) bool {
	return s.allowed[key]
}

func (s *blockingApprovalStore) SetAutoApproveTools(bool) {}

func (s *blockingApprovalStore) AutoApproveTools() bool {
	return false
}

func TestEngineRunsEffectsThroughInjectedExecutor(t *testing.T) {
	executor := &recordingExecutor{}
	engine := NewEngineWithExecutor(executor, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

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

func TestCustomPolicyClassifiesToolCallWithContext(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	policy := &recordingPolicy{decision: DecisionRunDirectly}
	engine := NewEngineWithExecutorAndPolicy(NewDefaultEffectExecutor(model, tools), policy, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	runSession(t, engine, "use bash")

	if len(policy.calls) != 1 || policy.calls[0].Name != "bash" {
		t.Fatalf("policy calls = %+v, want bash", policy.calls)
	}
	if len(policy.specs) != 1 || len(policy.specs[0]) != 1 || policy.specs[0][0].Name != "bash" || !policy.specs[0][0].Risky {
		t.Fatalf("policy specs = %+v, want risky bash spec", policy.specs)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("tool calls = %+v, want one", tools.calls)
	}
}

func TestAutoApproveBypassesCustomPolicyDecision(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	policy := &recordingPolicy{decision: DecisionNeedsApproval}
	engine := NewEngineWithExecutorAndPolicy(NewDefaultEffectExecutor(model, tools), policy, nil)
	engine.EnableAutoApproveTools()
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	runSession(t, engine, "use bash")

	if len(tools.calls) != 1 {
		t.Fatalf("tool calls = %+v, want one", tools.calls)
	}
}

func TestWaitingApprovalKeepsRunContext(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{specs: []ToolSpec{{Name: "bash", Risky: true}}}
	runs := NewDefaultRunController()
	approvals := NewMemoryApprovalStore()
	engine := NewEngineWithComponents(
		NewDefaultEffectRunner(NewDefaultEffectExecutor(model, tools)),
		DefaultResultResolver{},
		NewDefaultEventClassifier(NewDefaultPolicy(), approvals),
		DefaultReducer{},
		runs,
		approvals,
		nil,
	)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvalDecisions := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "danger", events, approvalDecisions)
	}()
	waitForApproval(t, events, approvalDecisions, nil)

	if _, ok := runs.CurrentContext(); !ok {
		t.Fatal("run context missing while waiting approval")
	}

	approvalDecisions <- DenyApproval
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}

func TestFinalAssistantResponseFinishesRun(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Content: "done"}}}
	runs := NewDefaultRunController()
	approvals := NewMemoryApprovalStore()
	engine := NewEngineWithComponents(
		NewDefaultEffectRunner(NewDefaultEffectExecutor(model, &fakeTool{})),
		DefaultResultResolver{},
		NewDefaultEventClassifier(NewDefaultPolicy(), approvals),
		DefaultReducer{},
		runs,
		approvals,
		nil,
	)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	runSession(t, engine, "hi")

	if _, ok := runs.CurrentContext(); ok {
		t.Fatal("run context still exists after final assistant response")
	}
}

func TestCancelAndResetClearRunContext(t *testing.T) {
	runs := NewDefaultRunController()
	_, ctx := runs.StartRun(context.Background())
	if _, ok := runs.CurrentContext(); !ok {
		t.Fatal("run context missing after start")
	}
	if ctx == nil {
		t.Fatal("started run returned nil context")
	}

	runs.CancelRun()
	if _, ok := runs.CurrentContext(); ok {
		t.Fatal("run context still exists after cancel")
	}

	runs.StartRun(context.Background())
	runs.StartNewGeneration()
	if _, ok := runs.CurrentContext(); ok {
		t.Fatal("run context still exists after reset generation")
	}
}

func TestApproveAlwaysWritesStoreWithoutHoldingEngineLock(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	store := newBlockingApprovalStore()
	engine := NewEngineWithComponents(
		NewDefaultEffectRunner(NewDefaultEffectExecutor(model, tools)),
		DefaultResultResolver{},
		NewDefaultEventClassifier(NewDefaultPolicy(), store),
		DefaultReducer{},
		NewDefaultRunController(),
		store,
		nil,
	)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "danger", events, approvals)
	}()
	waitForApproval(t, events, approvals, nil)

	approvals <- ApproveAlways
	<-store.allowing

	stateRead := make(chan State, 1)
	go func() {
		stateRead <- engine.State()
	}()
	select {
	case state := <-stateRead:
		if state == StateWaitingApproval {
			close(store.blockAllow)
			t.Fatal("approve-always persisted approval before dispatching approval transition")
		}
	case <-time.After(200 * time.Millisecond):
		close(store.blockAllow)
		t.Fatal("engine lock held while writing approval store")
	}

	close(store.blockAllow)
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}

func TestApproveAlwaysWritesApprovalStoreNotPolicy(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	policy := &recordingPolicy{decision: DecisionNeedsApproval}
	engine := NewEngineWithExecutorAndPolicy(NewDefaultEffectExecutor(model, tools), policy, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "use bash", events, approvals)
	}()

	waitForApproval(t, events, approvals, nil)
	approvals <- ApproveAlways
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("tool calls = %+v, want repeated approved tool to run", tools.calls)
	}
}

func TestToolCallFeedsResultBackToModel(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf pong"}}},
		{Content: "tool said pong"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "pong"},
		specs:   []ToolSpec{{Name: "bash"}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

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
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"first": "one", "second": "two"},
		specs:   []ToolSpec{{Name: "first"}, {Name: "second"}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	runSession(t, engine, "use tools")

	if len(tools.calls) != 2 {
		t.Fatalf("tool calls = %+v, want two", tools.calls)
	}
	if tools.calls[0].Name != "first" || tools.calls[1].Name != "second" {
		t.Fatalf("tool calls = %+v", tools.calls)
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

func TestQueuedRiskyToolWaitsForApproval(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{
			{ID: "call-1", Name: "first"},
			{ID: "call-2", Name: "second"},
		}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"first": "one", "second": "two"},
		specs:   []ToolSpec{{Name: "first"}, {Name: "second", Risky: true}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "use tools", events, approvals)
	}()

	waitForApproval(t, events, approvals, func() {
		if len(tools.calls) != 1 || tools.calls[0].Name != "first" {
			t.Fatalf("tool calls before approval = %+v, want first only", tools.calls)
		}
		call, ok := engine.PendingTool()
		if !ok {
			t.Fatal("pending tool not found")
		}
		if call.Name != "second" {
			t.Fatalf("pending tool = %+v, want second", call)
		}
	})

	approvals <- ApproveOnce
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(tools.calls) != 2 || tools.calls[1].Name != "second" {
		t.Fatalf("tool calls after approval = %+v, want first then second", tools.calls)
	}
}

func TestToolCallAssistantContentIsPreserved(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{
			Content:   "I will inspect the files.",
			ToolCalls: []ToolCall{{ID: "call-1", Name: "bash", Input: "ls"}},
		},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash"}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	runSession(t, engine, "use bash")

	messages := engine.Messages()
	if got := messages[1]; got.Role != RoleAssistant || got.Content != "I will inspect the files." {
		t.Fatalf("assistant tool-call message = %+v", got)
	}
}

func TestRiskyToolWaitsForShortcutApproval(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "rm -rf /"}}},
		{Content: "approved"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "danger", events, approvals)
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

	approvals <- ApproveOnce
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
}

func TestToolRiskComesFromToolSpec(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "approved"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "danger", events, approvals)
	}()

	waitForApproval(t, events, approvals, func() {
		if len(tools.calls) != 0 {
			t.Fatalf("tool ran before approval: %+v", tools.calls)
		}
	})
	approvals <- ApproveOnce
	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
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
func TestToolResultAdvancesQueueThroughEngine(t *testing.T) {
	call := ToolCall{ID: "call-1", Name: "bash", Input: "pwd"}
	decision, err := Transition(StateRunningTool, ToolResultReceived{Call: call, Result: "ok"})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if decision.NextState != StateAdvancingQueue {
		t.Fatalf("next state = %s, want %s", decision.NextState, StateAdvancingQueue)
	}
	if len(decision.Effects) != 1 {
		t.Fatalf("effects = %+v, want one", decision.Effects)
	}
	if _, ok := decision.Effects[0].(ProcessNextToolCall); !ok {
		t.Fatalf("effect = %T, want ProcessNextToolCall", decision.Effects[0])
	}
}

func TestDenialAdvancesQueueThroughEngine(t *testing.T) {
	call := ToolCall{ID: "call-1", Name: "bash", Input: "rm -rf /"}
	decision, err := Transition(StateWaitingApproval, ApprovalDenied{Call: call})
	if err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if decision.NextState != StateAdvancingQueue {
		t.Fatalf("next state = %s, want %s", decision.NextState, StateAdvancingQueue)
	}
	if len(decision.Effects) != 1 {
		t.Fatalf("effects = %+v, want one", decision.Effects)
	}
	if _, ok := decision.Effects[0].(ProcessNextToolCall); !ok {
		t.Fatalf("effect = %T, want ProcessNextToolCall", decision.Effects[0])
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
		{ToolCalls: []ToolCall{{Name: "bash", Input: "rm -rf /"}}},
	}}
	engine := NewEngine(model, &fakeTool{specs: []ToolSpec{{Name: "bash", Risky: true}}}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(ctx, "danger", events, approvals)
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

func TestApprovalWaitContextCancelCancelsEngine(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "rm -rf /"}}},
	}}
	engine := NewEngine(model, &fakeTool{specs: []ToolSpec{{Name: "bash", Risky: true}}}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}

	session := NewSession(engine)
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(ctx, "danger", events, approvals)
	}()
	waitForApproval(t, events, approvals, nil)

	cancel()
	err := <-done
	if err == nil {
		t.Fatal("RunTurn succeeded after approval context cancellation")
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if _, ok := engine.PendingTool(); ok {
		t.Fatal("pending tool still exists")
	}
}

func waitForApproval(t *testing.T, events <-chan SessionEvent, approvals chan<- ApprovalDecision, check func()) {
	t.Helper()
	for ev := range events {
		if _, ok := ev.(ToolApprovalRequested); ok {
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
		{Content: "hello"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 10)
	approvals := make(chan ApprovalDecision, 1)

	if err := session.RunTurn(context.Background(), "hi", events, approvals); err != nil {
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
		{Content: "hello", ReasoningContent: "thinking"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 10)
	approvals := make(chan ApprovalDecision, 1)

	if err := session.RunTurn(context.Background(), "hi", events, approvals); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var states []State
	var final *Message
	for ev := range events {
		switch ev := ev.(type) {
		case StateChanged:
			states = append(states, ev.State)
		case MessageAppended:
			if ev.Message.Role == RoleAssistant {
				final = &ev.Message
			}
		}
	}

	if len(states) < 2 || states[0] != StateWaitingLLM || states[len(states)-1] != StateIdle {
		t.Fatalf("states = %+v, want WaitingLLM ... Idle", states)
	}
	if final == nil || final.Content != "hello" || final.ReasoningContent != "thinking" {
		t.Fatalf("final = %+v", final)
	}
}

func TestSessionRunEmitsEachAppendedMessageOnce(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Content: "hello"},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 10)
	approvals := make(chan ApprovalDecision, 1)

	if err := session.RunTurn(context.Background(), "hi", events, approvals); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var appended []Message
	for ev := range events {
		if ev, ok := ev.(MessageAppended); ok {
			appended = append(appended, ev.Message)
		}
	}

	if len(appended) != 2 {
		t.Fatalf("appended = %+v, want user and assistant only once", appended)
	}
	if appended[0].Role != RoleUser || appended[0].Content != "hi" {
		t.Fatalf("first appended = %+v", appended[0])
	}
	if appended[1].Role != RoleAssistant || appended[1].Content != "hello" {
		t.Fatalf("second appended = %+v", appended[1])
	}
}

func TestSessionRunWaitsForApprovalChannel(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)

	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "run bash", events, approvals)
	}()

	var sawApproval bool
	for ev := range events {
		if _, ok := ev.(ToolApprovalRequested); ok {
			sawApproval = true
			approvals <- ApproveOnce
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

func TestSessionRunReturnsErrorWhenApprovalChannelCloses(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision)

	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "run bash", events, approvals)
	}()

	waitForApproval(t, events, approvals, nil)
	close(approvals)
	err := <-done
	if err == nil || err.Error() != "approval channel closed" {
		t.Fatalf("Run error = %v, want approval channel closed", err)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if _, ok := engine.PendingTool(); ok {
		t.Fatal("pending tool still exists")
	}
	if len(tools.calls) != 0 {
		t.Fatalf("tool calls = %+v, want none", tools.calls)
	}
}

func TestApproveAlwaysIsScopedToToolNameAndInput(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "ls"}}},
		{ToolCalls: []ToolCall{{Name: "bash", Input: "rm -rf /"}}},
		{ToolCalls: []ToolCall{{Name: "bash", Input: "ls"}}},
		{ToolCalls: []ToolCall{{Name: "read", Input: "/etc/hosts"}}},
		{Content: "done"},
	}}
	tools := &fakeTool{
		results: map[string]string{"bash": "ok", "read": "ok"},
		specs:   []ToolSpec{{Name: "bash", Risky: true}, {Name: "read", Risky: true}},
	}
	engine := NewEngine(model, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)

	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "run tools", events, approvals)
	}()

	approvalCount := 0
	for ev := range events {
		if _, ok := ev.(ToolApprovalRequested); ok {
			approvalCount++
			if approvalCount == 1 {
				approvals <- ApproveAlways
			} else {
				approvals <- ApproveOnce
			}
		}
	}

	if err := <-done; err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if approvalCount != 3 {
		t.Fatalf("approval count = %d, want 3 (bash ls, bash rm, read)", approvalCount)
	}
}

type blockingModel struct {
	started chan struct{}
	release chan struct{}
}

func newBlockingModel() *blockingModel {
	return &blockingModel{started: make(chan struct{}), release: make(chan struct{})}
}

func (m *blockingModel) Next(_ context.Context, _ []Message, _ []ToolSpec, _ func(StreamChunk)) (ModelResponse, error) {
	close(m.started)
	<-m.release
	return ModelResponse{Content: "stale"}, nil
}

func TestCancelDropsStaleModelResult(t *testing.T) {
	model := newBlockingModel()
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)

	go func() {
		done <- session.RunTurn(context.Background(), "hi", events, approvals)
	}()
	<-model.started
	if err := session.Cancel(); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}
	close(model.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunTurn did not return")
	}
	for range events {
	}
	if messages := engine.Messages(); len(messages) != 1 {
		t.Fatalf("messages = %+v, want only user message", messages)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
}

func TestResetDropsStaleModelResultAndClearsMessages(t *testing.T) {
	model := newBlockingModel()
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)

	go func() {
		done <- session.RunTurn(context.Background(), "hi", events, approvals)
	}()
	<-model.started
	if err := session.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}
	close(model.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunTurn did not return")
	}
	for range events {
	}
	if messages := engine.Messages(); len(messages) != 0 {
		t.Fatalf("messages = %+v, want reset context to stay empty", messages)
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
}

func TestNoToolsToolCallIsProtocolError(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{Name: "bash", Input: "printf ok"}}},
	}}
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)

	err := session.RunTurn(context.Background(), "use bash", events, approvals)
	if err == nil {
		t.Fatal("RunTurn succeeded after model returned a tool call with no tools")
	}
	if engine.State() != StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), StateIdle)
	}
	if len(approvals) != 0 {
		t.Fatal("approval requested for disabled tools")
	}
}

func TestInvalidSecondTurnDoesNotCancelActiveRun(t *testing.T) {
	model := newBlockingModel()
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	first := NewSession(engine)
	firstEvents := make(chan SessionEvent, 20)
	firstApprovals := make(chan ApprovalDecision, 1)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.RunTurn(context.Background(), "first", firstEvents, firstApprovals)
	}()
	<-model.started

	second := NewSession(engine)
	secondEvents := make(chan SessionEvent, 20)
	secondApprovals := make(chan ApprovalDecision, 1)
	if err := second.RunTurn(context.Background(), "second", secondEvents, secondApprovals); err == nil {
		t.Fatal("second RunTurn succeeded while first run was active")
	}

	close(model.release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first RunTurn failed after invalid second turn: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first RunTurn did not return")
	}
	for range firstEvents {
	}
	messages := engine.Messages()
	if len(messages) != 2 || messages[1].Role != RoleAssistant || messages[1].Content != "stale" {
		t.Fatalf("messages = %+v, want first run assistant result preserved", messages)
	}
}

type blockingSpecsRunner struct {
	specCalls chan struct{}
	block     chan struct{}
	mu        sync.Mutex
	specCount int
	modelRuns int
}

func newBlockingSpecsRunner() *blockingSpecsRunner {
	return &blockingSpecsRunner{
		specCalls: make(chan struct{}, 2),
		block:     make(chan struct{}),
	}
}

func (r *blockingSpecsRunner) Run(_ context.Context, effect QueuedEffect, _ ExecutionInput, _ func(StreamChunk)) (EffectOutcome, error) {
	outcome := EffectOutcome{RunID: effect.RunID, EffectID: effect.EffectID}
	switch eff := effect.Effect.(type) {
	case CallModel:
		r.modelRuns++
		if r.modelRuns == 1 {
			outcome.Result = ModelReplied{Response: ModelResponse{
				ToolCalls: []ToolCall{{ID: "call-1", Name: "bash", Input: "pwd"}},
			}}
		} else {
			outcome.Result = ModelReplied{Response: ModelResponse{Content: "done"}}
		}
	case RunTool:
		outcome.Result = ToolFinished{Call: eff.Call, Result: "ok"}
	case ProcessNextToolCall:
		outcome.Result = ToolQueueChecked{}
	default:
		outcome.Result = ModelReplied{Response: ModelResponse{Content: "done"}}
	}
	return outcome, nil
}

func (r *blockingSpecsRunner) ToolSpecs() []ToolSpec {
	r.mu.Lock()
	r.specCount++
	count := r.specCount
	if count <= 2 {
		r.specCalls <- struct{}{}
	}
	r.mu.Unlock()
	if count == 2 {
		<-r.block
	}
	return []ToolSpec{{Name: "bash"}}
}

func TestClassifierToolSpecsAreFetchedWithoutHoldingEngineLock(t *testing.T) {
	runner := newBlockingSpecsRunner()
	store := NewMemoryApprovalStore()
	engine := NewEngineWithComponents(
		runner,
		DefaultResultResolver{},
		NewDefaultEventClassifier(NewDefaultPolicy(), store),
		DefaultReducer{},
		NewDefaultRunController(),
		store,
		nil,
	)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	approvals := make(chan ApprovalDecision, 1)
	done := make(chan error, 1)
	go func() {
		done <- session.RunTurn(context.Background(), "hi", events, approvals)
	}()
	<-runner.specCalls
	<-runner.specCalls

	stateDone := make(chan State, 1)
	go func() {
		stateDone <- engine.State()
	}()
	select {
	case <-stateDone:
	case <-time.After(200 * time.Millisecond):
		close(runner.block)
		t.Fatal("State blocked while classifier fetched tool specs")
	}
	close(runner.block)
	if err := <-done; err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	for range events {
	}
}

func TestRunControllerCurrentContextReportsMissingContext(t *testing.T) {
	controller := NewDefaultRunController()
	if _, ok := controller.CurrentContext(); ok {
		t.Fatal("CurrentContext returned ok before a run started")
	}

	_, ctx := controller.StartRun(context.Background())
	got, ok := controller.CurrentContext()
	if !ok || got != ctx {
		t.Fatalf("CurrentContext = (%v, %v), want current context", got, ok)
	}

	controller.CancelRun()
	if _, ok := controller.CurrentContext(); ok {
		t.Fatal("CurrentContext returned ok after cancellation")
	}
}
