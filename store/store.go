package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"super-agent/runtime/protocol"
)

const (
	EventSessionStarted   = "session_started"
	EventMessageAppended  = "message_appended"
	EventApprovalDecision = "approval_decision"
	EventToolResult       = "tool_result"
	EventCancel           = "cancel"
	EventReset            = "reset"
	EventError            = "error"
	EventCheckpoint       = "checkpoint"
	EventCompact          = "compact"
)

type SessionID string
type TurnID string

type Metadata struct {
	ID                     SessionID `json:"id"`
	Title                  string    `json:"title"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
	Provider               string    `json:"provider"`
	Model                  string    `json:"model"`
	CWD                    string    `json:"cwd"`
	InstructionFingerprint string    `json:"instruction_fingerprint"`
	InstructionSources     []string  `json:"instruction_sources,omitempty"`
	CurrentTurnID          TurnID    `json:"current_turn_id"`
}

type Record struct {
	Type       string             `json:"type"`
	SessionID  SessionID          `json:"session_id"`
	TurnID     TurnID             `json:"turn_id,omitempty"`
	Time       time.Time          `json:"time"`
	Message    *protocol.Message  `json:"message,omitempty"`
	ToolCall   *protocol.ToolCall `json:"tool_call,omitempty"`
	Decision   string             `json:"decision,omitempty"`
	Result     string             `json:"result,omitempty"`
	Error      string             `json:"error,omitempty"`
	Checkpoint *Checkpoint        `json:"checkpoint,omitempty"`
	Compact    *Compact           `json:"compact,omitempty"`
}

type Checkpoint struct {
	ID     string         `json:"id"`
	Files  []FileSnapshot `json:"files"`
	Reason string         `json:"reason"`
}

type FileSnapshot struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Content string `json:"content,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
}

type Compact struct {
	Summary          string             `json:"summary"`
	OriginalMessages []protocol.Message `json:"original_messages"`
	KeptMessages     []protocol.Message `json:"kept_messages"`
}

