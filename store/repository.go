package store

import (
	"fmt"
	"os"
	"time"

	"super-agent/runtime/protocol"
	"super-agent/runtime/session"
)

type Repository struct{ store *Store }

func NewRepository(store *Store) *Repository { return &Repository{store: store} }

func (r Repository) Create(meta session.Metadata, messages []protocol.Message) (session.Metadata, error) {
	created, err := r.store.Create(Metadata{
		ID:                 SessionID(meta.ID),
		Title:              meta.Title,
		Provider:           meta.Provider,
		Model:              meta.Model,
		CWD:                meta.CWD,
		InstructionSources: meta.InstructionSources,
	}, messages)
	if err != nil {
		return session.Metadata{}, err
	}
	return toSessionMetadata(created), nil
}

func (r Repository) StartTurn(id session.SessionID) error {
	if err := r.store.SetCurrentTurn(SessionID(id), NewTurnID(time.Now())); err != nil {
		logPersistenceFailure("start turn", id, err)
		return err
	}
	return nil
}

func (r Repository) SaveMessage(id session.SessionID, message protocol.Message) error {
	msg := message
	record := Record{Type: EventMessageAppended, Message: &msg}
	if msg.Role == protocol.RoleTool {
		call := protocol.ToolCall{ID: msg.ToolCallID, Name: msg.ToolName}
		record.Type = EventToolResult
		record.ToolCall = &call
		record.Result = msg.Content
	}
	if err := r.store.Append(SessionID(id), record); err != nil {
		logPersistenceFailure("save message", id, err)
		return err
	}
	return nil
}

func (r Repository) SaveApproval(id session.SessionID, decision session.ApprovalDecision, call *protocol.ToolCall) error {
	if err := r.store.Append(SessionID(id), Record{Type: EventApprovalDecision, Decision: string(decision), ToolCall: call}); err != nil {
		logPersistenceFailure("save approval", id, err)
		return err
	}
	return nil
}

func (r Repository) SaveError(id session.SessionID, err error) error {
	if err == nil {
		return nil
	}
	if appendErr := r.store.Append(SessionID(id), Record{Type: EventError, Error: err.Error()}); appendErr != nil {
		logPersistenceFailure("save error", id, appendErr)
		return appendErr
	}
	return nil
}

func (r Repository) SaveCancel(id session.SessionID) error {
	if err := r.store.Append(SessionID(id), Record{Type: EventCancel}); err != nil {
		logPersistenceFailure("save cancel", id, err)
		return err
	}
	return nil
}

func (r Repository) SaveReset(id session.SessionID) error {
	if err := r.store.Append(SessionID(id), Record{Type: EventReset}); err != nil {
		logPersistenceFailure("save reset", id, err)
		return err
	}
	return nil
}

func (r Repository) SaveCompaction(id session.SessionID, summary string, original, kept []protocol.Message) error {
	if err := r.store.Append(SessionID(id), Record{Type: EventCompact, Compact: &Compact{Summary: summary, OriginalMessages: original, KeptMessages: kept}}); err != nil {
		logPersistenceFailure("save compaction", id, err)
		return err
	}
	return nil
}

// logPersistenceFailure keeps best-effort persistence visible: the turn
// keeps running, but transcript loss is reported instead of swallowed.
func logPersistenceFailure(operation string, id session.SessionID, err error) {
	fmt.Fprintf(os.Stderr, "super-agent: failed to persist %s for session %s: %v\n", operation, id, err)
}

func (r Repository) SaveCheckpoint(id session.SessionID, call protocol.ToolCall, files []session.FileSnapshot) error {
	if len(files) == 0 {
		return nil
	}
	cp := Checkpoint{ID: time.Now().UTC().Format("20060102T150405.000000000"), Reason: call.Name}
	for _, file := range files {
		cp.Files = append(cp.Files, FileSnapshot{Path: file.Path, Exists: file.Exists, Content: file.Content, Mode: file.Mode})
	}
	return r.store.Append(SessionID(id), Record{Type: EventCheckpoint, ToolCall: &call, Checkpoint: &cp})
}

func (r Repository) List() ([]session.Summary, error) {
	items, err := r.store.List()
	if err != nil {
		return nil, err
	}
	result := make([]session.Summary, 0, len(items))
	for _, item := range items {
		result = append(result, session.Summary{ID: session.SessionID(item.ID), Title: item.Title, UpdatedAt: item.UpdatedAt, Provider: item.Provider, Model: item.Model, CWD: item.CWD})
	}
	return result, nil
}

func (r Repository) Load(id session.SessionID) ([]protocol.Message, session.Metadata, error) {
	messages, err := r.store.Messages(SessionID(id))
	if err != nil {
		return nil, session.Metadata{}, err
	}
	meta, err := r.store.Metadata(SessionID(id))
	if err != nil {
		return nil, session.Metadata{}, err
	}
	return messages, toSessionMetadata(meta), nil
}

func (r Repository) Rename(id session.SessionID, title string) error {
	return r.store.Rename(SessionID(id), title)
}
func (r Repository) Delete(id session.SessionID) error {
	return r.store.Delete(SessionID(id))
}

func (r Repository) CheckpointState(id session.SessionID) ([]session.FileSnapshot, []protocol.Message, int, error) {
	cp, messages, index, err := r.store.CheckpointUndo(SessionID(id))
	if err != nil {
		return nil, nil, 0, err
	}
	files := make([]session.FileSnapshot, 0, len(cp.Files))
	for _, file := range cp.Files {
		files = append(files, session.FileSnapshot{Path: file.Path, Exists: file.Exists, Content: file.Content, Mode: file.Mode})
	}
	return files, messages, index, nil
}

func (r Repository) TruncateAfter(id session.SessionID, index int) error {
	return r.store.TruncateAfter(SessionID(id), index)
}

func toSessionMetadata(meta Metadata) session.Metadata {
	return session.Metadata{ID: session.SessionID(meta.ID), Title: meta.Title, Provider: meta.Provider, Model: meta.Model, CWD: meta.CWD, InstructionSources: meta.InstructionSources}
}
