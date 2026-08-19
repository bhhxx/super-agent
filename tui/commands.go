package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var slashCommands = []string{
	"/clear", "/compact", "/delete-session", "/help", "/instructions", "/permissions",
	"/quit", "/rename", "/reset", "/resume", "/sessions", "/undo",
}

var slashCommandDescriptions = map[string]string{
	"/clear":          "Reset the conversation",
	"/compact":        "Compact context [summary]",
	"/delete-session": "Delete a saved session <id>",
	"/help":           "Show commands and shortcuts",
	"/instructions":   "Show loaded instruction files",
	"/permissions":    "Inspect or change permission mode",
	"/quit":           "Exit Super Agent",
	"/rename":         "Rename a session <id> <title>",
	"/reset":          "Reset the conversation",
	"/resume":         "Resume a saved session <id>",
	"/sessions":       "List saved sessions",
	"/undo":           "Restore the last checkpoint",
}

func completeSlashCommand(value string) (string, bool) {
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t\n") {
		return value, false
	}
	match := ""
	for _, command := range slashCommands {
		if !strings.HasPrefix(command, value) {
			continue
		}
		if match != "" {
			return value, false
		}
		match = command
	}
	if match == "" || match == value {
		return value, false
	}
	return match, true
}

func matchingSlashCommands(value string) []string {
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t\n") {
		return nil
	}
	var matches []string
	for _, command := range slashCommands {
		if strings.HasPrefix(command, value) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (a App) slashMatches() []string {
	return matchingSlashCommands(a.input.Value())
}

func (a App) completeSelectedSlashCommand() (App, bool) {
	matches := a.slashMatches()
	if len(matches) == 0 {
		return a, false
	}
	if a.slashSelection >= len(matches) {
		a.slashSelection = len(matches) - 1
	}
	selected := matches[a.slashSelection]
	if a.input.Value() == selected {
		return a, false
	}
	a.input.SetValue(selected)
	a.input.CursorEnd()
	a.slashSelection = 0
	a.resizeInput()
	return a, true
}

func (a App) submit() (tea.Model, tea.Cmd) {
	if a.compacting {
		a.status = "Compacting conversation…"
		return a, nil
	}
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return a, nil
	}
	a.remember(text)
	if strings.HasPrefix(text, "/") {
		return a.runSlashCommand(text)
	}
	return a.startTurn(text)
}

func (a App) queueInput() (tea.Model, tea.Cmd) {
	if a.compacting {
		a.status = "Compacting conversation…"
		return a, nil
	}
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return a, nil
	}
	if strings.HasPrefix(text, "/") {
		a.err = "Slash commands are unavailable while a turn is running"
		return a, nil
	}
	a.remember(text)
	a.queuedInputs = append(a.queuedInputs, text)
	a.input.SetValue("")
	a.resizeInput()
	a.status = "Message queued"
	return a, nil
}

func (a App) steerInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return a, nil
	}
	if strings.HasPrefix(text, "/") {
		a.err = "Slash commands are unavailable while a turn is running"
		return a, nil
	}
	a.remember(text)
	a.queuedInputs = append([]string{text}, a.queuedInputs...)
	a.input.SetValue("")
	a.resizeInput()
	a.cancelRun(false)
	a.status = "Steering current turn"
	return a, nil
}

func (a *App) remember(text string) {
	if len(a.history) == 0 || a.history[len(a.history)-1] != text {
		a.history = append(a.history, text)
	}
	a.historyIdx = len(a.history)
	a.historyDraft = ""
}

func (a App) runSlashCommand(text string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(text)
	command := parts[0]
	a.input.SetValue("")
	a.resizeInput()
	switch command {
	case "/instructions":
		a.err = ""
		a.status = formatInstructions(a.info.InstructionPaths)
	case "/permissions":
		a.handlePermissions(parts)
	case "/clear", "/reset":
		a.handleReset()
	case "/sessions":
		a.handleSessions()
	case "/resume":
		a.handleResume(parts)
	case "/rename":
		a.handleRename(text, parts)
	case "/delete-session":
		a.handleDelete(parts)
	case "/compact":
		return a.handleCompact(text, command)
	case "/undo":
		a.handleUndo()
	case "/quit", "/exit":
		return a, tea.Quit
	case "/help":
		a.showHelp = true
	default:
		a.err = "Unknown command: " + command
	}
	return a, nil
}

