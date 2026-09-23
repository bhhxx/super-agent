package tui_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"super-agent/app"
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

func TestSubmitShowsBusyPresentationWhileModelCommandStarts(t *testing.T) {
	release := make(chan struct{})
	engine := runtime.NewEngine(blockingModel{release: release}, noopTools{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{ModelName: "test-model"})
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
	if !strings.Contains(view, "Thinking...") {
		t.Fatalf("view = %q, want compact thinking presentation", view)
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
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{ModelName: "test-model"})
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

func TestTabCompletesUniqueSlashCommand(t *testing.T) {
	model := newEventOnlyTUI(t)
	for _, r := range "/ins" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !strings.Contains(model.View(), "Instructions") {
		t.Fatalf("view = %q, want completed /instructions command", model.View())
	}
}

func TestWorkflowCommandsShowDiffAndBranchStatus(t *testing.T) {
	model := newEventOnlyTUI(t)
	model = typeText(model, "/diff")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !strings.Contains(model.View(), "Patch preview") {
		t.Fatalf("diff view = %q", model.View())
	}
	model = typeText(model, "/branch")
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !strings.Contains(model.View(), "Branch status") {
		t.Fatalf("branch view = %q", model.View())
	}
}

func TestCustomSlashCommandExpandsArguments(t *testing.T) {
	session := &notificationOnlyConversation{customCommands: map[string]string{"audit": "Audit $ARGUMENTS"}}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model = typeText(model, "/audit auth")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("custom command did not start a turn")
	}
	msg := <-runCommandAsync(t, cmd)
	if msg == nil {
		t.Fatal("custom command returned no message")
	}
	if len(session.queries) != 1 || session.queries[0] != "Audit auth" {
		t.Fatalf("queries = %+v", session.queries)
	}
}

func TestSlashPaletteSelectsCommandWithArrowsAndEnter(t *testing.T) {
	model := newEventOnlyTUI(t)
	model = typeText(model, "/")
	if view := model.View(); !strings.Contains(view, "› /clear") || !strings.Contains(view, "/compact") || !strings.Contains(view, "Reset the conversation") {
		t.Fatalf("view = %q, want described slash command palette", view)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := model.View(); !strings.Contains(view, "› /compact") {
		t.Fatalf("view = %q, want /compact selected and completed", view)
	}
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := model.View(); !strings.Contains(view, "Compacting conversation") {
		t.Fatalf("view = %q, want compact started", view)
	}
	if cmd == nil {
		t.Fatal("compact cmd is nil")
	}
	done := runSingleCommandAsync(cmd)
	msg := <-done
	if msg == nil {
		t.Fatal("compact msg is nil")
	}
	model, _ = model.Update(msg)
	if view := model.View(); !strings.Contains(view, "Compacted conversation") {
		t.Fatalf("view = %q, want selected command executed", view)
	}
}

func TestSmallWindowKeepsSelectedSlashCommandVisible(t *testing.T) {
	model := newEventOnlyTUI(t)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	model = typeText(model, "/")
	for range 4 {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if view := model.View(); !strings.Contains(view, "› /instructions") || strings.Contains(view, "Show loaded instruction files") {
		t.Fatalf("view = %q, want selected command visible in compact palette", view)
	}
}

func TestEscapeClearsInputWithoutQuitting(t *testing.T) {
	model := newEventOnlyTUI(t)
	for _, r := range "draft" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("escape should not quit")
	}
	if view := model.View(); !strings.Contains(view, "Input cleared") {
		t.Fatalf("view = %q, want input-cleared status", view)
	}
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("cleared input should not submit")
	}
}

func TestTabQueuesPromptWhileTurnRuns(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = typeText(model, "first")
	model, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	firstBatch := firstCmd().(tea.BatchMsg)

	model = typeText(model, "second")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Fatal("queue should not start a concurrent turn")
	}
	if view := model.View(); !strings.Contains(view, "Message queued") || !strings.Contains(view, "Queued (1)") || !strings.Contains(view, "1. second") {
		t.Fatalf("view = %q, want queued status, count, and preview", view)
	}

	done := firstBatch[len(firstBatch)-1]()
	model, secondCmd := model.Update(done)
	if secondCmd == nil {
		t.Fatal("queued turn command is nil")
	}
	secondBatch := secondCmd().(tea.BatchMsg)
	_ = secondBatch[len(secondBatch)-1]()
	if got := strings.Join(session.queries, ","); got != "first,second" {
		t.Fatalf("queries = %q, want first,second", got)
	}
	if len(session.contextCanceled) == 0 || session.contextCanceled[0] {
		t.Fatalf("contextCanceled = %#v, queued follow-up must not cancel current turn", session.contextCanceled)
	}
}

