package session

import "time"

type SessionID string

type Metadata struct {
	ID                 SessionID
	Title              string
	Provider           string
	Model              string
	CWD                string
	InstructionSources []string
}

type Summary struct {
	ID        SessionID
	Title     string
	UpdatedAt time.Time
	Provider  string
	Model     string
	CWD       string
}

type FileSnapshot struct {
	Path    string
	Exists  bool
	Content string
	Mode    uint32
}

// Repository is the outbound persistence port used by session use cases.
// Implementations decide how metadata, transcripts, and checkpoints are stored.
type Repository interface {
	Create(Metadata, []Message) (Metadata, error)
	StartTurn(SessionID) error
	SaveMessage(SessionID, Message) error
	SaveApproval(SessionID, ApprovalDecision, *ToolCall) error
	SaveError(SessionID, error) error
	SaveCancel(SessionID) error
	SaveReset(SessionID) error
	SaveCompaction(SessionID, string, []Message, []Message) error
	SaveCheckpoint(SessionID, ToolCall, []FileSnapshot) error
	List() ([]Summary, error)
	Load(SessionID) ([]Message, Metadata, error)
	Rename(SessionID, string) error
	Delete(SessionID) error
	// CheckpointState returns the files of the most recent non-empty
	// checkpoint, the transcript as of that checkpoint, and the checkpoint
	// record index for use with TruncateAfter.
	CheckpointState(SessionID) ([]FileSnapshot, []Message, int, error)
	// TruncateAfter drops every record after the given index, keeping the
	// checkpoint record itself.
	TruncateAfter(SessionID, int) error
}

// Workspace is the outbound filesystem port used by checkpoints and undo.
type Workspace interface {
	Capture([]string) ([]FileSnapshot, error)
	Restore([]FileSnapshot) error
}
