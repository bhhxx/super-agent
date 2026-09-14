package runtime_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	. "super-agent/runtime"
	"super-agent/store"
	"super-agent/workspace"
)

func persistentSession(engine *Engine, st *store.Store, meta store.Metadata) *Session {
	repository := store.NewRepository(st)
	root := meta.CWD
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			panic(err)
		}
	}
	context, err := workspace.NewDefaultContext(root)
	if err != nil {
		panic(err)
	}
	return NewPersistentSession(engine, repository, workspace.New(context), SessionMetadata{
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

func TestResumeMigratesLegacyWorkspaceToCanonicalSpec(t *testing.T) {
	st := store.New(t.TempDir())
	legacyCWD := t.TempDir()
	meta, err := st.Create(store.Metadata{Title: "legacy", Provider: "test", Model: "test-model", CWD: legacyCWD, InstructionSources: []string{"AGENTS.md"}}, []Message{{Role: RoleSystem, Content: "rules"}})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, store.Metadata{ID: "new"})
	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatal(err)
	}

	canonical, err := filepath.EvalSymlinks(legacyCWD)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := st.Metadata(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Workspace == nil {
		t.Fatal("legacy session was not upgraded to a WorkspaceSpec")
	}
	if persisted.Workspace.PrimaryRoot != canonical || persisted.Workspace.CWD != canonical || persisted.CWD != canonical {
		t.Fatalf("migrated workspace = %+v cwd = %q, want %q", persisted.Workspace, persisted.CWD, canonical)
	}
	if len(persisted.Workspace.Roots) != 1 || persisted.Workspace.Roots[0].Path != canonical || persisted.Workspace.Roots[0].Access != "read_write" {
		t.Fatalf("migrated roots = %+v", persisted.Workspace.Roots)
	}
	// Migration touches only the workspace description: unrelated metadata,
	// including ProjectID and ConfigRoot, keeps its saved value.
	if persisted.Title != "legacy" || persisted.Provider != "test" || persisted.Model != "test-model" || !persisted.CreatedAt.Equal(meta.CreatedAt) {
		t.Fatalf("migration changed unrelated metadata: %+v", persisted)
	}
	if len(persisted.InstructionSources) != 1 || persisted.InstructionSources[0] != "AGENTS.md" {
		t.Fatalf("migration changed instruction sources: %+v", persisted.InstructionSources)
	}
	if persisted.ProjectID != "" || persisted.ConfigRoot != "" {
		t.Fatalf("migration derived ProjectID/ConfigRoot from the workspace: %+v", persisted)
	}
}

func TestResumeLegacyWorkspaceMigrationIsIdempotent(t *testing.T) {
	st := store.New(t.TempDir())
	legacyCWD := t.TempDir()
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: legacyCWD}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, store.Metadata{ID: "new"})
	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatal(err)
	}
	first, err := st.Metadata(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Workspace == nil {
		t.Fatal("legacy session was not migrated")
	}

	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatalf("second resume failed on migrated metadata: %v", err)
	}
	second, err := st.Metadata(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Workspace, second.Workspace) {
		t.Fatalf("migration is not idempotent: %+v then %+v", first.Workspace, second.Workspace)
	}
}

func TestResumeUsesStrictValidationAfterLegacyMigration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	parent := t.TempDir()
	savedRoot := filepath.Join(parent, "project")
	if err := os.Mkdir(savedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	st := store.New(t.TempDir())
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: savedRoot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, store.Metadata{ID: "new"})
	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatal(err)
	}

	// The one-time upgrade persisted the canonical root, so replacing it with
	// an escaping symlink must now fail the strict check instead of silently
	// adopting the new target.
	if err := os.Remove(savedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), savedRoot); err != nil {
		t.Fatal(err)
	}
	err = session.Resume(SessionID(meta.ID))
	if err == nil || !strings.Contains(err.Error(), "saved workspace is no longer valid") {
		t.Fatalf("resume error = %v, want strict validation after migration", err)
	}
}

