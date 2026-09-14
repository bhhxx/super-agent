package commands

// StatusBar carries the session values a command changed, so the root status
// line follows an agent or permission switch without reading the runtime itself.
// Only a command that switches agents moves the model, so an empty ModelName
// keeps the value the status line already shows.
type StatusBar struct {
	ModelName      string
	PermissionMode string
}

// Outcome is a command's request to the root. Every field is a separate,
// explicit request: the root applies the ones it owns and routes the
// cross-feature ones to the feature that owns them. A nil Outcome means the
// message produced no visible effect.
type Outcome struct {
	Status string
	Err    string
	// Output is committed to terminal scrollback rather than the live view.
	Output string
	// Prompt starts a new turn with this text, which is how workflow commands
	// and expanded custom commands submit their instructions.
	Prompt string
	// AttachPath queues a workspace file through the attachments feature.
	AttachPath string
	// RefreshSnapshot asks the root to re-read conversation state after a
	// command replaced, restored, or shrank the transcript.
	RefreshSnapshot bool
	StatusBar       *StatusBar
	ShowHelp        bool
	Quit            bool
}

// CompactDone reports the result of an asynchronous /compact run.
type CompactDone struct{ Err error }

// MCPDone reports the result of an asynchronous MCP lifecycle change.
type MCPDone struct {
	Status string
	Err    error
}
