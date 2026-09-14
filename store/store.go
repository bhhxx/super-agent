package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	EventContextReplaced  = "context_replaced"
)

type SessionID string
type TurnID string

type Metadata struct {
	ID                     SessionID      `json:"id"`
	Title                  string         `json:"title"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
	Provider               string         `json:"provider"`
	Model                  string         `json:"model"`
	CWD                    string         `json:"cwd"`
	InstructionFingerprint string         `json:"instruction_fingerprint"`
	InstructionSources     []string       `json:"instruction_sources,omitempty"`
	CurrentTurnID          TurnID         `json:"current_turn_id"`
	ParentID               SessionID      `json:"parent_id,omitempty"`
	ProjectID              string         `json:"project_id,omitempty"`
	ConfigRoot             string         `json:"config_root,omitempty"`
	Workspace              *WorkspaceSpec `json:"workspace,omitempty"`
}

type WorkspaceSpec struct {
	PrimaryRoot string              `json:"primary_root"`
	CWD         string              `json:"cwd"`
	Roots       []WorkspaceRootSpec `json:"roots"`
}

type WorkspaceRootSpec struct {
	Path   string `json:"path"`
	Access string `json:"access"`
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
	Messages   []protocol.Message `json:"messages,omitempty"`
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
	ParentID  SessionID `json:"parent_id,omitempty"`
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

// validateID rejects session identifiers that could escape the sessions
// root. Every ID is joined into filesystem paths via filepath.Join, so path
// separators, dot segments, and reserved names must never reach the store:
// user-supplied ids (for example from /resume or /delete-session) resolve to
// directories here unchecked otherwise.
func validateID(id SessionID) error {
	name := string(id)
	if name == "" {
		return errors.New("session id is required")
	}
	if name == "." || name == ".." || name != filepath.Base(name) || strings.HasPrefix(name, "_") {
		return errors.New("invalid session id: " + name)
	}
	for _, r := range name {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
		default:
			return errors.New("invalid session id: " + name)
		}
	}
	return nil
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
	if err := validateID(meta.ID); err != nil {
		return Metadata{}, err
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
	if err := validateID(id); err != nil {
		return err
	}
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
		// Best effort: the UpdatedAt refresh is cosmetic, and refusing to
		// append the record over a failed refresh would lose the transcript
		// entry the record exists for.
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
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	// Sync every append: a torn tail line would otherwise make the whole
	// transcript unreadable after a crash or power loss.
	return file.Sync()
}

func (s *Store) Metadata(id SessionID) (Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateID(id); err != nil {
		return Metadata{}, err
	}
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
	if err := validateID(id); err != nil {
		return err
	}
	meta, err := s.metadataUnlocked(id)
	if err != nil {
		return err
	}
	meta.CurrentTurnID = turn
	meta.UpdatedAt = time.Now().UTC()
	return s.writeMeta(meta)
}

// SaveWorkspaceDescription persists the durable workspace description and its
// canonical cwd. It rewrites only those fields of the session metadata so an
// upgrade from legacy metadata cannot disturb unrelated values.
func (s *Store) SaveWorkspaceDescription(id SessionID, spec WorkspaceSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.metadataUnlocked(id)
	if err != nil {
		return err
	}
	cloned := spec
	cloned.Roots = append([]WorkspaceRootSpec(nil), spec.Roots...)
	meta.Workspace = &cloned
	meta.CWD = spec.CWD
	meta.UpdatedAt = time.Now().UTC()
	return s.writeMeta(meta)
}

func (s *Store) RenameSession(id SessionID, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateID(id); err != nil {
		return err
	}
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
	if err := validateID(id); err != nil {
		return err
	}
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
			Provider: meta.Provider, Model: meta.Model, CWD: meta.CWD, ParentID: meta.ParentID,
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
	if err := validateID(id); err != nil {
		return nil, err
	}
	return s.recordsUnlocked(id)
}

// recordsUnlocked reads the event log. A torn final line — the signature of
// a crash mid-append — is healed by truncating back to the last complete
// record; corruption anywhere else fails loudly, because silently dropping
// mid-file records would rewrite history.
func (s *Store) recordsUnlocked(id SessionID) ([]Record, error) {
	file, err := os.Open(s.eventsPath(id))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 20*1024*1024)
	var records []Record
	var good int64
	for scanner.Scan() {
		line := scanner.Bytes()
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			offset := good + int64(len(line)) + 1
			if s.onlyWhitespaceAfter(file, offset) {
				return records, os.Truncate(s.eventsPath(id), good)
			}
			return nil, fmt.Errorf("corrupt session event at byte %d: %w", good, err)
		}
		records = append(records, record)
		good += int64(len(line)) + 1
	}
	return records, scanner.Err()
}

// onlyWhitespaceAfter reports whether the file holds nothing but whitespace
// after offset, i.e. the parse failure hit the tail of the log.
func (s *Store) onlyWhitespaceAfter(file *os.File, offset int64) bool {
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return false
	}
	rest, err := io.ReadAll(file)
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(rest)) == 0
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
		case EventContextReplaced:
			messages = append([]protocol.Message(nil), record.Messages...)
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
	if err := validateID(id); err != nil {
		return nil, nil, 0, err
	}
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
	if err := validateID(id); err != nil {
		return err
	}
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
	if err := writer.Flush(); err != nil {
		return err
	}
	// Sync before the caller renames the temporary file into place, so the
	// rewrite is durable when it becomes visible.
	return file.Sync()
}

func (s *Store) writeMeta(meta Metadata) error {
	if err := validateID(meta.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.sessionDir(meta.ID), 0700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	// meta.json gates session discovery and every further append, so a torn
	// write must never leave a half-written file behind: write, fsync, and
	// atomically rename a temporary file, like SaveMemory does.
	temporary, err := os.CreateTemp(s.sessionDir(meta.ID), ".meta-*.json")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(path, s.metaPath(meta.ID))
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

func (s *Store) memoryPath() string { return filepath.Join(s.root, "_memory.json") }

func (s *Store) LoadMemory() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := os.ReadFile(s.memoryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []string
	if err := json.Unmarshal(content, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) SaveMemory(items []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	temporary, err := os.CreateTemp(s.root, ".memory-*.json")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(path, s.memoryPath())
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
