package runtime_test

import (
	"os"
	"strings"
	"testing"

	. "super-agent/runtime"
	"super-agent/store"
)

func TestSessionExportsMarkdownJSONAndLocalHTML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	messages := []Message{{Role: RoleSystem, Content: "rules"}, {Role: RoleUser, Content: "<hello>"}}
	engine := NewEngineWithExecutor(&staticExecutor{}, messages)
	session := NewPersistentSession(engine, nil, configuredWorkspace(t, dir), SessionMetadata{ID: "session-1", Title: "Demo", Provider: "test", Model: "model", CWD: dir})
	for _, format := range []string{"markdown", "json", "html"} {
		path, err := session.Export(format)
		if err != nil {
			t.Fatalf("Export(%s): %v", format, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(content) == 0 {
			t.Fatalf("empty %s export", format)
		}
		if format == "html" && (!strings.Contains(string(content), "&lt;hello&gt;") || strings.Contains(string(content), "<hello>")) {
			t.Fatalf("unsafe HTML export: %s", content)
		}
	}
}

func TestSessionExportIncludesPersistedAuditEvents(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	st := store.New(t.TempDir())
	meta, err := st.Create(store.Metadata{Title: "Audit", Provider: "test", Model: "model", CWD: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "call-1", Name: "run_command", Input: `{"command":"go test ./..."}`}
	if err := st.Append(meta.ID, store.Record{Type: store.EventApprovalDecision, ToolCall: &call, Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(meta.ID, store.Record{Type: store.EventError, Error: "denied"}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	session := NewPersistentSession(engine, store.NewRepository(st), configuredWorkspace(t, dir), SessionMetadata{ID: SessionID(meta.ID), Title: "Audit"})
	path, err := session.Export("json")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"events"`, `"approval_decision"`, `"run_command"`, `"denied"`} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("export missing %s: %s", expected, content)
		}
	}
}