func TestQueuePreviewIsBounded(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = typeText(model, "active")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	for _, prompt := range []string{"one", "two", "three", "four"} {
		model = typeText(model, prompt)
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	view := model.View()
	for _, want := range []string{"Queued (4)", "1. one", "2. two", "3. three", "… 1 more"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view = %q, want %q", view, want)
		}
	}
	if strings.Contains(view, "4. four") {
		t.Fatalf("view = %q, queue preview should show at most three items", view)
	}
}

func TestSmallWindowCollapsesQueueDetails(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	model = typeText(model, "active")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, prompt := range []string{"one", "two", "three", "four"} {
		model = typeText(model, prompt)
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	view := model.View()
	if !strings.Contains(view, "Queued (4)") || !strings.Contains(view, "… 4 more") {
		t.Fatalf("view = %q, want compact queue summary", view)
	}
	if strings.Contains(view, "1. one") {
		t.Fatalf("view = %q, compact queue should hide item details", view)
	}
}

func TestNarrowWindowClampsEveryRenderedLine(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", CWD: strings.Repeat("/segment", 30)})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 30, Height: 12})
	assertLinesFitWidth(t, model.View(), 30)
}

func TestInfoBarKeepsModeWhenWorkingDirectoryIsLong(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", CWD: strings.Repeat("/segment", 30)})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	view := model.View()
	if !strings.Contains(view, "ask · test-model · tools on") {
		t.Fatalf("view = %q, want permission mode kept after dropping the working directory", view)
	}
	assertLinesFitWidth(t, view, 40)
}

func TestOverlongErrorIsClampedNotWrapped(t *testing.T) {
	session := &notificationOnlyConversation{permissionErr: errors.New(strings.Repeat("boom", 40))}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", PermissionMode: "ask"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	model = typeText(model, "/permissions mode root")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := model.View()
	if !strings.Contains(view, "!! error:") {
		t.Fatalf("view = %q, want permission error", view)
	}
	assertLinesFitWidth(t, view, 40)
}

func TestNarrowWindowKeepsComposerVisible(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", CWD: strings.Repeat("/segment", 30)})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	view := model.View()
	if !strings.Contains(view, "Ask me anything") {
		t.Fatalf("view = %q, want composer visible", view)
	}
	assertLinesFitWidth(t, view, 30)
}

func TestQueuedPreviewIsClampedToTerminalWidth(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 24, Height: 12})
	model = typeText(model, "active")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = typeText(model, strings.Repeat("queued prompt ", 6))
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	assertLinesFitWidth(t, model.View(), 24)
}

func TestHelpOverlayIsClampedToTerminalWidth(t *testing.T) {
	model := newEventOnlyTUI(t)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assertLinesFitWidth(t, model.View(), 20)
}

func TestResizeRecomputesClampedBudget(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", CWD: strings.Repeat("/segment", 30)})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 24, Height: 14})
	assertLinesFitWidth(t, model.View(), 24)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	assertLinesFitWidth(t, model.View(), 60)
}

func TestWelcomeIsPartOfManagedTranscript(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", CWD: "/repo", InstructionPaths: []string{"/repo/AGENTS.md"}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := model.View(); !strings.Contains(got, "Super Agent") || !strings.Contains(got, "test-model · /repo · AGENTS.md") {
		t.Fatalf("view = %q, want compact welcome block", got)
	}
}

func TestStatusLineKeepsModelAndMode(t *testing.T) {
	session := &notificationOnlyConversation{}
	var narrow tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	narrow, _ = narrow.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	narrowView := narrow.View()
	if !strings.Contains(narrowView, "ask · test-model · tools on") {
		t.Fatalf("view = %q, want model and mode", narrowView)
	}
	assertLinesFitWidth(t, narrowView, 50)
}

