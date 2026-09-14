package commands

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Config carries the session settings commands display but do not own.
type Config struct {
	CWD              string
	InstructionPaths []string
	NoTools          bool
}

// Input carries what the root knows about a submission and the command cannot
// own, such as the attachments another feature currently holds.
type Input struct {
	Text        string
	Attachments []Attachment
}

// Model owns the slash-command catalogue, its input semantics, and the
// background operations it started on the user's behalf.
type Model struct {
	ports          Ports
	config         Config
	customCommands []string
	compacting     bool
	managingMCP    bool
}

func New(config Config, ports Ports) Model {
	return Model{config: config, ports: ports, customCommands: ports.Extensions.CustomCommands()}
}

// Compacting reports whether /compact is still running.
func (m Model) Compacting() bool { return m.compacting }

// ManagingMCP reports whether an MCP lifecycle change is still running.
func (m Model) ManagingMCP() bool { return m.managingMCP }

// Update applies the result of a background command. A nil Outcome means the
// message did not belong to this feature.
func (m Model) Update(message tea.Msg) (Model, *Outcome) {
	switch message := message.(type) {
	case CompactDone:
		m.compacting = false
		if message.Err != nil {
			return m, &Outcome{Err: "Compact failed: " + message.Err.Error()}
		}
		return m, &Outcome{Status: "Compacted conversation", Output: divider("Compacted conversation"), RefreshSnapshot: true}
	case MCPDone:
		m.managingMCP = false
		if message.Err != nil {
			return m, &Outcome{Err: "MCP failed: " + message.Err.Error()}
		}
		return m, &Outcome{Status: message.Status}
	default:
		return m, nil
	}
}

// Handle runs the slash command in input.Text. Callers check IsCommand first.
func (m Model) Handle(input Input) (Model, *Outcome, tea.Cmd) {
	parts := strings.Fields(input.Text)
	command := parts[0]
	switch command {
	case "/agent":
		return m, m.handleAgent(parts), nil
	case "/plan":
		return m, m.handleAgent([]string{"/agent", "plan"}), nil
	case "/build":
		return m, m.handleAgent([]string{"/agent", "build"}), nil
	case "/mode":
		if len(parts) != 2 || (parts[1] != "plan" && parts[1] != "build") {
			return m, &Outcome{Err: "Usage: /mode <plan|build>"}, nil
		}
		return m, m.handleAgent([]string{"/agent", parts[1]}), nil
	case "/fork":
		return m, m.handleFork(strings.TrimSpace(strings.TrimPrefix(input.Text, command))), nil
	case "/memory":
		return m, m.handleMemory(), nil
	case "/remember":
		return m, m.handleRemember(strings.TrimSpace(strings.TrimPrefix(input.Text, command))), nil
	case "/forget":
		return m, m.handleForget(), nil
	case "/diff":
		return m, m.handleGitDiff(), nil
	case "/branch":
		return m, m.handleGitStatus(), nil
	case "/review":
		return m, &Outcome{Prompt: "Review the current changes. Inspect the git diff, relevant code, and LSP diagnostics when configured, then report only actionable defects with file and line references. Do not modify files."}, nil
	case "/fix-ci":
		return m, &Outcome{Prompt: "Inspect the repository CI configuration and current failures, reproduce them locally, implement the fixes, and verify the result."}, nil
	case "/commit-message":
		return m, &Outcome{Prompt: "Inspect the current git diff and suggest one concise conventional commit subject. Do not modify files or commit."}, nil
	case "/export":
		if len(parts) != 2 {
			return m, &Outcome{Err: "Usage: /export <markdown|json>"}, nil
		}
		return m, m.handleExport(parts[1]), nil
	case "/share":
		return m, m.handleExport("html"), nil
	case "/attach":
		if len(parts) != 2 {
			return m, &Outcome{Err: "Usage: /attach <path>"}, nil
		}
		return m, &Outcome{Status: "Attaching…", AttachPath: parts[1]}, nil
	case "/attachments":
		if len(input.Attachments) == 0 {
			return m, &Outcome{Status: "No pending attachments"}, nil
		}
		names := make([]string, 0, len(input.Attachments))
		for _, item := range input.Attachments {
			names = append(names, item.Name+" ("+item.MIME+")")
		}
		return m, &Outcome{Status: "Attachments", Output: "Attachments:\n- " + strings.Join(names, "\n- ")}, nil
	case "/commands":
		return m, &Outcome{Status: "Custom commands", Output: formatNamedItems("Custom commands", m.ports.Extensions.CustomCommands())}, nil
	case "/skills":
		return m, &Outcome{Status: "Skills", Output: formatNamedItems("Skills", m.ports.Extensions.Skills())}, nil
	case "/plugins":
		return m, &Outcome{Status: "Plugins", Output: formatNamedItems("Plugins", m.ports.Extensions.Plugins())}, nil
	case "/diagnostics":
		if len(parts) != 2 {
			return m, &Outcome{Err: "Usage: /diagnostics <path>"}, nil
		}
		result, err := m.ports.Workspace.Diagnostics(context.Background(), parts[1])
		if err != nil {
			return m, &Outcome{Err: "Diagnostics failed: " + err.Error()}, nil
		}
		return m, &Outcome{Status: "Diagnostics", Output: result}, nil
	case "/instructions":
		return m, &Outcome{Status: "Instructions", Output: formatInstructions(m.config.InstructionPaths)}, nil
	case "/permissions":
		return m, m.handlePermissions(parts), nil
	case "/mcp":
		return m.handleMCP(parts)
	case "/clear", "/reset":
		return m, m.handleReset(), nil
	case "/sessions":
		return m, m.handleSessions(), nil
	case "/resume":
		return m, m.handleResume(parts), nil
	case "/rename":
		return m, m.handleRename(input.Text, parts), nil
	case "/delete-session":
		return m, m.handleDelete(parts), nil
	case "/compact":
		return m.handleCompact(input.Text, command)
	case "/undo":
		return m, m.handleUndo(), nil
	case "/quit", "/exit":
		return m, &Outcome{Quit: true}, nil
	case "/help":
		return m, &Outcome{ShowHelp: true}, nil
	default:
		arguments := strings.TrimSpace(strings.TrimPrefix(input.Text, command))
		expanded, err := m.ports.Extensions.ExpandCustomCommand(strings.TrimPrefix(command, "/"), arguments)
		if err != nil {
			return m, &Outcome{Err: "Unknown command: " + command}, nil
		}
		return m, &Outcome{Prompt: expanded}, nil
	}
}

