package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	. "super-agent/runtime"
	"super-agent/store"
	"super-agent/workspace"
)

func persistentSession(engine *Engine, st *store.Store, meta store.Metadata) *Session {
	repository := store.NewRepository(st)
	return NewPersistentSession(engine, repository, workspace.Workspace{}, SessionMetadata{
		ID: SessionID(meta.ID), Title: meta.Title, Provider: meta.Provider, Model: meta.Model,
		CWD: meta.CWD, InstructionSources: meta.InstructionSources,
	})
}

func TestPersistentSessionResumesConversationWithToolResults(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "rules"}}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleUser, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "call-1", Name: "read_file"}
	if err := st.Append(meta.ID, store.Record{Type: store.EventToolResult, ToolCall: &call, Result: "file contents"}); err != nil {
		t.Fatal(err)
	}

	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, store.Metadata{ID: "new"})
	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatal(err)
	}

	messages := session.Snapshot().Messages
	if len(messages) != 3 {
		t.Fatalf("messages = %+v, want 3", messages)
	}
	if messages[2].Role != RoleTool || messages[2].Content != "file contents" || messages[2].ToolCallID != "call-1" {
		t.Fatalf("tool message = %+v", messages[2])
	}
}

func TestPersistentResetPreservesSystemMessages(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "rules"}}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, initial)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, initial)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := persistentSession(engine, st, meta)
	events := make(chan SessionEvent, 10)
	if err := session.RunTurn(context.Background(), "hi", events, make(chan ApprovalDecision)); err != nil {
		t.Fatal(err)
	}
	if err := session.Reset(); err != nil {
		t.Fatal(err)
	}

	messages, err := st.Messages(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != RoleSystem {
		t.Fatalf("messages = %+v, want only system", messages)
	}
}

func TestCompactKeepsSystemInstructionsAndNewestContext(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "rules"}}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, initial)
	if err != nil {
		t.Fatal(err)
	}
	messages := append(initial,
		Message{Role: RoleUser, Content: "one"},
		Message{Role: RoleAssistant, Content: "two"},
		Message{Role: RoleUser, Content: "three"},
		Message{Role: RoleAssistant, Content: "four"},
	)
	engine := NewEngineWithExecutor(&staticExecutor{}, messages)
	engine.ReplaceMessages(messages)
	session := persistentSession(engine, st, meta)
	if err := session.Compact(context.Background(), "", 2); err != nil {
		t.Fatal(err)
	}

	got := session.Snapshot().Messages
	if len(got) != 4 {
		t.Fatalf("messages = %+v, want 4", got)
	}
	if got[0].Role != RoleSystem || got[0].Content != "rules" {
		t.Fatalf("first message = %+v", got[0])
	}
	if got[1].Role != RoleSystem || got[1].Content != "Conversation summary:\nmodel summary" {
		t.Fatalf("summary message = %+v", got[1])
	}
	if got[2].Content != "three" || got[3].Content != "four" {
		t.Fatalf("newest context = %+v", got[2:])
	}
}

func TestUndoRestoresWriteFileCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.txt")
	if err := os.WriteFile(path, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	st := store.New(t.TempDir())
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cp := store.Checkpoint{
		ID: "cp1",
		Files: []store.FileSnapshot{{
			Path: path, Exists: true, Content: "before", Mode: 0644,
		}},
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventCheckpoint, Checkpoint: &cp}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0644); err != nil {
		t.Fatal(err)
	}

	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, meta)
	if err := session.Undo(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "before" {
		t.Fatalf("content = %q, want before", content)
	}
}

