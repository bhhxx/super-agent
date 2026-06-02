package tui_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"super-agent/runtime"
	"super-agent/tui"
)

type blockingModel struct {
	release chan struct{}
}

func (m blockingModel) Next(_ context.Context, _ []runtime.Message, _ []runtime.ToolSpec, _ func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	<-m.release
	return runtime.ModelResponse{Content: "done"}, nil
}

type noopTools struct{}

func (noopTools) Run(_ context.Context, _ runtime.ToolCall) (string, error) {
	return "", nil
}

func (noopTools) Specs() []runtime.ToolSpec {
	return nil
}

func TestSubmitShowsWaitingLLMWhileModelCommandRuns(t *testing.T) {
	release := make(chan struct{})
	engine := runtime.NewEngine(blockingModel{release: release}, noopTools{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	for _, r := range "hello" {
		var cmd tea.Cmd
		model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			_ = cmd()
		}
	}

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	done := runCommandAsync(t, cmd)
	waitForState(t, session, runtime.StateWaitingLLM)

	view := model.View()
	if !strings.Contains(view, "Thinking") {
		t.Fatalf("view = %q, want friendly state 'Thinking'", view)
	}
	close(release)
	if msg := <-done; msg == nil {
		t.Fatal("done message is nil")
	}
}

func TestQuestionMarkCanBeTypedInPrompt(t *testing.T) {
	release := make(chan struct{})
	engine := runtime.NewEngine(blockingModel{release: release}, noopTools{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	for _, r := range "what?" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	var cmd tea.Cmd
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	done := runCommandAsync(t, cmd)
	waitForMessages(t, session, 1)

	messages := session.Snapshot().Messages
	if len(messages) != 1 || messages[0].Content != "what?" {
		t.Fatalf("messages = %+v, want one user message with question mark", messages)
	}
	close(release)
	<-done
}

func TestApprovalUsesShortcutKeys(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)

	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "run bash" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}
	done := runSingleCommandAsync(batch[len(batch)-1])
	waitForState(t, session, runtime.StateWaitingApproval)
	model, _ = drainEventsUntil(t, model, batch[0], "ACTION REQUIRED")

	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd != nil {
		t.Fatal("cmd is not nil")
	}
	if msg := <-done; msg == nil {
		t.Fatal("done message is nil")
	}

	if session.Snapshot().PendingTool != nil {
		t.Fatal("pending tool still exists after approval")
	}
}

func TestEscCancelsPendingApproval(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok"}}},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)

	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "run bash" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}
	done := runSingleCommandAsync(batch[len(batch)-1])
	waitForState(t, session, runtime.StateWaitingApproval)
	model, _ = drainEventsUntil(t, model, batch[0], "ACTION REQUIRED")

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("cmd is not nil")
	}
	if msg := <-done; msg == nil {
		t.Fatal("done message is nil")
	}
	if session.Snapshot().State != runtime.StateIdle {
		t.Fatalf("state = %s, want %s", session.Snapshot().State, runtime.StateIdle)
	}
	if session.Snapshot().PendingTool != nil {
		t.Fatal("pending tool still exists")
	}
}

func TestTUIRendersSessionEventsWithoutSnapshotReads(t *testing.T) {
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	for _, r := range "hello" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}
	runCmd := batch[len(batch)-1]
	if done := runCmd(); done == nil {
		t.Fatal("done message is nil")
	}

	session.rejectSnapshots = true
	eventCmd := batch[0]
	for {
		eventMsg := eventCmd()
		if eventMsg == nil {
			break
		}
		var next tea.Cmd
		model, next = model.Update(eventMsg)
		if next == nil {
			break
		}
		eventCmd = next
	}

	view := model.View()
	if !strings.Contains(view, "ASSISTANT") || !strings.Contains(view, "from event") {
		t.Fatalf("view = %q, want assistant message from event", view)
	}
}

