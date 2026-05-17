package tui_test

import (
	"context"
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
	engine.Ready()
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
	engine.Ready()
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
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok", Risky: true}}},
		{Content: "done"},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	engine.Ready()
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
	done := runCommandAsync(t, cmd)
	waitForState(t, session, runtime.StateWaitingApproval)

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
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok", Risky: true}}},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	engine.Ready()
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
	done := runCommandAsync(t, cmd)
	waitForState(t, session, runtime.StateWaitingApproval)

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
