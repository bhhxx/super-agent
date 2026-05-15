package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"super-agent/runtime"
	"super-agent/tui"
)

type blockingModel struct {
	release chan struct{}
}

func (m blockingModel) Next(_ context.Context, _ []runtime.Message, _ []runtime.ToolSpec, _ func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	<-m.release
	return runtime.ModelResponse{FinalAnswer: "done"}, nil
}

type noopTools struct{}

func (noopTools) Run(_ context.Context, _ runtime.ToolCall) (string, error) {
	return "", nil
}

func (noopTools) Specs() []runtime.ToolSpec {
	return nil
}

func TestSubmitShowsWaitingLLMWhileModelCommandRuns(t *testing.T) {
	engine := runtime.NewEngine(blockingModel{release: make(chan struct{})}, noopTools{}, nil)
	engine.Ready()
	var model tea.Model = tui.New(engine)
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

	view := model.View()
	if !strings.Contains(view, string(runtime.StateWaitingLLM)) {
		t.Fatalf("view = %q, want state %s", view, runtime.StateWaitingLLM)
	}
}

func TestQuestionMarkCanBeTypedInPrompt(t *testing.T) {
	engine := runtime.NewEngine(blockingModel{release: make(chan struct{})}, noopTools{}, nil)
	engine.Ready()
	var model tea.Model = tui.New(engine)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	for _, r := range "what?" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	var cmd tea.Cmd
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}

	messages := engine.Messages()
	if len(messages) != 1 || messages[0].Content != "what?" {
		t.Fatalf("messages = %+v, want one user message with question mark", messages)
	}
}

func TestApprovalUsesShortcutKeys(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCall: &runtime.ToolCall{Name: "bash", Input: "printf ok", Risky: true}},
		{FinalAnswer: "done"},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	engine.Ready()

	if err := engine.SubmitUserMessage(context.Background(), "run bash", nil); err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}
	model := tea.Model(tui.New(engine))
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}

	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) > 0 && batch[0] != nil {
			batch[0]()
		}
	} else {
		// fallback if not a batch
	}

	if _, ok := engine.PendingTool(); ok {
		t.Fatal("pending tool still exists after approval")
	}
}

func TestEscCancelsPendingApproval(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCall: &runtime.ToolCall{Name: "bash", Input: "printf ok", Risky: true}},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	engine.Ready()

	if err := engine.SubmitUserMessage(context.Background(), "run bash", nil); err != nil {
		t.Fatalf("SubmitUserMessage failed: %v", err)
	}
	model := tea.Model(tui.New(engine))
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("cmd is not nil")
	}
	if engine.State() != runtime.StateIdle {
		t.Fatalf("state = %s, want %s", engine.State(), runtime.StateIdle)
	}
	if _, ok := engine.PendingTool(); ok {
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
