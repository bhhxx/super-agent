package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type TUIInfo struct {
	Provider         string
	ModelName        string
	AutoApprove      bool
	PermissionMode   string
	NoTools          bool
	CWD              string
	InstructionPaths []string
}

type App struct {
	session           Conversation
	input             textarea.Model
	viewport          viewport.Model
	spinner           spinner.Model
	styles            Styles
	info              TUIInfo
	history           []string
	historyIdx        int
	historyDraft      string
	queuedInputs      []string
	slashSelection    int
	ready             bool
	width             int
	height            int
	showHelp          bool
	err               string
	status            string
	lastActivity      string
	cancel            context.CancelFunc
	eventsCh          chan Event
	approvalsCh       chan ApprovalDecision
	agentStatus       AgentStatus
	stateHistory      []string
	messages          []Message
	pendingTool       *ToolCall
	pendingRequest    PermissionRequest
	pendingToolIndex  int
	pendingToolTotal  int
	approvalSelection int
	approvalSubmitted bool
	streamingMessage  *Message
	turn              int
	compacting        bool
}

type submitDoneMsg struct {
	err error
}

type compactDoneMsg struct {
	err error
}

type sessionEventMsg struct {
	event Event
	turn  int
}

// waitForEvent listens on ch and tags the delivered event with turn so
// that events left over from a replaced turn channel can be discarded.
func waitForEvent(ch <-chan Event, turn int) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return sessionEventMsg{event: event, turn: turn}
	}
}

func New(session Conversation, info TUIInfo) App {
	styles := DefaultStyles()

	input := textarea.New()
	input.Placeholder = "Ask me anything... (try /help)"
	input.Focus()
	input.CharLimit = 2000
	input.ShowLineNumbers = false
	input.SetPromptFunc(3, func(line int) string {
		if line == 0 {
			return " ◇ "
		}
		return "   "
	})
	input.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	input.SetHeight(1)

	s := spinner.New()
	s.Spinner = spinner.Pulse
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	return App{
		session:     session,
		input:       input,
		spinner:     s,
		styles:      styles,
		info:        info,
		history:     []string{},
		eventsCh:    make(chan Event, 100),
		approvalsCh: make(chan ApprovalDecision, 1),
		agentStatus: AgentStatus{Label: "Idle"},
	}
}

func (a App) Init() tea.Cmd {
	// The initial events channel has no producer; arming a listener on it
	// would leak a goroutine for the app's lifetime. startTurn arms the
	// first listener on the real turn channel.
	return tea.Batch(
		a.input.Cursor.BlinkCmd(),
		a.spinner.Tick,
	)
}

func (a App) headerView() string {
	var icon string
	switch {
	case a.isBusy():
		icon = a.spinner.View()
	case a.needsInput():
		icon = "✋"
	default:
		icon = "◇"
	}

	title := a.styles.Header.Render(" SUPER AGENT ")
	status := a.styles.Status.Render(fmt.Sprintf(" %s %s", icon, a.agentStatus.Label))

	header := title + status
	version := a.styles.Version.Width(max(0, a.width-lipgloss.Width(header))).Render("v0.1.0")
	return header + version + "\n" + a.infoBar() + "\n"
}

func (a App) infoBar() string {
	modelStr := a.info.Provider + "/" + a.info.ModelName
	cwdStr := a.info.CWD
	if home, err := os.UserHomeDir(); err == nil {
		cwdStr = strings.Replace(cwdStr, home, "~", 1)
	}
	toolsStr := "tools:on"
	if a.info.NoTools {
		toolsStr = "tools:off"
	}
	approveStr := "mode:" + a.info.PermissionMode
	if approveStr == "mode:" {
		approveStr = "mode:ask"
	}
	sep := a.styles.Footer.Render(" │ ")
	return a.styles.Footer.Render(modelStr) + sep +
		a.styles.Footer.Render(cwdStr) + sep +
		a.styles.Footer.Render(toolsStr) + sep +
		a.styles.Footer.Render(approveStr)
}

func (a App) isBusy() bool {
	return a.compacting || a.agentStatus.Busy
}

func (a App) needsInput() bool {
	return a.agentStatus.AwaitingApproval
}

func (a App) welcomeString() string {
	var b strings.Builder
	b.WriteString("# Welcome to Super Agent\n\n")
	b.WriteString("An AI coding assistant. Type a message below to get started.\n\n")
	toolsLabel := "enabled"
	if a.info.NoTools {
		toolsLabel = "disabled"
	}
	approveLabel := "manual"
	if a.info.AutoApprove {
		approveLabel = "auto"
	}
	b.WriteString(fmt.Sprintf("**Model:** %s/%s  ", a.info.Provider, a.info.ModelName))
	b.WriteString(fmt.Sprintf("**Tools:** %s  **Approval:** %s  **Mode:** %s\n\n", toolsLabel, approveLabel, firstNonEmpty(a.info.PermissionMode, "ask")))
	b.WriteString("**Commands:** `/help` `/instructions` `/permissions` `/clear` `/sessions` `/resume <id>` `/compact` `/undo` `/quit`\n")
	return a.renderMarkdown(b.String())
}