func TestResumeDoesNotFallBackToLegacyWhenSavedSpecIsInvalid(t *testing.T) {
	validCWD := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	st := store.New(t.TempDir())
	spec := WorkspaceSpec{PrimaryRoot: missing, CWD: missing, Roots: []WorkspaceRootSpec{{Path: missing, Access: WorkspaceAccessReadWrite}}}
	meta, err := store.NewRepository(st).Create(SessionMetadata{
		Provider: "test", Model: "test-model", CWD: validCWD, WorkspaceSpec: &spec,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := persistentSession(engine, st, store.Metadata{ID: "new"})
	err = session.Resume(SessionID(meta.ID))
	if err == nil || !strings.Contains(err.Error(), "saved workspace is no longer valid") {
		t.Fatalf("resume error = %v, want invalid saved spec rejection", err)
	}
	persisted, err := st.Metadata(store.SessionID(meta.ID))
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Workspace == nil || persisted.Workspace.PrimaryRoot != missing {
		t.Fatalf("resume mutated the saved spec instead of rejecting it: %+v", persisted.Workspace)
	}
}

func TestResumeFailsWhenWorkspaceMigrationCannotBePersisted(t *testing.T) {
	st := store.New(t.TempDir())
	legacyCWD := t.TempDir()
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: legacyCWD}, nil)
	if err != nil {
		t.Fatal(err)
	}
	repository := failingWorkspaceRepository{store.NewRepository(st)}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := NewPersistentSession(engine, repository, configuredWorkspace(t, t.TempDir()), SessionMetadata{ID: "new"})
	err = session.Resume(SessionID(meta.ID))
	if err == nil || !strings.Contains(err.Error(), "persist migrated workspace") {
		t.Fatalf("resume error = %v, want migration persistence failure", err)
	}
	persisted, err := st.Metadata(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Workspace != nil {
		t.Fatalf("failed migration was persisted: %+v", persisted.Workspace)
	}
}

type failingWorkspaceRepository struct{ *store.Repository }

func (failingWorkspaceRepository) SaveWorkspaceDescription(SessionID, WorkspaceSpec) error {
	return errors.New("workspace metadata write failed")
}

func TestResumeUpgradesLegacyWorkspaceOnlyOnce(t *testing.T) {
	st := store.New(t.TempDir())
	legacyCWD := t.TempDir()
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: legacyCWD}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spy := &canonicalizeSpy{Workspace: configuredWorkspace(t, t.TempDir())}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := NewPersistentSession(engine, store.NewRepository(st), spy, SessionMetadata{ID: "new"})

	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatal(err)
	}
	if spy.calls != 1 {
		t.Fatalf("canonicalize calls = %d, want 1 on the first resume", spy.calls)
	}
	if err := session.Resume(SessionID(meta.ID)); err != nil {
		t.Fatal(err)
	}
	if spy.calls != 1 {
		t.Fatalf("canonicalize calls = %d, want the legacy upgrade to run once", spy.calls)
	}
}

type canonicalizeSpy struct {
	*workspace.Workspace
	calls int
}

func (s *canonicalizeSpy) Canonicalize(spec WorkspaceSpec) (WorkspaceSpec, error) {
	s.calls++
	return s.Workspace.Canonicalize(spec)
}

func TestSessionListPreservesParentRelationship(t *testing.T) {
	st := store.New(t.TempDir())
	repository := store.NewRepository(st)
	created, err := repository.Create(SessionMetadata{ParentID: "parent", Provider: "test", Model: "model"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != created.ID || items[0].ParentID != "parent" {
		t.Fatalf("sessions = %+v", items)
	}
}

func TestSessionForkCopiesTranscriptAndSelectsChild(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "rules"}}
	meta, err := st.Create(store.Metadata{Title: "parent", Provider: "test", Model: "model"}, initial)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, initial)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := persistentSession(engine, st, meta)
	child, err := session.Fork("experiment")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != SessionID(meta.ID) || session.Metadata().ID != child.ID {
		t.Fatalf("child = %+v active = %+v", child, session.Metadata())
	}
	messages, _, err := store.NewRepository(st).Load(child.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "rules" {
		t.Fatalf("fork messages = %+v err=%v", messages, err)
	}
}

