package session

import "time"

type SessionID string

type WorkspaceAccessMode string

const (
	WorkspaceAccessRead      WorkspaceAccessMode = "read"
	WorkspaceAccessReadWrite WorkspaceAccessMode = "read_write"
)

type WorkspaceRootSpec struct {
	Path   string
	Access WorkspaceAccessMode
}

// WorkspaceSpec is durable session data. Filesystem validation and access
// decisions belong to the concrete Workspace implementation.
type WorkspaceSpec struct {
	PrimaryRoot string
	CWD         string
	Roots       []WorkspaceRootSpec
}

type Metadata struct {
	ID                 SessionID
	Title              string
	Provider           string
	Model              string
	CWD                string
	InstructionSources []string
	ParentID           SessionID
	ProjectID          string
	ConfigRoot         string
	WorkspaceSpec      *WorkspaceSpec
}

type Summary struct {
	ID        SessionID
	Title     string
	UpdatedAt time.Time
	Provider  string
	Model     string
	CWD       string
	ParentID  SessionID
}

type FileSnapshot struct {
	Path    string
	Exists  bool
	Content string
	Mode    uint32
}

type AuditEvent struct {
	Type     string    `json:"type"`
	Time     time.Time `json:"time"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	Decision string    `json:"decision,omitempty"`
	Result   string    `json:"result,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Repository is the outbound persistence port used by session use cases.
// Implementations decide how metadata, transcripts, and checkpoints are stored.
type Repository interface {
	Create(Metadata, []Message) (Metadata, error)
	AssignNewTurnID(SessionID) error
	SaveMessage(SessionID, Message) error
	SaveApproval(SessionID, ApprovalDecision, *ToolCall) error
	SaveError(SessionID, error) error
	SaveCancel(SessionID) error
	SaveReset(SessionID) error
	SaveConversationReplacement(SessionID, []Message) error
	SaveCompaction(SessionID, string, []Message, []Message) error
	SaveCheckpoint(SessionID, ToolCall, []FileSnapshot) error
	List() ([]Summary, error)
	Load(SessionID) ([]Message, Metadata, error)
	LoadAuditEvents(SessionID) ([]AuditEvent, error)
	RenameSession(SessionID, string) error
	Delete(SessionID) error
	// LoadUndoPoint returns the files of the most recent non-empty
	// checkpoint, the transcript as of that checkpoint, and the checkpoint
	// record index for use with TruncateAfter.
	LoadUndoPoint(SessionID) ([]FileSnapshot, []Message, int, error)
	// TruncateAfter drops every record after the given index, keeping the
	// checkpoint record itself.
	TruncateAfter(SessionID, int) error
	LoadMemory() ([]string, error)
	SaveMemory([]string) error
	// SaveWorkspaceDescription persists the durable workspace description and
	// its canonical cwd without touching unrelated metadata. Resume uses it to
	// upgrade legacy metadata to canonical WorkspaceSpec form exactly once.
	SaveWorkspaceDescription(SessionID, WorkspaceSpec) error
}

// Workspace is the outbound filesystem port used by checkpoints and undo.
type Workspace interface {
	Spec() WorkspaceSpec
	Validate(WorkspaceSpec) error
	// Canonicalize re-resolves every path in the spec against the current
	// filesystem. Resume uses it once to upgrade legacy metadata, whose saved
	// cwd never promised a canonical path; new specs stay strict.
	Canonicalize(WorkspaceSpec) (WorkspaceSpec, error)
	Activate(WorkspaceSpec) error
	Capture([]string) ([]FileSnapshot, error)
	Restore([]FileSnapshot) error
}
