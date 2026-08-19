package permission

type CommandClass string

const (
	CommandClassReadOnly    CommandClass = "read-only"
	CommandClassWrite       CommandClass = "write"
	CommandClassNetwork     CommandClass = "network"
	CommandClassDestructive CommandClass = "destructive"
	CommandClassUnknown     CommandClass = "unknown"
)

type Request struct {
	ToolName     string
	Command      string
	CommandClass CommandClass
	CWD          string
	TouchedPaths []string
	EnvVars      []string
	Reason       string
}