func TestEscCancelsTurnAndClearsQueuedFollowUps(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = typeText(model, "active")
	model, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	firstBatch := firstCmd().(tea.BatchMsg)

	model = typeText(model, "follow-up")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if view := model.View(); !strings.Contains(view, "Turn canceled") || strings.Contains(view, "Queued (1)") {
		t.Fatalf("view = %q, want canceled status and cleared queue", view)
	}

	done := firstBatch[len(firstBatch)-1]()
	model, nextCmd := model.Update(done)
	if nextCmd != nil {
		t.Fatal("manual cancellation must not run queued follow-up")
	}
	if got := strings.Join(session.queries, ","); got != "active" {
		t.Fatalf("queries = %q, want only active turn", got)
	}
}

func TestEnterSteersByCancelingCurrentTurnAndRunningPromptNext(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = typeText(model, "first")
	model, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	firstBatch := firstCmd().(tea.BatchMsg)
	model = typeText(model, "steer")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("steering should wait for canceled turn to finish")
	}
	if view := model.View(); !strings.Contains(view, "Steering current turn") {
		t.Fatalf("view = %q, want steering status", view)
	}

	done := firstBatch[len(firstBatch)-1]()
	model, secondCmd := model.Update(done)
	secondBatch := secondCmd().(tea.BatchMsg)
	_ = secondBatch[len(secondBatch)-1]()
	if got := strings.Join(session.queries, ","); got != "first,steer" {
		t.Fatalf("queries = %q, want first,steer", got)
	}
	if len(session.contextCanceled) == 0 || !session.contextCanceled[0] {
		t.Fatalf("contextCanceled = %#v, want canceled first turn", session.contextCanceled)
	}
}

func TestCtrlJInsertsNewlineAndEnterSubmits(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = typeText(model, "first line")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	model = typeText(model, "second line")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("multiline submit command is nil")
	}
	batch := cmd().(tea.BatchMsg)
	_ = batch[len(batch)-1]()
	if len(session.queries) != 1 || session.queries[0] != "first line\nsecond line" {
		t.Fatalf("queries = %#v, want one multiline prompt", session.queries)
	}
}

func TestHistoryNavigationRestoresUnsubmittedDraft(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = typeText(model, "previous")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch := cmd().(tea.BatchMsg)
	done := batch[len(batch)-1]()
	model, _ = model.Update(done)

	model = typeText(model, "draft")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if view := model.View(); !strings.Contains(view, "❯ previous") {
		t.Fatalf("view = %q, want previous prompt", view)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if view := model.View(); !strings.Contains(view, "❯ draft") {
		t.Fatalf("view = %q, want restored draft", view)
	}
}

func typeText(model tea.Model, text string) tea.Model {
	for _, r := range text {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return model
}

// submitAndDrain submits text and delivers every notification the turn
// produces, so the transcript reaches the state a finished turn leaves it in.
func submitAndDrain(t *testing.T, model tea.Model, text string) tea.Model {
	t.Helper()
	model = typeText(model, text)
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}
	if done := batch[len(batch)-1](); done == nil {
		t.Fatal("done message is nil")
	}
	for eventCmd := batch[0]; eventCmd != nil; {
		eventMsg := eventCmd()
		if eventMsg == nil {
			break
		}
		model, eventCmd = model.Update(eventMsg)
	}
	return model
}

func assertLinesFitWidth(t *testing.T, view string, width int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width %d exceeds terminal %d: %q", i, got, width, line)
		}
	}
}

func newEventOnlyTUI(t *testing.T) tea.Model {
	t.Helper()
	var model tea.Model = tui.New(&notificationOnlyConversation{}, tui.StartupInfo{ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return model
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

	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{ModelName: "test-model"}, discardOutputOption)
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

func TestToolRunClearsApprovalPresentation(t *testing.T) {
	tools := &blockingTools{started: make(chan struct{}), release: make(chan struct{})}
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)

	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{ModelName: "test-model"}, discardOutputOption)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	model = typeText(model, "run bash")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd is nil")
	}
	batch := cmd().(tea.BatchMsg)
	done := runSingleCommandAsync(batch[len(batch)-1])
	waitForState(t, session, runtime.StateWaitingApproval)
	model, eventCmd := drainEventsUntil(t, model, batch[0], "ACTION REQUIRED")

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	<-tools.started
	for range 2 {
		var next tea.Cmd
		model, next = model.Update(eventCmd())
		eventCmd = next
	}
	if view := model.View(); strings.Contains(view, "ACTION REQUIRED") {
		t.Fatalf("view = %q, approval presentation was not cleared", view)
	}
	if view := model.View(); !strings.Contains(view, "Thinking...") {
		t.Fatalf("view = %q, want compact busy presentation while tool runs", view)
	}

	close(tools.release)
	if msg := <-done; msg == nil {
		t.Fatal("done message is nil")
	}
	// Drain the remaining turn events.
	for {
		msg := eventCmd()
		if msg == nil {
			break
		}
		var next tea.Cmd
		model, next = model.Update(msg)
		if next == nil {
			break
		}
		eventCmd = next
	}
}