// handleCompact reports the keep-newest policy stays in the runtime; the TUI
// only passes the optional summary.
func (m Model) handleCompact(text, command string) (Model, *Outcome, tea.Cmd) {
	summary := strings.TrimSpace(strings.TrimPrefix(text, command))
	m.compacting = true
	run := func() tea.Msg { return CompactDone{Err: m.ports.Sessions.Compact(context.Background(), summary)} }
	return m, &Outcome{Status: "Compacting conversation…"}, run
}

func (m Model) handleMCP(parts []string) (Model, *Outcome, tea.Cmd) {
	if len(parts) == 1 || (len(parts) == 2 && parts[1] == "list") {
		return m, &Outcome{Status: "MCP servers", Output: formatMCPServers(m.ports.MCP.ListMCPServers())}, nil
	}
	operation := parts[1]
	ctx := context.Background()
	var run func() error
	var status string
	switch operation {
	case "add":
		if len(parts) < 4 {
			return m, &Outcome{Err: "Usage: /mcp add <name> <command> [args...]"}, nil
		}
		name, command, args := parts[2], parts[3], append([]string(nil), parts[4:]...)
		run = func() error { return m.ports.MCP.AddMCPServer(ctx, name, command, args) }
		status = "Added MCP server " + name
	case "remove":
		if len(parts) != 3 {
			return m, &Outcome{Err: "Usage: /mcp remove <name>"}, nil
		}
		name := parts[2]
		run = func() error { return m.ports.MCP.RemoveMCPServer(name) }
		status = "Removed MCP server " + name
	case "restart":
		if len(parts) != 3 {
			return m, &Outcome{Err: "Usage: /mcp restart <name>"}, nil
		}
		name := parts[2]
		run = func() error { return m.ports.MCP.RestartMCPServer(ctx, name) }
		status = "Restarted MCP server " + name
	default:
		return m, &Outcome{Err: "Usage: /mcp <list|add|remove|restart>"}, nil
	}
	m.managingMCP = true
	change := func() tea.Msg { return MCPDone{Status: status, Err: run()} }
	return m, &Outcome{Status: "Updating MCP servers…"}, change
}

func (m Model) handleAgent(parts []string) *Outcome {
	if len(parts) == 1 || (len(parts) == 2 && parts[1] == "list") {
		current := m.ports.Agents.CurrentAgent().Name
		var rows []string
		for _, profile := range m.ports.Agents.ListAgents() {
			marker := "  "
			if profile.Name == current {
				marker = "* "
			}
			rows = append(rows, marker+profile.Name+" ("+profile.Provider+"/"+profile.Model+", "+profile.PermissionMode+")")
		}
		return &Outcome{Status: "Agents", Output: strings.Join(rows, "\n")}
	}
	if len(parts) != 2 {
		return &Outcome{Err: "Usage: /agent <list|name>"}
	}
	if err := m.ports.Agents.UseAgent(parts[1]); err != nil {
		return &Outcome{Err: "Agent failed: " + err.Error()}
	}
	profile := m.ports.Agents.CurrentAgent()
	return &Outcome{Status: "Using agent " + profile.Name, RefreshSnapshot: true, StatusBar: &StatusBar{
		ModelName:      profile.Model,
		PermissionMode: profile.PermissionMode,
	}}
}

