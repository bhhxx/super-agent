package runtime_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	. "super-agent/runtime"
)

func TestAttachedFileIsConsumedByNextUserMessage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("attachment body"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngineWithExecutor(&staticExecutor{}, nil)
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	session := NewPersistentSession(engine, nil, configuredWorkspace(t, dir), SessionMetadata{})
	attachment, err := session.Attach("note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Name != "note.txt" || attachment.Data != base64.StdEncoding.EncodeToString([]byte("attachment body")) {
		t.Fatalf("attachment = %+v", attachment)
	}
	notifications := make(chan SessionNotification, 20)
	if err := session.RunTurn(context.Background(), "inspect", notifications, make(chan ApprovalDecision)); err != nil {
		t.Fatal(err)
	}
	messages := session.Snapshot().Messages
	if len(messages) < 1 || len(messages[0].Attachments) != 1 || messages[0].Attachments[0].Name != "note.txt" {
		t.Fatalf("messages = %+v", messages)
	}
	if len(session.PendingAttachments()) != 0 {
		t.Fatalf("pending = %+v", session.PendingAttachments())
	}
}

func TestAttachmentCannotEscapeWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	session := NewPersistentSession(NewEngineWithExecutor(&staticExecutor{}, nil), nil, configuredWorkspace(t, dir), SessionMetadata{})
	if _, err := session.Attach("../outside.txt"); err == nil {
		t.Fatal("outside attachment accepted")
	}
}
