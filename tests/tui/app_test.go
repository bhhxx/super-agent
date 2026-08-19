package tui_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	if !strings.Contains(view, "Submitting") {
		t.Fatalf("view = %q, want submitting presentation", view)
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
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := model.View(); !strings.Contains(view, "No instruction files loaded") {
		t.Fatalf("view = %q, want completed /instructions command", view)
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
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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

func TestEscCancelsTurnAndClearsQueuedFollowUps(t *testing.T) {
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	session := &eventOnlyConversation{}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	model = typeText(model, "previous")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	batch := cmd().(tea.BatchMsg)
	done := batch[len(batch)-1]()
	model, _ = model.Update(done)

	model = typeText(model, "draft")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if view := model.View(); !strings.Contains(view, "◇ previous") {
		t.Fatalf("view = %q, want previous prompt", view)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if view := model.View(); !strings.Contains(view, "◇ draft") {
		t.Fatalf("view = %q, want restored draft", view)
	}
}

func typeText(model tea.Model, text string) tea.Model {
	for _, r := range text {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return model
}

func newEventOnlyTUI(t *testing.T) tea.Model {
	t.Helper()
	var model tea.Model = tui.New(&eventOnlyConversation{}, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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

	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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

func TestToolRunShowsRunningToolState(t *testing.T) {
	tools := &blockingTools{started: make(chan struct{}), release: make(chan struct{})}
	engine := runtime.NewEngine(&approvalModel{responses: []runtime.ModelResponse{
		{ToolCalls: []runtime.ToolCall{{Name: "bash", Input: "printf ok"}}},
		{Content: "done"},
	}}, tools, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)

	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{Provider: "test", ModelName: "test-model"})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
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
	model, eventCmd = drainEventsUntil(t, model, eventCmd, "RunningTool")

	close(tools.release)
	if msg := <-done; msg == nil {
		t.Fatal("done message is nil")
	}
	// Drain the remaining turn events so the footer state history is final.
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
	if view := model.View(); !strings.Contains(view, "states: WaitingLLM → AdvancingQueue → WaitingApproval → RunningTool → AdvancingQueue") {
		t.Fatalf("view = %q, want state history line with AdvancingQueue", view)
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
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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

	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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

func TestPageKeysScrollConversationAndReturnToBottom(t *testing.T) {
	session := &eventOnlyConversation{extraMessages: 30}
	var model tea.Model = tui.New(session, tui.TUIInfo{Provider: "test", ModelName: "test-model"})
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
	if view := model.View(); !strings.Contains(view, "100%") {
		t.Fatalf("view = %q, want viewport at bottom", view)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if view := model.View(); strings.Contains(view, "100%") {
		t.Fatalf("view = %q, page up should leave bottom", view)
	}
	for range 50 {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if view := model.View(); !strings.Contains(view, "100%") {
		t.Fatalf("view = %q, repeated page down should return to bottom", view)
	}
}

func TestInstructionsCommandDisplaysLoadedSources(t *testing.T) {
	engine := runtime.NewEngine(&approvalModel{}, noopTools{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := runtime.NewSession(engine)
	var model tea.Model = tui.New(app.NewTUIConversation(session), tui.TUIInfo{
		Provider:         "test",
		ModelName:        "test-model",
		InstructionPaths: []string{"/repo/AGENTS.md", "/repo/pkg/CLAUDE.md"},
	})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "/instructions" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := model.View()
	if !strings.Contains(view, "Loaded instruction sources") ||
		!strings.Contains(view, "/repo/AGENTS.md") ||
		!strings.Contains(view, "/repo/pkg/CLAUDE.md") {
		t.Fatalf("view = %q, want instruction sources", view)
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

type eventOnlyConversation struct {
	rejectSnapshots bool
	permissionMode  string
	permissionErr   error
	queries         []string
	contextCanceled []bool
	extraMessages   int
}

func (c *eventOnlyConversation) Snapshot() tui.Snapshot {
	if c.rejectSnapshots {
		panic("unexpected Snapshot read")
	}
	return tui.Snapshot{AgentStatus: tui.AgentStatus{Label: "Idle"}}
}

func (c *eventOnlyConversation) RunTurn(ctx context.Context, query string, events chan<- tui.Event, _ <-chan tui.ApprovalDecision) error {
	c.queries = append(c.queries, query)
	c.contextCanceled = append(c.contextCanceled, ctx.Err() != nil)
	events <- tui.AgentStatusChanged{Status: tui.AgentStatus{Label: "WaitingLLM", Busy: true}}
	events <- tui.MessageAppended{Message: tui.Message{Role: "user", Content: query}}
	events <- tui.MessageAppended{Message: tui.Message{Role: tui.RoleAssistant, Content: "from event"}}
	for index := 0; index < c.extraMessages; index++ {
		events <- tui.MessageAppended{Message: tui.Message{Role: tui.RoleAssistant, Content: strings.Repeat("message ", 12)}}
	}
	events <- tui.AgentStatusChanged{Status: tui.AgentStatus{Label: "Idle"}}
	close(events)
	return nil
}

func (c *eventOnlyConversation) Cancel() error {
	return nil
}

func (c *eventOnlyConversation) Reset() error {
	return nil
}

func (c *eventOnlyConversation) Sessions() ([]tui.SessionSummary, error) {
	return nil, nil
}

func (c *eventOnlyConversation) Resume(string) error {
	return nil
}

func (c *eventOnlyConversation) Rename(string, string) error {
	return nil
}

func (c *eventOnlyConversation) DeleteSession(string) error {
	return nil
}

func (c *eventOnlyConversation) Compact(context.Context, string) error {
	return nil
}

func (c *eventOnlyConversation) Undo() error {
	return nil
}

func (c *eventOnlyConversation) SetPermissionMode(mode string) error {
	c.permissionMode = mode
	return c.permissionErr
}

func (c *eventOnlyConversation) PermissionMode() string {
	return c.permissionMode
}

func (c *eventOnlyConversation) AutoApproveTools() bool {
	return c.permissionMode == "bypass"
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