func TestApprovalMenuUsesArrowsAndEnter(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}, &recordingTools{results: map[string]string{"bash": "ok"}}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{ModelName: "test-model"}, discardOutputOption)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = typeText(model, "run bash")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch := cmd().(tea.BatchMsg)
	done := runSingleCommandAsync(batch[len(batch)-1])
	waitForState(t, session, runtime.StateWaitingApproval)
	model, _ = drainEventsUntil(t, model, batch[0], "ACTION REQUIRED")

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if view := model.View(); !strings.Contains(view, "› 3. No, deny") {
		t.Fatalf("view = %q, want deny selected", view)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := model.View(); !strings.Contains(view, "Decision submitted") {
		t.Fatalf("view = %q, want submitted state", view)
	}
	if msg := <-done; msg == nil {
		t.Fatal("done message is nil")
	}
	if session.Snapshot().PendingTool != nil {
		t.Fatal("pending tool still exists after denial")
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

	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{ModelName: "test-model"}, discardOutputOption)
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

func TestTUIRendersSessionNotificationsWithoutSnapshotReads(t *testing.T) {
	session := &notificationOnlyConversation{}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
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

	if view := model.View(); !strings.Contains(view, "from notification") {
		t.Fatalf("view = %q, want assistant in managed transcript", view)
	}
}

func TestContentThatLeavesTheWindowIsCommittedToTerminalScrollback(t *testing.T) {
	session := &notificationOnlyConversation{extraMessages: 20}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = submitAndDrain(t, model, "hi")

	scrollback := strings.Join(printed.items, "\n")
	if !strings.Contains(scrollback, "❯ hi") || !strings.Contains(scrollback, "from notification") {
		t.Fatalf("scrollback = %q, want the messages the window pushed out", scrollback)
	}
	view := model.View()
	if strings.Contains(view, "from notification") {
		t.Fatalf("view = %q, committed messages must leave the live window", view)
	}
	if !strings.Contains(view, "message message") {
		t.Fatalf("view = %q, want the newest messages still live", view)
	}
}

// The commit is what makes the terminal scroll: a frame shorter than the
// terminal absorbs the print and the committed text never reaches scrollback.
func TestLiveViewFillsTheTerminalSoCommitsScroll(t *testing.T) {
	session := &notificationOnlyConversation{extraMessages: 4}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = submitAndDrain(t, model, "hi")

	if rows := len(strings.Split(model.View(), "\n")); rows != 24 {
		t.Fatalf("view rows = %d, want the whole 24-row terminal", rows)
	}
}

func TestCommittedContentIsNeverCommittedTwice(t *testing.T) {
	session := &notificationOnlyConversation{extraMessages: 20}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = submitAndDrain(t, model, "hi")
	committed := strings.Join(printed.items, "\n")

	for _, message := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyPgUp},
		tea.KeyMsg{Type: tea.KeyCtrlO},
		tea.KeyMsg{Type: tea.KeyCtrlT},
		tea.WindowSizeMsg{Width: 80, Height: 24},
	} {
		model, _ = model.Update(message)
	}
	if got := strings.Join(printed.items, "\n"); got != committed {
		t.Fatalf("scrollback changed to %q, want %q", got, committed)
	}
}

func TestOverflowCommitsWithinTheTerminalWidth(t *testing.T) {
	session := &notificationOnlyConversation{extraMessages: 20}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 40, Height: 24})

	model = submitAndDrain(t, model, "hi")

	if len(printed.items) == 0 {
		t.Fatal("nothing was committed")
	}
	for _, item := range printed.items {
		assertLinesFitWidth(t, item, 40)
	}
}