func (a App) footerView() string {
	var b strings.Builder

	if a.err != "" {
		b.WriteString(a.styles.Error.Render(" !! error: "+a.err) + "\n")
	}

	if a.status != "" {
		b.WriteString(a.styles.Status.Render(" "+a.status) + "\n")
	}

	compact := a.height > 0 && a.height < 18
	if len(a.stateHistory) > 0 && !compact {
		b.WriteString(a.styles.Footer.Render(" states: "+stateHistoryString(a.stateHistory)) + "\n")
	}

	if a.lastActivity != "" && !compact {
		b.WriteString(a.styles.Footer.Render(" Last: "+a.lastActivity) + "\n")
	}

	if len(a.queuedInputs) > 0 {
		b.WriteString(a.styles.Status.Render(fmt.Sprintf(" Queued (%d)", len(a.queuedInputs))) + "\n")
		limit := min(3, len(a.queuedInputs))
		if compact {
			limit = 0
		}
		for index := 0; index < limit; index++ {
			preview := strings.Join(strings.Fields(a.queuedInputs[index]), " ")
			if len([]rune(preview)) > 72 {
				preview = string([]rune(preview)[:72]) + "…"
			}
			b.WriteString(a.styles.Footer.Render(fmt.Sprintf(" %d. %s", index+1, preview)) + "\n")
		}
		if remaining := len(a.queuedInputs) - limit; remaining > 0 {
			b.WriteString(a.styles.Footer.Render(fmt.Sprintf(" … %d more", remaining)) + "\n")
		}
	}

	if call := a.pendingTool; call != nil {
		prompt := lipgloss.NewStyle().
			Background(lipgloss.Color("3")).
			Foreground(lipgloss.Color("0")).
			Padding(0, 1).
			Render(" ACTION REQUIRED ")
		progress := ""
		if a.pendingToolTotal > 0 {
			progress = fmt.Sprintf(" tool %d/%d:", a.pendingToolIndex, a.pendingToolTotal)
		}
		b.WriteString(prompt + progress + " approve " + lipgloss.NewStyle().Bold(true).Render(call.Name) + "?\n")
		options := []string{"1. Yes, run once", "2. Yes, always allow", "3. No, deny"}
		for index, option := range options {
			prefix := "  "
			style := a.styles.Footer
			if index == a.approvalSelection {
				prefix = "› "
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
			}
			b.WriteString(style.Render(prefix+option) + "\n")
		}
		if a.approvalSubmitted {
			b.WriteString(a.styles.Status.Render(" Decision submitted…") + "\n")
		}
		if a.pendingRequest.ToolName != "" || a.pendingRequest.Reason != "" {
			meta := fmt.Sprintf(" class: %s cwd: %s", a.pendingRequest.CommandClass, firstNonEmpty(a.pendingRequest.CWD, a.info.CWD))
			if len(a.pendingRequest.TouchedPaths) > 0 {
				meta += " paths: " + strings.Join(a.pendingRequest.TouchedPaths, ",")
			}
			if a.pendingRequest.Reason != "" {
				meta += " reason: " + a.pendingRequest.Reason
			}
			b.WriteString(a.styles.Footer.Render(meta) + "\n")
		} else if call.Input != "" {
			input := call.Input
			if len(input) > 240 {
				input = input[:240] + "..."
			}
			b.WriteString(a.styles.Footer.Render(" cwd: "+a.info.CWD+" input: "+input) + "\n")
		}
	}

	if matches := a.slashMatches(); len(matches) > 0 && a.cancel == nil && a.pendingTool == nil {
		visible := 6
		if compact {
			visible = 3
		}
		start := 0
		if a.slashSelection >= visible {
			start = a.slashSelection - visible + 1
		}
		end := min(start+visible, len(matches))
		for index := start; index < end; index++ {
			prefix := "  "
			style := a.styles.Footer
			if index == a.slashSelection {
				prefix = "› "
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
			}
			label := matches[index]
			if !compact {
				label = fmt.Sprintf("%-17s %s", label, slashCommandDescriptions[matches[index]])
			}
			b.WriteString(style.Render(prefix+label) + "\n")
		}
	}

	count := fmt.Sprintf("%d/%d", a.input.Length(), a.input.CharLimit)
	scrollPercent := "  0%"
	if a.ready {
		scrollPercent = fmt.Sprintf("%3.f%%", a.viewport.ScrollPercent()*100)
	}

	stats := a.styles.Footer.Render(fmt.Sprintf("%s  •  %s", count, scrollPercent))
	inputView := a.styles.InputFocused.Width(max(1, a.width-2)).Render(a.input.View())
	b.WriteString(inputView + "\n")

	shortcut := "enter: send • ctrl+j: newline • ?: help • esc: clear"
	if a.pendingTool != nil {
		shortcut = "↑↓: select • enter: confirm • 1/y once • 2/a always • 3/n deny"
	} else if a.cancel != nil {
		shortcut = "enter: steer • tab: queue • ctrl+j: newline • esc: cancel"
	}
	if len(a.queuedInputs) > 0 {
		shortcut += fmt.Sprintf(" • queued: %d", len(a.queuedInputs))
	}
	footerText := a.styles.Footer.Render(" " + shortcut)
	padding := a.width - lipgloss.Width(footerText) - lipgloss.Width(stats)
	if padding < 0 {
		padding = 0
	}
	b.WriteString(footerText + strings.Repeat(" ", padding) + stats)

	return b.String()
}