func (m Model) handlePermissions(parts []string) *Outcome {
	if len(parts) >= 3 && parts[1] == "mode" {
		if err := m.ports.Permissions.SetPermissionMode(parts[2]); err != nil {
			return &Outcome{Err: "Permissions failed: " + err.Error()}
		}
	}
	// Report the runtime's view of the policy instead of deriving it locally,
	// so the display cannot drift from actual behavior. The model is untouched,
	// so the status bar keeps showing it.
	mode := m.ports.Permissions.PermissionMode()
	return &Outcome{Status: "Permissions", Output: formatPermissions(m.config, mode, m.ports.Permissions.AutoApproveTools()), StatusBar: &StatusBar{
		PermissionMode: mode,
	}}
}

func (m Model) handleReset() *Outcome {
	if err := m.ports.Sessions.Reset(); err != nil {
		return &Outcome{Err: "Reset failed: " + err.Error(), RefreshSnapshot: true}
	}
	return &Outcome{Output: divider("New conversation"), RefreshSnapshot: true}
}

func (m Model) handleSessions() *Outcome {
	summaries, err := m.ports.Sessions.ListSessions()
	if err != nil {
		return &Outcome{Err: "Sessions failed: " + err.Error()}
	}
	return &Outcome{Status: "Sessions", Output: formatSessions(summaries)}
}

func (m Model) handleResume(parts []string) *Outcome {
	if len(parts) < 2 {
		return &Outcome{Err: "Usage: /resume <id>"}
	}
	if err := m.ports.Sessions.Resume(parts[1]); err != nil {
		return &Outcome{Err: "Resume failed: " + err.Error()}
	}
	return &Outcome{Status: "Resumed " + parts[1], Output: divider("Resumed session " + parts[1]), RefreshSnapshot: true}
}

func (m Model) handleRename(text string, parts []string) *Outcome {
	if len(parts) < 3 {
		return &Outcome{Err: "Usage: /rename <id> <title>"}
	}
	title := strings.TrimSpace(strings.TrimPrefix(text, parts[0]+" "+parts[1]))
	if err := m.ports.Sessions.RenameSession(parts[1], title); err != nil {
		return &Outcome{Err: "Rename failed: " + err.Error()}
	}
	return &Outcome{Status: "Renamed " + parts[1]}
}

func (m Model) handleDelete(parts []string) *Outcome {
	if len(parts) < 2 {
		return &Outcome{Err: "Usage: /delete-session <id>"}
	}
	if err := m.ports.Sessions.DeleteSession(parts[1]); err != nil {
		return &Outcome{Err: "Delete failed: " + err.Error()}
	}
	return &Outcome{Status: "Deleted " + parts[1]}
}

func (m Model) handleUndo() *Outcome {
	if err := m.ports.Sessions.Undo(); err != nil {
		return &Outcome{Err: "Undo failed: " + err.Error()}
	}
	return &Outcome{Status: "Restored last checkpoint", Output: divider("Restored checkpoint"), RefreshSnapshot: true}
}

func (m Model) handleExport(format string) *Outcome {
	path, err := m.ports.Sessions.Export(format)
	if err != nil {
		return &Outcome{Err: "Export failed: " + err.Error()}
	}
	return &Outcome{Status: "Exported " + path}
}

func (m Model) handleFork(title string) *Outcome {
	id, err := m.ports.Sessions.Fork(title)
	if err != nil {
		return &Outcome{Err: "Fork failed: " + err.Error()}
	}
	return &Outcome{Status: "Forked session " + id, RefreshSnapshot: true}
}

func (m Model) handleMemory() *Outcome {
	items, err := m.ports.Memory.Memories()
	if err != nil {
		return &Outcome{Err: "Memory failed: " + err.Error()}
	}
	if len(items) == 0 {
		return &Outcome{Status: "No cross-session memory"}
	}
	return &Outcome{Status: "Memory", Output: "Memory:\n- " + strings.Join(items, "\n- ")}
}

func (m Model) handleRemember(value string) *Outcome {
	if err := m.ports.Memory.Remember(value); err != nil {
		return &Outcome{Err: "Remember failed: " + err.Error()}
	}
	return &Outcome{Status: "Memory saved"}
}

func (m Model) handleForget() *Outcome {
	if err := m.ports.Memory.ForgetMemories(); err != nil {
		return &Outcome{Err: "Forget failed: " + err.Error()}
	}
	return &Outcome{Status: "Memory cleared"}
}

func (m Model) handleGitDiff() *Outcome {
	result, err := m.ports.Workspace.GitDiff(context.Background())
	if err != nil {
		return &Outcome{Err: "Diff failed: " + err.Error()}
	}
	if strings.TrimSpace(result) == "" {
		return &Outcome{Status: "No changes"}
	}
	return &Outcome{Status: "Patch preview", Output: result}
}

func (m Model) handleGitStatus() *Outcome {
	result, err := m.ports.Workspace.GitStatus(context.Background())
	if err != nil {
		return &Outcome{Err: "Branch status failed: " + err.Error()}
	}
	return &Outcome{Status: "Branch status", Output: result}
}