func TestDefaultPrinterCommitsMessagesToTerminalOutput(t *testing.T) {
	output := &lockedBuffer{}
	program := tea.NewProgram(
		tui.New(&notificationOnlyConversation{}, tui.StartupInfo{ModelName: "test-model"}),
		tea.WithInput(nil),
		tea.WithOutput(output),
		tea.WithoutSignalHandler(),
	)
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()
	program.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(output.String(), "from notification") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	program.Quit()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "❯ hi") || !strings.Contains(got, "from notification") {
		t.Fatalf("terminal output = %q, want submitted user and assistant messages", got)
	}
}

func TestToolCallsAreSummarizedAndExpandOnDemand(t *testing.T) {
	session := &toolNotificationConversation{}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = typeText(model, "inspect")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch := cmd().(tea.BatchMsg)
	_ = batch[len(batch)-1]()
	for eventCmd := batch[0]; eventCmd != nil; {
		msg := eventCmd()
		if msg == nil {
			break
		}
		model, eventCmd = model.Update(msg)
	}

	view := model.View()
	if !strings.Contains(view, "● Read 2 files") || !strings.Contains(view, "● Edited tui/view.go") {
		t.Fatalf("view = %q, want compact tool summaries", view)
	}
	if strings.Contains(view, "private reasoning") || strings.Contains(view, `{"path":`) {
		t.Fatalf("view = %q, reasoning and raw tool inputs must stay hidden", view)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	latest := model.View()
	if !strings.Contains(latest, "tui/view.go") || strings.Contains(latest, "tui/app.go") {
		t.Fatalf("transcript = %q, want only latest tool group expanded", latest)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}, Alt: true})
	all := model.View()
	if !strings.Contains(all, "tui/app.go") || !strings.Contains(all, "tui/view.go") {
		t.Fatalf("transcript = %q, want all tool groups expanded", all)
	}
	if read, path, edit := strings.Index(all, "● Read"), strings.Index(all, "tui/app.go"), strings.Index(all, "● Edited"); !(read < path && path < edit) {
		t.Fatalf("transcript = %q, expanded details must remain below their tool call", all)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	latestThinking := model.View()
	if !strings.Contains(latestThinking, "patch reasoning") || strings.Contains(latestThinking, "private reasoning") {
		t.Fatalf("view = %q, want only latest reasoning expanded", latestThinking)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}, Alt: true})
	allThinking := model.View()
	if !strings.Contains(allThinking, "patch reasoning") || !strings.Contains(allThinking, "private reasoning") {
		t.Fatalf("view = %q, want all reasoning expanded", allThinking)
	}
}

func TestPageKeysDoNotReplaceTerminalScrollback(t *testing.T) {
	session := &notificationOnlyConversation{extraMessages: 30}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, discardOutputOption)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 15})
	model = typeText(model, "hello")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch := cmd().(tea.BatchMsg)
	_ = batch[len(batch)-1]()
	eventCmd := batch[0]
	for eventCmd != nil {
		msg := eventCmd()
		if msg == nil {
			break
		}
		model, eventCmd = model.Update(msg)
	}
	before := model.View()
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if view := model.View(); view != before {
		t.Fatalf("view changed on page key: before=%q after=%q", before, view)
	}
}

func TestInstructionsCommandDisplaysLoadedSources(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{}, noopTools{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	printed := &recordedOutput{}
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.StartupInfo{
		ModelName:        "test-model",
		InstructionPaths: []string{"/repo/AGENTS.md", "/repo/pkg/CLAUDE.md"},
	}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "/instructions" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := strings.Join(printed.items, "\n")
	if !strings.Contains(output, "Loaded instruction sources") ||
		!strings.Contains(output, "/repo/AGENTS.md") ||
		!strings.Contains(output, "/repo/pkg/CLAUDE.md") {
		t.Fatalf("output = %q, want instruction sources", output)
	}
}

func TestPermissionsModeCommandRejectsInvalidMode(t *testing.T) {
	session := &notificationOnlyConversation{permissionErr: errors.New("invalid permission mode: root")}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", PermissionMode: "ask"})
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

func TestPermissionsModeKeepsDisplayedModel(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model", PermissionMode: "ask"}, discardOutputOption)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = typeText(model, "/permissions mode plan")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if view := model.View(); !strings.Contains(view, "plan · test-model · tools on") {
		t.Fatalf("view = %q, want the displayed model preserved across a permission change", view)
	}
}

func TestAttachCommandRoutesThroughTheAttachmentsFeature(t *testing.T) {
	session := &notificationOnlyConversation{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, discardOutputOption)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = typeText(model, "/attach notes.md")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("attach command did not start")
	}

	model, _ = model.Update(cmd())
	if view := model.View(); !strings.Contains(view, "Attachments: notes.md") || !strings.Contains(view, "Attached notes.md") {
		t.Fatalf("view = %q, want the attachment queued and listed", view)
	}
}