func TestCrossSessionMemoryUpdatesTranscript(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "rules"}, {Role: RoleUser, Content: "hello"}}
	meta, err := st.Create(store.Metadata{Title: "memory"}, initial)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, initial)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := persistentSession(engine, st, meta)
	if err := session.Remember("Prefer concise answers"); err != nil {
		t.Fatal(err)
	}
	items, err := session.Memories()
	if err != nil || len(items) != 1 || items[0] != "Prefer concise answers" {
		t.Fatalf("memory = %+v err=%v", items, err)
	}
	messages := session.Snapshot().Messages
	if len(messages) != 3 || !strings.Contains(messages[0].Content, "Prefer concise answers") || messages[2].Content != "hello" {
		t.Fatalf("messages = %+v", messages)
	}
	if err := session.ForgetMemories(); err != nil {
		t.Fatal(err)
	}
	if len(session.Snapshot().Messages) != 2 {
		t.Fatalf("messages after forget = %+v", session.Snapshot().Messages)
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
	events := make(chan SessionNotification, 10)
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

func TestConversationReplacementPersistsExactAgentContext(t *testing.T) {
	st := store.New(t.TempDir())
	initial := []Message{{Role: RoleSystem, Content: "build"}}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, initial)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, initial)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := persistentSession(engine, st, meta)
	replacement := []Message{{Role: RoleSystem, Content: "plan"}}
	if err := session.ReplaceConversation(replacement); err != nil {
		t.Fatal(err)
	}
	messages, err := st.Messages(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "plan" {
		t.Fatalf("messages = %+v, want exact replacement", messages)
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

func TestCompactKeepsAssistantForRetainedToolResults(t *testing.T) {
	st := store.New(t.TempDir())
	call1 := ToolCall{ID: "call-1", Name: "first"}
	call2 := ToolCall{ID: "call-2", Name: "second"}
	messages := []Message{
		{Role: RoleSystem, Content: "rules"},
		{Role: RoleAssistant, ToolCalls: []*ToolCall{&call1, &call2}},
		{Role: RoleTool, ToolCallID: call1.ID, ToolName: call1.Name, Content: "one"},
		{Role: RoleTool, ToolCallID: call2.ID, ToolName: call2.Name, Content: "two"},
		{Role: RoleUser, Content: "next"},
		{Role: RoleAssistant, Content: "done"},
	}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, messages)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, messages)
	engine.ReplaceMessages(messages)
	session := persistentSession(engine, st, meta)
	if err := session.Compact(context.Background(), "summary", 3); err != nil {
		t.Fatal(err)
	}

	got := session.Snapshot().Messages
	if len(got) != 7 {
		t.Fatalf("messages = %+v, want system, summary, and complete retained turn", got)
	}
	if got[2].Role != RoleAssistant || len(got[2].ToolCalls) != 2 {
		t.Fatalf("retained boundary = %+v, want parent assistant tool call", got[2])
	}
	if got[3].ToolCallID != call1.ID || got[4].ToolCallID != call2.ID {
		t.Fatalf("retained tool results = %+v", got[3:5])
	}
}