func (a *App) handlePermissions(parts []string) {
	a.err = ""
	if len(parts) >= 3 && parts[1] == "mode" {
		mode := parts[2]
		if err := a.session.SetPermissionMode(mode); err != nil {
			a.err = "Permissions failed: " + err.Error()
			return
		}
		// Report the runtime's view of the policy instead of deriving it
		// locally, so the display cannot drift from actual behavior.
		a.info.PermissionMode = a.session.PermissionMode()
		a.info.AutoApprove = a.session.AutoApproveTools()
	}
	a.status = formatPermissions(a.info)
}

func (a *App) handleReset() {
	if err := a.session.Reset(); err != nil {
		a.err = "Reset failed: " + err.Error()
	} else {
		a.err = ""
	}
	a.refreshSnapshot()
	a.lastActivity = "Conversation reset"
}

func (a *App) handleSessions() {
	summaries, err := a.session.Sessions()
	if err != nil {
		a.err = "Sessions failed: " + err.Error()
		return
	}
	a.err = ""
	a.status = formatSessions(summaries)
}

func (a *App) handleResume(parts []string) {
	if len(parts) < 2 {
		a.err = "Usage: /resume <id>"
		return
	}
	if err := a.session.Resume(parts[1]); err != nil {
		a.err = "Resume failed: " + err.Error()
		return
	}
	a.err = ""
	a.status = "Resumed " + parts[1]
	a.refreshSnapshot()
}

func (a *App) handleRename(text string, parts []string) {
	if len(parts) < 3 {
		a.err = "Usage: /rename <id> <title>"
		return
	}
	title := strings.TrimSpace(strings.TrimPrefix(text, parts[0]+" "+parts[1]))
	if err := a.session.Rename(parts[1], title); err != nil {
		a.err = "Rename failed: " + err.Error()
		return
	}
	a.err = ""
	a.status = "Renamed " + parts[1]
}

func (a *App) handleDelete(parts []string) {
	if len(parts) < 2 {
		a.err = "Usage: /delete-session <id>"
		return
	}
	if err := a.session.DeleteSession(parts[1]); err != nil {
		a.err = "Delete failed: " + err.Error()
		return
	}
	a.err = ""
	a.status = "Deleted " + parts[1]
}

// handleCompact runs outside the update loop: with no summary it triggers
// a full model call that must not freeze the UI. The keep-newest policy
// stays in the runtime; the TUI only passes the optional summary.
func (a App) handleCompact(text, command string) (tea.Model, tea.Cmd) {
	summary := strings.TrimSpace(strings.TrimPrefix(text, command))
	a.compacting = true
	a.status = "Compacting conversation…"
	run := func() tea.Msg { return compactDoneMsg{err: a.session.Compact(context.Background(), summary)} }
	return a, run
}

func (a *App) handleUndo() {
	if err := a.session.Undo(); err != nil {
		a.err = "Undo failed: " + err.Error()
		return
	}
	a.err = ""
	a.status = "Restored last checkpoint"
	a.refreshSnapshot()
}

func (a App) startTurn(text string) (tea.Model, tea.Cmd) {
	a.err = ""
	a.status = ""
	a.lastActivity = text
	a.streamingMessage = nil
	a.agentStatus = AgentStatus{Label: "Submitting", Busy: true}
	a.stateHistory = nil
	a.pendingTool = nil
	a.pendingRequest = PermissionRequest{}
	a.input.SetValue("")
	a.resizeInput()
	a.eventsCh = make(chan Event, 100)
	a.approvalsCh = make(chan ApprovalDecision, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.turn++
	a.viewport.SetContent(a.contentString())
	a.viewport.GotoBottom()
	run := func() tea.Msg { return submitDoneMsg{err: a.session.RunTurn(ctx, text, a.eventsCh, a.approvalsCh)} }
	return a, tea.Batch(waitForEvent(a.eventsCh, a.turn), run)
}

func (a App) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.approvalSubmitted {
		return a, nil
	}
	var decision ApprovalDecision
	switch strings.ToLower(msg.String()) {
	case "up", "k":
		if a.approvalSelection > 0 {
			a.approvalSelection--
		}
		return a, nil
	case "down", "j":
		if a.approvalSelection < 2 {
			a.approvalSelection++
		}
		return a, nil
	case "enter":
		decision = []ApprovalDecision{ApproveOnce, ApproveAlways, DenyApproval}[a.approvalSelection]
	case "1", "y":
		decision = ApproveOnce
	case "2", "a":
		decision = ApproveAlways
	case "3", "n":
		decision = DenyApproval
	default:
		return a, nil
	}
	a.streamingMessage = nil
	a.approvalSubmitted = true
	a.approvalsCh <- decision
	return a, nil
}