type Summary struct {
	ID        SessionID `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	CWD       string    `json:"cwd"`
}

// Store serializes every access so concurrent writers, for example a
// running turn and a user-initiated cancel, cannot interleave meta.json
// read-modify-write cycles or tear event file reads.
type Store struct {
	root string
	mu   sync.Mutex
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".superagent", "sessions"), nil
}

func New(root string) *Store {
	return &Store{root: root}
}

func OpenDefault() (*Store, error) {
	root, err := DefaultRoot()
	if err != nil {
		return nil, err
	}
	return New(root), nil
}

func NewID(now time.Time) SessionID {
	base := now.UTC().Format("20060102T150405.000000000")
	base = strings.ReplaceAll(base, ".", "")
	return SessionID(base)
}

func NewTurnID(now time.Time) TurnID {
	return TurnID(now.UTC().Format("20060102T150405.000000000"))
}

func Fingerprint(messages []protocol.Message) string {
	h := sha256.New()
	for _, message := range messages {
		if message.Role != protocol.RoleSystem {
			continue
		}
		_, _ = h.Write([]byte(message.Content))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) Create(meta Metadata, messages []protocol.Message) (Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta.ID == "" {
		meta.ID = NewID(time.Now())
	}
	now := time.Now().UTC()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now
	if meta.Title == "" {
		meta.Title = "Untitled"
	}
	if meta.InstructionFingerprint == "" {
		meta.InstructionFingerprint = Fingerprint(messages)
	}
	if err := os.MkdirAll(s.sessionDir(meta.ID), 0700); err != nil {
		return Metadata{}, err
	}
	// The transcript is written first and meta.json last: sessions become
	// discoverable only once the metadata exists, so a partial failure
	// removes the directory instead of leaving an orphan session behind.
	if err := s.appendUnlocked(meta.ID, Record{Type: EventSessionStarted}); err != nil {
		s.removeSessionDir(meta.ID)
		return Metadata{}, err
	}
	for _, message := range messages {
		msg := message
		if err := s.appendUnlocked(meta.ID, Record{Type: EventMessageAppended, Message: &msg}); err != nil {
			s.removeSessionDir(meta.ID)
			return Metadata{}, err
		}
	}
	if err := s.writeMeta(meta); err != nil {
		s.removeSessionDir(meta.ID)
		return Metadata{}, err
	}
	return meta, nil
}

func (s *Store) Append(id SessionID, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendUnlocked(id, record)
}

func (s *Store) appendUnlocked(id SessionID, record Record) error {
	if record.Type == "" {
		return errors.New("record type is required")
	}
	meta, err := s.metadataUnlocked(id)
	if err == nil {
		record.SessionID = id
		if record.TurnID == "" {
			record.TurnID = meta.CurrentTurnID
		}
		meta.UpdatedAt = time.Now().UTC()
		_ = s.writeMeta(meta)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if record.SessionID == "" {
		record.SessionID = id
	}
	if record.Time.IsZero() {
		record.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(s.sessionDir(id), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.eventsPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = file.Write(append(encoded, '\n'))
	return err
}

func (s *Store) Metadata(id SessionID) (Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metadataUnlocked(id)
}

func (s *Store) metadataUnlocked(id SessionID) (Metadata, error) {
	content, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(content, &meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func (s *Store) SetCurrentTurn(id SessionID, turn TurnID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.metadataUnlocked(id)
	if err != nil {
		return err
	}
	meta.CurrentTurnID = turn
	meta.UpdatedAt = time.Now().UTC()
	return s.writeMeta(meta)
}

func (s *Store) Rename(id SessionID, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("title is required")
	}
	meta, err := s.metadataUnlocked(id)
	if err != nil {
		return err
	}
	meta.Title = title
	meta.UpdatedAt = time.Now().UTC()
	return s.writeMeta(meta)
}

func (s *Store) Delete(id SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.sessionDir(id))
}

func (s *Store) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var summaries []Summary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.metadataUnlocked(SessionID(entry.Name()))
		if err != nil {
			continue
		}
		summaries = append(summaries, Summary{
			ID: meta.ID, Title: meta.Title, UpdatedAt: meta.UpdatedAt,
			Provider: meta.Provider, Model: meta.Model, CWD: meta.CWD,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func (s *Store) Records(id SessionID) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordsUnlocked(id)
}

func (s *Store) recordsUnlocked(id SessionID) ([]Record, error) {
	file, err := os.Open(s.eventsPath(id))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 20*1024*1024)
	var records []Record
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func (s *Store) Messages(id SessionID) ([]protocol.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.recordsUnlocked(id)
	if err != nil {
		return nil, err
	}
	return messagesFromRecords(records), nil
}

func messagesFromRecords(records []Record) []protocol.Message {
	var messages []protocol.Message
	for _, record := range records {
		switch record.Type {
		case EventMessageAppended:
			if record.Message != nil {
				messages = append(messages, *record.Message)
			}
		case EventToolResult:
			if record.ToolCall != nil {
				messages = append(messages, protocol.Message{
					Role: protocol.RoleTool, Content: record.Result,
					ToolCallID: record.ToolCall.ID, ToolName: record.ToolCall.Name,
				})
			}
		case EventReset:
			messages = systemMessages(messages)
		case EventCompact:
			if record.Compact != nil {
				messages = append([]protocol.Message(nil), record.Compact.KeptMessages...)
			}
		}
	}
	return messages
}

func (s *Store) LastCheckpoint(id SessionID) (*Checkpoint, error) {
	checkpoint, _, _, err := s.CheckpointUndo(id)
	return checkpoint, err
}

// CheckpointUndo returns the most recent checkpoint that carries file
// snapshots, the transcript as of that checkpoint, and the record index
// of the checkpoint. Checkpoints without files (tools that do not track
// paths) are skipped so undo always has something to restore.
func (s *Store) CheckpointUndo(id SessionID) (*Checkpoint, []protocol.Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.recordsUnlocked(id)
	if err != nil {
		return nil, nil, 0, err
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Type == EventCheckpoint && records[i].Checkpoint != nil && len(records[i].Checkpoint.Files) > 0 {
			checkpoint := *records[i].Checkpoint
			return &checkpoint, messagesFromRecords(records[:i]), i, nil
		}
	}
	return nil, nil, 0, os.ErrNotExist
}

// TruncateAfter drops every record after index keep, keeping the record
// at keep itself (the checkpoint marker). Rewriting goes through a
// temporary file so a failed write cannot corrupt the transcript.
func (s *Store) TruncateAfter(id SessionID, keep int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.recordsUnlocked(id)
	if err != nil {
		return err
	}
	if keep < 0 || keep >= len(records)-1 {
		return nil
	}
	tmp := s.eventsPath(id) + ".tmp"
	if err := writeRecords(tmp, records[:keep+1]); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.eventsPath(id)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeRecords(path string, records []Record) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func (s *Store) writeMeta(meta Metadata) error {
	if meta.ID == "" {
		return errors.New("session id is required")
	}
	if err := os.MkdirAll(s.sessionDir(meta.ID), 0700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(meta.ID), append(content, '\n'), 0600)
}

func (s *Store) sessionDir(id SessionID) string {
	return filepath.Join(s.root, string(id))
}

func (s *Store) metaPath(id SessionID) string {
	return filepath.Join(s.sessionDir(id), "meta.json")
}

func (s *Store) eventsPath(id SessionID) string {
	return filepath.Join(s.sessionDir(id), "events.jsonl")
}

func (s *Store) removeSessionDir(id SessionID) {
	_ = os.RemoveAll(s.sessionDir(id))
}

func systemMessages(messages []protocol.Message) []protocol.Message {
	var kept []protocol.Message
	for _, message := range messages {
		if message.Role == protocol.RoleSystem {
			kept = append(kept, message)
		}
	}
	return kept
}

func (m Metadata) String() string {
	return fmt.Sprintf("%s %s", m.ID, m.Title)
}