func TestSessionCheckpointUsesWorkspaceAndRepositoryPorts(t *testing.T) {
	st := store.New(t.TempDir())
	meta, err := st.Create(store.Metadata{Provider: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	ws := &checkpointWorkspace{files: []FileSnapshot{{Path: "/work/file", Exists: true, Content: "before", Mode: 0644}}}
	session := NewPersistentSession(engine, store.NewRepository(st), ws, SessionMetadata{ID: SessionID(meta.ID)})
	call := ToolCall{Name: "write_file", Input: `{"path":"file"}`}
	if err := session.Checkpoint(call); err != nil {
		t.Fatal(err)
	}
	if len(ws.paths) != 1 || ws.paths[0] != "file" {
		t.Fatalf("captured paths = %v", ws.paths)
	}
	records, err := st.Records(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.Type != store.EventCheckpoint || last.Checkpoint == nil || last.Checkpoint.Files[0].Content != "before" {
		t.Fatalf("checkpoint record = %+v", last)
	}
}

type checkpointWorkspace struct {
	paths []string
	files []FileSnapshot
}

func (w *checkpointWorkspace) Capture(paths []string) ([]FileSnapshot, error) {
	w.paths = append([]string(nil), paths...)
	return w.files, nil
}

func (*checkpointWorkspace) Restore([]FileSnapshot) error { return nil }

type staticExecutor struct{}

func (x *staticExecutor) Execute(context.Context, Effect, ExecutionInput, func(StreamChunk)) (ExecutionResult, error) {
	return ModelReplied{Response: ModelResponse{Content: "model summary"}}, nil
}

func (x *staticExecutor) ToolSpecs() []ToolSpec {
	return nil
}

type noToolRunner struct{}

func (noToolRunner) Run(_ context.Context, _ ToolCall) (string, error) {
	return "", nil
}

func (noToolRunner) Specs() []ToolSpec {
	return nil
}

func TestCompactDoesNotDuplicateTranscriptOnResume(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "rules"}}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, initial)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, initial)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := persistentSession(engine, st, meta)

	for _, query := range []string{"one", "two", "three"} {
		if err := session.RunTurn(context.Background(), query, make(chan SessionEvent, 20), make(chan ApprovalDecision)); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Compact(context.Background(), "summary", 2); err != nil {
		t.Fatal(err)
	}
	if err := session.RunTurn(context.Background(), "four", make(chan SessionEvent, 20), make(chan ApprovalDecision)); err != nil {
		t.Fatal(err)
	}

	messages, err := st.Messages(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	var rules, summaries, userFours int
	for _, message := range messages {
		switch {
		case message.Role == RoleSystem && message.Content == "rules":
			rules++
		case message.Content == "Conversation summary:\nsummary":
			summaries++
		case message.Role == RoleUser && message.Content == "four":
			userFours++
		}
	}
	// Kept messages are persisted through the compaction record; re-emitting
	// them as appended messages would duplicate the transcript on replay.
	if rules != 1 {
		t.Fatalf("messages = %+v, want exactly one copy of the system message", messages)
	}
	if summaries != 1 {
		t.Fatalf("messages = %+v, want exactly one summary message", messages)
	}
	if userFours != 1 {
		t.Fatalf("messages = %+v, want the post-compact turn appended once", messages)
	}
}

func TestUndoTruncatesTranscriptAfterCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.txt")
	if err := os.WriteFile(path, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	st := store.New(t.TempDir())
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleUser, Content: "write file"}}); err != nil {
		t.Fatal(err)
	}
	cp := store.Checkpoint{
		ID: "cp1",
		Files: []store.FileSnapshot{{
			Path: path, Exists: true, Content: "before", Mode: 0644,
		}},
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventCheckpoint, Checkpoint: &cp}); err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "call-1", Name: "write_file"}
	if err := st.Append(meta.ID, store.Record{Type: store.EventToolResult, ToolCall: &call, Result: "wrote"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleAssistant, Content: "done"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0644); err != nil {
		t.Fatal(err)
	}

	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, meta)
	if err := session.Undo(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "before" {
		t.Fatalf("content = %q, want before", content)
	}
	messages := session.Snapshot().Messages
	if len(messages) != 1 || messages[0].Role != RoleUser || messages[0].Content != "write file" {
		t.Fatalf("messages = %+v, want transcript truncated to the checkpoint", messages)
	}
	replayed, err := st.Messages(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0].Content != "write file" {
		t.Fatalf("replayed = %+v, want resume to match the undone transcript", replayed)
	}
}

func TestConcurrentRunTurnFailsWithoutBlockingEvents(t *testing.T) {
	model := newBlockingModel()
	engine := NewEngine(model, &fakeTool{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events1 := make(chan SessionEvent, 20)
	done1 := make(chan error, 1)
	go func() {
		done1 <- session.RunTurn(context.Background(), "one", events1, make(chan ApprovalDecision))
	}()
	<-model.started

	events2 := make(chan SessionEvent, 20)
	err := session.RunTurn(context.Background(), "two", events2, make(chan ApprovalDecision))
	if err == nil || err.Error() != "session is already running a turn" {
		t.Fatalf("second RunTurn error = %v, want busy error", err)
	}
	for range events2 {
		// must terminate: the busy path closes the events channel
	}

	close(model.release)
	if err := <-done1; err != nil {
		t.Fatalf("first RunTurn failed: %v", err)
	}
}

func TestStoreSerializesConcurrentAppends(t *testing.T) {
	st := store.New(t.TempDir())
	meta, err := st.Create(store.Metadata{Provider: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCurrentTurn(meta.ID, store.TurnID("turn-1")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleUser, Content: "x"}})
		}()
	}
	wg.Wait()

	records, err := st.Records(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	appended := 0
	for _, record := range records {
		if record.Type != store.EventMessageAppended {
			continue
		}
		appended++
		if record.TurnID != store.TurnID("turn-1") {
			t.Fatalf("record %d turn = %q, want turn-1 (torn meta.json interleave)", appended, record.TurnID)
		}
	}
	if appended != 20 {
		t.Fatalf("appended = %d, want 20", appended)
	}
}

func TestResolverErrorAppendsSingleRuntimeErrorMessage(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "bash", Input: "pwd"}}},
	}}
	engine := NewEngine(model, noToolRunner{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionEvent, 20)
	err := session.RunTurn(context.Background(), "use tool", events, make(chan ApprovalDecision))
	if err == nil {
		t.Fatal("RunTurn error = nil, want tools-disabled error")
	}

	count := 0
	for _, message := range session.Snapshot().Messages {
		if message.ToolName == "runtime_error" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("runtime_error messages = %d, want exactly 1", count)
	}
}