func (a *App) refreshSnapshot() {
	snapshot := a.session.Snapshot()
	a.agentStatus = snapshot.AgentStatus
	a.recordState(a.agentStatus.Label)
	a.messages = append([]Message(nil), snapshot.Messages...)
	a.pendingTool = snapshot.PendingTool
	if snapshot.PendingPermission != nil {
		a.pendingRequest = *snapshot.PendingPermission
	} else {
		a.pendingRequest = PermissionRequest{}
	}
	a.streamingMessage = snapshot.StreamingMessage
	// Match the event path: the runtime advances the batch index before
	// the pending tool is set, so the value is already one-based.
	a.pendingToolIndex = snapshot.PendingToolBatchIndex
	a.pendingToolTotal = snapshot.PendingToolBatchTotal
	a.viewport.SetContent(a.contentString())
	a.viewport.GotoBottom()
}

// recordState appends the state to the footer history, collapsing
// consecutive repeats so duplicated snapshot emissions do not spam the
// line. The history resets when a new turn starts.
func (a *App) recordState(state string) {
	if len(a.stateHistory) > 0 && a.stateHistory[len(a.stateHistory)-1] == state {
		return
	}
	a.stateHistory = append(a.stateHistory, state)
	if len(a.stateHistory) > 8 {
		a.stateHistory = append([]string(nil), a.stateHistory[len(a.stateHistory)-8:]...)
	}
}

func stateHistoryString(history []string) string {
	return strings.Join(history, " → ")
}

func (a App) renderMarkdown(content string) string {
	if a.styles.MarkdownRenderer == nil {
		return content
	}
	out, err := a.styles.MarkdownRenderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSpace(out)
}

func (a App) renderToolCall(tc *ToolCall) string {
	header := a.styles.ToolLabel.Render("  " + tc.Name)
	input := tc.Input
	if len(input) > 200 {
		input = input[:200] + "..."
	}
	body := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  " + input)
	return a.styles.ToolCard.Render(header + "\n" + body)
}

func (a App) contentString() string {
	var b strings.Builder

	width := a.viewport.Width - 4
	if width <= 0 {
		width = 80
	}
	wrapStyle := lipgloss.NewStyle().Width(width).Padding(0, 1)

	messages := a.messages

	if len(messages) == 0 && !a.isBusy() {
		return a.welcomeString()
	}

	for _, msg := range messages {
		var msgBlock strings.Builder

		role := strings.ToUpper(string(msg.Role))
		switch msg.Role {
		case "user":
			msgBlock.WriteString(a.styles.UserLabel.Render(role) + "\n")
		case "assistant":
			msgBlock.WriteString(a.styles.AgentLabel.Render(role) + "\n")
		case "tool":
			msgBlock.WriteString(a.styles.ToolLabel.Render(fmt.Sprintf("%s (%s)", role, msg.ToolName)) + "\n")
		default:
			msgBlock.WriteString(lipgloss.NewStyle().Bold(true).Render(role) + "\n")
		}

		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				msgBlock.WriteString(a.renderToolCall(tc) + "\n")
			}
		}

		if msg.Role == "tool" {
			content := msg.Content
			if len(content) > 1000 {
				content = content[:1000] + "... (truncated)"
			}
			resultHeader := a.styles.ToolLabel.Render("  " + msg.ToolName + " result")
			resultBody := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(content)
			msgBlock.WriteString(a.styles.ToolCard.Render(resultHeader+"\n"+resultBody) + "\n")
		} else {
			if msg.ReasoningContent != "" {
				msgBlock.WriteString(a.styles.Thinking.Render(msg.ReasoningContent) + "\n\n")
			}
			if msg.Content != "" {
				if msg.Role == "assistant" {
					msgBlock.WriteString(a.renderMarkdown(msg.Content))
				} else {
					msgBlock.WriteString(msg.Content)
				}
				msgBlock.WriteString("\n")
			}
		}

		b.WriteString(wrapStyle.Render(msgBlock.String()) + "\n\n")
	}

	if a.isBusy() && a.streamingMessage != nil {
		var streamBlock strings.Builder
		streamBlock.WriteString(a.styles.AgentLabel.Render("AGENT") + "\n")

		if a.streamingMessage.ReasoningContent != "" {
			streamBlock.WriteString(a.styles.Thinking.Render(a.streamingMessage.ReasoningContent))
		}
		if a.streamingMessage.Content != "" {
			if a.streamingMessage.ReasoningContent != "" {
				streamBlock.WriteString("\n\n")
			}
			streamBlock.WriteString(a.streamingMessage.Content)
		}

		if a.streamingMessage.ReasoningContent != "" || a.streamingMessage.Content != "" {
			b.WriteString(wrapStyle.Render(streamBlock.String()) + "\n")
		}
	}

	return b.String()
}