func TestMCPCommandsListAndAddServer(t *testing.T) {
	session := &notificationOnlyConversation{mcpServers: []tui.MCPServerSummary{{Name: "files", Tools: []string{"read_remote"}}}}
	printed := &recordedOutput{}
	var model tea.Model = tui.New(session, tui.StartupInfo{ModelName: "test-model"}, printed.option())
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "/mcp list" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if output := strings.Join(printed.items, "\n"); !strings.Contains(output, "files  read_remote") {
		t.Fatalf("output = %q, want MCP server list", output)
	}
	for _, r := range "/mcp add local helper --stdio" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	var command tea.Cmd
	model, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("add command did not run asynchronously")
	}
	model, _ = model.Update(command())
	if session.mcpAddedName != "local" || session.mcpAddedCommand != "helper" || len(session.mcpAddedArgs) != 1 || session.mcpAddedArgs[0] != "--stdio" {
		t.Fatalf("add = %q %q %+v", session.mcpAddedName, session.mcpAddedCommand, session.mcpAddedArgs)
	}
	if view := model.View(); !strings.Contains(view, "Added MCP server local") {
		t.Fatalf("view = %q, want add status", view)
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

type blockingTools struct {
	started chan struct{}
	release chan struct{}
}

func (t *blockingTools) Run(_ context.Context, _ runtime.ToolCall) (string, error) {
	close(t.started)
	<-t.release
	return "ok", nil
}

func (t *blockingTools) Specs() []runtime.ToolSpec {
	return []runtime.ToolSpec{{Name: "bash", Risky: true}}
}

type recordedOutput struct {
	items []string
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (r *recordedOutput) option() tui.Option {
	return tui.WithOutputPrinter(func(content string) tea.Cmd {
		r.items = append(r.items, content)
		return nil
	})
}

var discardOutputOption = tui.WithOutputPrinter(func(string) tea.Cmd { return nil })

type notificationOnlyConversation struct {
	rejectSnapshots bool
	permissionMode  string
	permissionErr   error
	queries         []string
	contextCanceled []bool
	extraMessages   int
	mcpServers      []tui.MCPServerSummary
	mcpAddedName    string
	mcpAddedCommand string
	mcpAddedArgs    []string
	customCommands  map[string]string
}

type toolNotificationConversation struct{ notificationOnlyConversation }

func (c *toolNotificationConversation) RunTurn(_ context.Context, query string, notifications chan<- tui.ConversationNotification, _ <-chan tui.ApprovalDecision) error {
	notifications <- tui.MessageAppended{Message: tui.Message{Role: "user", Content: query}}
	notifications <- tui.MessageAppended{Message: tui.Message{
		Role:             tui.RoleAssistant,
		Content:          "Answer",
		ReasoningContent: "private reasoning",
		ToolCalls: []*tui.ToolCall{
			{Name: "read_file", Input: `{"path":"tui/app.go"}`},
			{Name: "read_file", Input: `{"path":"tui/update.go"}`},
		},
	}}
	notifications <- tui.MessageAppended{Message: tui.Message{Role: tui.RoleAssistant, ReasoningContent: "patch reasoning", ToolCalls: []*tui.ToolCall{{Name: "apply_patch", Input: `{"path":"tui/view.go"}`}}}}
	close(notifications)
	return nil
}

func (c *notificationOnlyConversation) ListAgents() []tui.AgentSummary { return nil }
func (c *notificationOnlyConversation) CurrentAgent() tui.AgentSummary { return tui.AgentSummary{} }
func (c *notificationOnlyConversation) UseAgent(string) error          { return nil }
func (c *notificationOnlyConversation) Fork(string) (string, error)    { return "fork", nil }
func (c *notificationOnlyConversation) Memories() ([]string, error)    { return nil, nil }
func (c *notificationOnlyConversation) Remember(string) error          { return nil }
func (c *notificationOnlyConversation) ForgetMemories() error          { return nil }
func (c *notificationOnlyConversation) GitDiff(context.Context) (string, error) {
	return "diff", nil
}
func (c *notificationOnlyConversation) GitStatus(context.Context) (string, error) {
	return "status", nil
}
func (c *notificationOnlyConversation) CustomCommands() []string {
	var names []string
	for name := range c.customCommands {
		names = append(names, name)
	}
	return names
}
func (c *notificationOnlyConversation) ExpandCustomCommand(name, arguments string) (string, error) {
	return strings.ReplaceAll(c.customCommands[name], "$ARGUMENTS", arguments), nil
}
func (c *notificationOnlyConversation) Export(string) (string, error) { return "/tmp/export", nil }
func (c *notificationOnlyConversation) Attach(path string) (tui.AttachmentSummary, error) {
	return tui.AttachmentSummary{Name: path, MIME: "text/plain"}, nil
}
func (c *notificationOnlyConversation) PendingAttachments() []tui.AttachmentSummary { return nil }
func (c *notificationOnlyConversation) Skills() []string                            { return nil }
func (c *notificationOnlyConversation) Plugins() []string                           { return nil }
func (c *notificationOnlyConversation) Diagnostics(context.Context, string) (string, error) {
	return "[]", nil
}

func (c *notificationOnlyConversation) Snapshot() tui.ConversationView {
	if c.rejectSnapshots {
		panic("unexpected Snapshot read")
	}
	return tui.ConversationView{AgentStatus: tui.AgentStatus{Label: "Idle"}}
}

func (c *notificationOnlyConversation) RunTurn(ctx context.Context, query string, notifications chan<- tui.ConversationNotification, _ <-chan tui.ApprovalDecision) error {
	c.queries = append(c.queries, query)
	c.contextCanceled = append(c.contextCanceled, ctx.Err() != nil)
	notifications <- tui.AgentStatusChanged{Status: tui.AgentStatus{Label: "WaitingLLM", Busy: true}}
	notifications <- tui.MessageAppended{Message: tui.Message{Role: "user", Content: query}}
	notifications <- tui.MessageAppended{Message: tui.Message{Role: tui.RoleAssistant, Content: "from notification"}}
	for index := 0; index < c.extraMessages; index++ {
		notifications <- tui.MessageAppended{Message: tui.Message{Role: tui.RoleAssistant, Content: strings.Repeat("message ", 12)}}
	}
	notifications <- tui.AgentStatusChanged{Status: tui.AgentStatus{Label: "Idle"}}
	close(notifications)
	return nil
}

func (c *notificationOnlyConversation) Cancel() error {
	return nil
}

func (c *notificationOnlyConversation) Reset() error {
	return nil
}

func (c *notificationOnlyConversation) ListSessions() ([]tui.SessionSummary, error) {
	return nil, nil
}

func (c *notificationOnlyConversation) Resume(string) error {
	return nil
}

func (c *notificationOnlyConversation) RenameSession(string, string) error {
	return nil
}

func (c *notificationOnlyConversation) DeleteSession(string) error {
	return nil
}

func (c *notificationOnlyConversation) Compact(context.Context, string) error {
	return nil
}

func (c *notificationOnlyConversation) Undo() error {
	return nil
}

func (c *notificationOnlyConversation) SetPermissionMode(mode string) error {
	c.permissionMode = mode
	return c.permissionErr
}

func (c *notificationOnlyConversation) PermissionMode() string {
	return c.permissionMode
}

func (c *notificationOnlyConversation) AutoApproveTools() bool {
	return c.permissionMode == "bypass"
}

func (c *notificationOnlyConversation) ListMCPServers() []tui.MCPServerSummary {
	return c.mcpServers
}
func (c *notificationOnlyConversation) AddMCPServer(_ context.Context, name, command string, args []string) error {
	c.mcpAddedName = name
	c.mcpAddedCommand = command
	c.mcpAddedArgs = append([]string(nil), args...)
	return nil
}
func (c *notificationOnlyConversation) RemoveMCPServer(string) error { return nil }
func (c *notificationOnlyConversation) RestartMCPServer(context.Context, string) error {
	return nil
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