func TestCompactSkipsSummaryWhenHistoryAlreadyFits(t *testing.T) {
	st := store.New(t.TempDir())
	messages := []Message{
		{Role: RoleSystem, Content: "rules"},
		{Role: RoleUser, Content: "one"},
		{Role: RoleAssistant, Content: "two"},
	}
	meta, err := st.Create(store.Metadata{Provider: "test", Model: "test-model", CWD: t.TempDir()}, messages)
	if err != nil {
		t.Fatal(err)
	}
	executor := &staticExecutor{}
	engine := NewEngineWithExecutor(executor, messages)
	engine.ReplaceMessages(messages)
	session := persistentSession(engine, st, meta)
	if err := session.Compact(context.Background(), "", 4); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("summary calls = %d, want 0", executor.calls)
	}
	if got := session.Snapshot().Messages; len(got) != len(messages) {
		t.Fatalf("messages = %+v, want unchanged history", got)
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

func (*checkpointWorkspace) Spec() WorkspaceSpec {
	return WorkspaceSpec{PrimaryRoot: "/work", CWD: "/work", Roots: []WorkspaceRootSpec{{Path: "/work", Access: WorkspaceAccessReadWrite}}}
}
func (*checkpointWorkspace) Validate(WorkspaceSpec) error { return nil }
func (*checkpointWorkspace) Canonicalize(spec WorkspaceSpec) (WorkspaceSpec, error) {
	return spec, nil
}
func (*checkpointWorkspace) Activate(WorkspaceSpec) error { return nil }

func (w *checkpointWorkspace) Capture(paths []string) ([]FileSnapshot, error) {
	w.paths = append([]string(nil), paths...)
	return w.files, nil
}

func (*checkpointWorkspace) Restore([]FileSnapshot) error { return nil }

type staticExecutor struct {
	calls int
}

func (x *staticExecutor) Execute(context.Context, ScheduledAction, ScheduledActionInput, func(StreamChunk)) (ScheduledActionResult, error) {
	x.calls++
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
		if err := session.RunTurn(context.Background(), query, make(chan SessionNotification, 20), make(chan ApprovalDecision)); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Compact(context.Background(), "summary", 2); err != nil {
		t.Fatal(err)
	}
	if err := session.RunTurn(context.Background(), "four", make(chan SessionNotification, 20), make(chan ApprovalDecision)); err != nil {
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
	events1 := make(chan SessionNotification, 20)
	done1 := make(chan error, 1)
	go func() {
		done1 <- session.RunTurn(context.Background(), "one", events1, make(chan ApprovalDecision))
	}()
	<-model.started

	events2 := make(chan SessionNotification, 20)
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

func TestResolverErrorLeavesNoToolMessageWhenNothingWasAsked(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{ToolCalls: []ToolCall{{ID: "call-1", Name: "bash", Input: "pwd"}}},
	}}
	engine := NewEngine(model, noToolRunner{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewSession(engine)
	events := make(chan SessionNotification, 20)
	err := session.RunTurn(context.Background(), "use tool", events, make(chan ApprovalDecision))
	if err == nil {
		t.Fatal("RunTurn error = nil, want tools-disabled error")
	}

	// The resolver rejects the tool call before it reaches the transcript, so
	// the assistant never asked for anything and nothing may be answered. The
	// previous behaviour appended a tool message carrying a placeholder id,
	// which the provider rejects on the next request.
	messages := session.Snapshot().Messages
	if len(messages) != 1 || messages[0].Role != RoleUser {
		t.Fatalf("messages = %+v, want only the user message", messages)
	}
}

func TestStoreRejectsSessionIDsThatEscapeRoot(t *testing.T) {
	st := store.New(t.TempDir())
	for _, id := range []store.SessionID{"../..", "..", ".", "a/b", "_memory", "", "x y"} {
		if err := st.Delete(id); err == nil {
			t.Fatalf("Delete(%q) succeeded, want invalid-session-id error", id)
		}
		if err := st.Append(id, store.Record{Type: store.EventSessionStarted}); err == nil {
			t.Fatalf("Append(%q) succeeded, want invalid-session-id error", id)
		}
	}
}

func TestStoreHealsTornTailLine(t *testing.T) {
	root := t.TempDir()
	st := store.New(root)
	meta, err := st.Create(store.Metadata{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleUser, Content: "before crash"}}); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash mid-append by appending a truncated JSON line.
	eventsPath := filepath.Join(root, string(meta.ID), "events.jsonl")
	handle, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.WriteString(`{"type":"message_app`); err != nil {
		t.Fatal(err)
	}
	handle.Close()

	messages, err := st.Messages(meta.ID)
	if err != nil {
		t.Fatalf("Messages after torn tail: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "before crash" {
		t.Fatalf("messages = %+v, want the single record before the torn line", messages)
	}
	// The healed file must accept new appends.
	if err := st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleUser, Content: "after crash"}}); err != nil {
		t.Fatal(err)
	}
	messages, err = st.Messages(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %+v, want two after append on healed log", messages)
	}
}

func TestStoreFailsLoudlyOnMidFileCorruption(t *testing.T) {
	root := t.TempDir()
	st := store.New(root)
	meta, err := st.Create(store.Metadata{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"one", "two", "three"} {
		if err := st.Append(meta.ID, store.Record{Type: store.EventMessageAppended, Message: &Message{Role: RoleUser, Content: content}}); err != nil {
			t.Fatal(err)
		}
	}
	eventsPath := filepath.Join(root, string(meta.ID), "events.jsonl")
	raw, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(string(raw), "\n", 2)
	corrupted := "NOT JSON\n" + lines[1]
	if err := os.WriteFile(eventsPath, []byte(corrupted), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Messages(meta.ID); err == nil {
		t.Fatal("Messages on mid-file corruption succeeded, want a loud error")
	}
}