func TestMemoryCommandDisplaysLoadedSources(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{}, noopTools{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	var model tea.Model = tui.New(session, tui.TUIInfo{
		Provider:    "test",
		ModelName:   "test-model",
		MemoryPaths: []string{"/repo/AGENTS.md", "/repo/pkg/CLAUDE.md"},
	})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "/memory" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := model.View()
	if !strings.Contains(view, "Loaded memory sources") ||
		!strings.Contains(view, "/repo/AGENTS.md") ||
		!strings.Contains(view, "/repo/pkg/CLAUDE.md") {
		t.Fatalf("view = %q, want memory sources", view)
	}
}

func TestPermissionsModeCommandRejectsInvalidMode(t *testing.T) {
	session := &eventOnlyConversation{permissionErr: errors.New("invalid permission mode: root")}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model", PermissionMode: "ask"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "/permissions mode root" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := model.View()
	if !strings.Contains(view, "Permissions failed: invalid permission mode: root") {
		t.Fatalf("view = %q, want invalid permission mode error", view)
	}
	if strings.Contains(view, "mode:root") {
		t.Fatalf("view = %q, mode should stay ask", view)
	}
	if session.permissionMode != "root" {
		t.Fatalf("permissionMode call = %q, want root", session.permissionMode)
	}
}

type approvalModel struct {
	responses []runtime.ModelResponse
}

func (m *approvalModel) Next(_ context.Context, _ []runtime.Message, _ []runtime.ToolSpec, _ func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

type recordingTools struct {
	results map[string]string
}

func (t *recordingTools) Run(_ context.Context, call runtime.ToolCall) (string, error) {
	return t.results[call.Name], nil
}

func (t *recordingTools) Specs() []runtime.ToolSpec {
	return []runtime.ToolSpec{{Name: "bash", Risky: true}}
}

type eventOnlyConversation struct {
	rejectSnapshots bool
	permissionMode  runtime.PermissionMode
	permissionErr   error
}

func (c *eventOnlyConversation) Snapshot() runtime.Snapshot {
	if c.rejectSnapshots {
		panic("unexpected Snapshot read")
	}
	return runtime.Snapshot{State: runtime.StateIdle}
}

func (c *eventOnlyConversation) RunTurn(_ context.Context, query string, events chan<- runtime.SessionEvent, _ <-chan runtime.ApprovalDecision) error {
	events <- runtime.StateChanged{State: runtime.StateWaitingLLM}
	events <- runtime.MessageAppended{Message: runtime.Message{Role: runtime.RoleUser, Content: query}}
	events <- runtime.MessageAppended{Message: runtime.Message{Role: runtime.RoleAssistant, Content: "from event"}}
	events <- runtime.StateChanged{State: runtime.StateIdle}
	close(events)
	return nil
}

func (c *eventOnlyConversation) Cancel() error {
	return nil
}

func (c *eventOnlyConversation) Reset() error {
	return nil
}

func (c *eventOnlyConversation) Sessions() ([]runtime.SessionSummary, error) {
	return nil, nil
}

func (c *eventOnlyConversation) Resume(runtime.SessionID) error {
	return nil
}

func (c *eventOnlyConversation) Rename(runtime.SessionID, string) error {
	return nil
}

func (c *eventOnlyConversation) DeleteSession(runtime.SessionID) error {
	return nil
}

func (c *eventOnlyConversation) Compact(context.Context, string, int) error {
	return nil
}

func (c *eventOnlyConversation) Undo() error {
	return nil
}

func (c *eventOnlyConversation) SetPermissionMode(mode runtime.PermissionMode) error {
	c.permissionMode = mode
	return c.permissionErr
}

func runCommandAsync(t *testing.T, cmd tea.Cmd) <-chan tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		done <- msg
		return done
	}
	if len(batch) == 0 {
		t.Fatal("empty batch")
	}
	runCmd := batch[len(batch)-1]
	if runCmd == nil {
		t.Fatal("run command is nil")
	}
	go func() {
		done <- runCmd()
	}()
	return done
}

func runSingleCommandAsync(cmd tea.Cmd) <-chan tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()
	return done
}

func drainEventsUntil(t *testing.T, model tea.Model, eventCmd tea.Cmd, want string) (tea.Model, tea.Cmd) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		msg := eventCmd()
		if msg == nil {
			break
		}
		var next tea.Cmd
		model, next = model.Update(msg)
		if strings.Contains(model.View(), want) {
			return model, next
		}
		if next == nil {
			break
		}
		eventCmd = next
	}
	t.Fatalf("view = %q, want %q", model.View(), want)
	return model, nil
}

func waitForState(t *testing.T, session *runtime.Session, state runtime.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if session.Snapshot().State == state {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state = %s, want %s", session.Snapshot().State, state)
}

func waitForMessages(t *testing.T, session *runtime.Session, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(session.Snapshot().Messages) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("messages = %+v, want at least %d", session.Snapshot().Messages, count)
}
