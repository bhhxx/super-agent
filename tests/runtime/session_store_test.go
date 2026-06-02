package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "super-agent/runtime"
	"super-agent/runtime/store"
)

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
	session := NewPersistentSession(engine, st, store.Metadata{ID: "new"})
	if err := session.Resume(meta.ID); err != nil {
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
	session := NewPersistentSession(engine, st, meta)
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
	session := NewPersistentSession(engine, st, meta)
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
	session := NewPersistentSession(engine, st, meta)
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

type staticExecutor struct{}

func (x *staticExecutor) Execute(context.Context, Effect, ExecutionInput, func(StreamChunk)) (ExecutionResult, error) {
	return ModelReplied{Response: ModelResponse{Content: "model summary"}}, nil
}

func (x *staticExecutor) ToolSpecs() []ToolSpec {
	return nil
}
