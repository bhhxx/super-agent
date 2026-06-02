package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"super-agent/runtime"
)

// Styles defines the UI theme and layout constraints
type Styles struct {
	Header           lipgloss.Style
	Status           lipgloss.Style
	UserLabel        lipgloss.Style
	AgentLabel       lipgloss.Style
	ToolLabel        lipgloss.Style
	CommandLabel     lipgloss.Style
	Thinking         lipgloss.Style
	Error            lipgloss.Style
	Footer           lipgloss.Style
	Version          lipgloss.Style
	ViewportBorder   lipgloss.Style
	InputFocused     lipgloss.Style
	ToolCard         lipgloss.Style
	MarkdownRenderer *glamour.TermRenderer
}

func DefaultStyles() Styles {
	s := Styles{}

	primary := lipgloss.Color("4")
	secondary := lipgloss.Color("8")
	accent := lipgloss.Color("6")
	errorCol := lipgloss.Color("1")
	userCol := lipgloss.Color("2")
	agentCol := lipgloss.Color("5")

	s.Header = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(primary).
		Bold(true).
		Padding(0, 1)

	s.Status = lipgloss.NewStyle().Foreground(accent).Italic(true)
	s.UserLabel = lipgloss.NewStyle().Foreground(userCol).Bold(true)
	s.AgentLabel = lipgloss.NewStyle().Foreground(agentCol).Bold(true)
	s.ToolLabel = lipgloss.NewStyle().Foreground(accent).Bold(true)
	s.CommandLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	s.Thinking = lipgloss.NewStyle().Foreground(secondary).Italic(true)
	s.Error = lipgloss.NewStyle().Foreground(errorCol).Bold(true)
	s.Footer = lipgloss.NewStyle().Foreground(secondary).Italic(true)
	s.Version = lipgloss.NewStyle().Foreground(secondary).Align(lipgloss.Right)

	s.ViewportBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondary)

	s.InputFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1)

	s.ToolCard = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondary).
		Padding(0, 1)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0),
	)
	s.MarkdownRenderer = renderer

	return s
}

type TUIInfo struct {
	Provider       string
	ModelName      string
	AutoApprove    bool
	PermissionMode string
	SandboxBackend string
	OpenSandboxID  string
	OpenSandboxCLI string
	OpenSandboxCWD string
	NoTools        bool
	CWD            string
	MemoryPaths    []string
}

type App struct {
	session          Conversation
	input            textinput.Model
	viewport         viewport.Model
	spinner          spinner.Model
	styles           Styles
	info             TUIInfo
	history          []string
	historyIdx       int
	ready            bool
	width            int
	height           int
	showHelp         bool
	err              string
	status           string
	lastActivity     string
	cancel           context.CancelFunc
	eventsCh         chan runtime.SessionEvent
	approvalsCh      chan runtime.ApprovalDecision
	state            runtime.State
	messages         []runtime.Message
	pendingTool      *runtime.ToolCall
	pendingRequest   runtime.PermissionRequest
	pendingToolIndex int
	pendingToolTotal int
	streamingMessage *runtime.Message
}

type Conversation interface {
	Snapshot() runtime.Snapshot
	RunTurn(context.Context, string, chan<- runtime.SessionEvent, <-chan runtime.ApprovalDecision) error
	Cancel() error
	Reset() error
	Sessions() ([]runtime.SessionSummary, error)
	Resume(runtime.SessionID) error
	Rename(runtime.SessionID, string) error
	DeleteSession(runtime.SessionID) error
	Compact(context.Context, string, int) error
	Undo() error
	SetPermissionMode(runtime.PermissionMode) error
}

type submitDoneMsg struct {
	err error
}

type sessionEventMsg struct {
	event runtime.SessionEvent
}

func waitForEvent(ch <-chan runtime.SessionEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return sessionEventMsg{event: event}
	}
}

func New(session Conversation, info TUIInfo) App {
	styles := DefaultStyles()

	input := textinput.New()
	input.Placeholder = "Ask me anything... (try /help)"
	input.Focus()
	input.CharLimit = 2000
	input.Prompt = " ◇ "
	input.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

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
		eventsCh:    make(chan runtime.SessionEvent, 100),
		approvalsCh: make(chan runtime.ApprovalDecision, 1),
		state:       runtime.StateIdle,
	}
}

func (a App) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		a.spinner.Tick,
		waitForEvent(a.eventsCh),
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
	status := a.styles.Status.Render(fmt.Sprintf(" %s %s", icon, friendlyState(a.state)))

	header := title + status
	version := a.styles.Version.Width(a.width - lipgloss.Width(header)).Render("v0.1.0")
	return header + version + "\n" + a.infoBar() + "\n"
}

func friendlyState(s runtime.State) string {
	switch s {
	case runtime.StateInitializing:
		return "Initializing"
	case runtime.StateIdle:
		return "Ready"
	case runtime.StateWaitingLLM:
		return "Thinking"
	case runtime.StateRunningTool:
		return "Running tool"
	case runtime.StateWaitingApproval:
		return "Waiting approval"
	case runtime.StateAdvancingQueue:
		return "Streaming"
	default:
		return string(s)
	}
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
	return a.state == runtime.StateWaitingLLM || a.state == runtime.StateRunningTool || a.state == runtime.StateAdvancingQueue
}

func (a App) needsInput() bool {
	return a.state == runtime.StateWaitingApproval
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
	b.WriteString("**Commands:** `/help` `/memory` `/permissions` `/clear` `/sessions` `/resume <id>` `/compact` `/undo` `/quit`\n")
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

	if a.lastActivity != "" {
		b.WriteString(a.styles.Footer.Render(" Last: "+a.lastActivity) + "\n")
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
		b.WriteString(prompt + progress + " approve " + lipgloss.NewStyle().Bold(true).Render(call.Name) + "? [y/a/n]\n")
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

	count := fmt.Sprintf("%d/%d", len(a.input.Value()), a.input.CharLimit)
	scrollPercent := "  0%"
	if a.ready {
		scrollPercent = fmt.Sprintf("%3.f%%", a.viewport.ScrollPercent()*100)
	}

	stats := a.styles.Footer.Render(fmt.Sprintf("%s  •  %s", count, scrollPercent))
	inputView := a.styles.InputFocused.Width(a.width - 2).Render(a.input.View())
	b.WriteString(inputView + "\n")

	footerText := a.styles.Footer.Render(" ?: help • esc: quit • ↑↓ history • ctrl+l clear")
	padding := a.width - lipgloss.Width(footerText) - lipgloss.Width(stats)
	if padding < 0 {
		padding = 0
	}
	b.WriteString(footerText + strings.Repeat(" ", padding) + stats)

	return b.String()
}

func (a *App) refreshSnapshot() {
	snapshot := a.session.Snapshot()
	a.state = snapshot.State
	a.messages = append([]runtime.Message(nil), snapshot.Messages...)
	a.pendingTool = snapshot.PendingTool
	if snapshot.PendingPermission != nil {
		a.pendingRequest = *snapshot.PendingPermission
	} else {
		a.pendingRequest = runtime.PermissionRequest{}
	}
	a.streamingMessage = snapshot.StreamingMessage
	a.pendingToolIndex = snapshot.PendingToolBatchIndex + 1
	a.pendingToolTotal = snapshot.PendingToolBatchTotal
	a.viewport.SetContent(a.contentString())
	a.viewport.GotoBottom()
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

func (a App) renderToolCall(tc *runtime.ToolCall) string {
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

func ExtractCodeBlocks(content string) []string {
	var blocks []string
	lines := strings.Split(content, "\n")
	inBlock := false
	var currentBlock strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inBlock {
				blocks = append(blocks, strings.TrimSuffix(currentBlock.String(), "\n"))
				currentBlock.Reset()
				inBlock = false
			} else {
				inBlock = true
			}
			continue
		}
		if inBlock {
			currentBlock.WriteString(line + "\n")
		}
	}
	return blocks
}

func (a *App) copyLastCodeBlock() tea.Cmd {
	messages := a.messages
	for i := len(messages) - 1; i >= 0; i-- {
		blocks := ExtractCodeBlocks(messages[i].Content)
		if len(blocks) > 0 {
			code := blocks[len(blocks)-1]
			err := clipboard.WriteAll(code)
			if err != nil {
				a.err = "Failed to copy: " + err.Error()
				a.status = ""
			} else {
				a.status = "Copied code block to clipboard!"
				a.err = ""
			}
			return nil
		}
	}
	a.err = "No code blocks found to copy"
	a.status = ""
	return nil
}

func (a *App) cancelRun() {
	if a.pendingTool != nil {
		if err := a.session.Cancel(); err != nil {
			a.err = err.Error()
		}
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd tea.Cmd
	)

	switch msg := msg.(type) {
	case spinner.TickMsg:
		a.spinner, cmd = a.spinner.Update(msg)
		return a, cmd

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		headerHeight := lipgloss.Height(a.headerView())
		footerHeight := lipgloss.Height(a.footerView())

		if !a.ready {
			a.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			a.viewport.Style = a.styles.ViewportBorder
			a.ready = true
		} else {
			a.viewport.Width = msg.Width
			a.viewport.Height = msg.Height - headerHeight - footerHeight
		}
		a.input.Width = msg.Width - 8

		a.viewport.SetContent(a.contentString())
		a.viewport.GotoBottom()
		return a, nil

	case tea.MouseMsg:
		a.viewport, cmd = a.viewport.Update(msg)
		return a, cmd

	case tea.KeyMsg:
		if a.showHelp {
			if msg.String() == "?" || msg.String() == "esc" {
				a.showHelp = false
			}
			return a, nil
		}

		switch msg.String() {
		case "ctrl+c":
			if a.pendingTool != nil || a.cancel != nil {
				a.cancelRun()
				return a, nil
			}
			return a, tea.Quit
		case "ctrl+l":
			a.viewport.SetContent("")
			a.err = ""
			a.status = ""
			a.lastActivity = "Viewport cleared"
			return a, nil
		case "ctrl+y":
			return a, a.copyLastCodeBlock()
		case "esc":
			if a.pendingTool != nil || a.cancel != nil {
				a.cancelRun()
				return a, nil
			}
			return a, tea.Quit
		case "?":
			if a.input.Value() == "" {
				a.showHelp = true
				a.status = ""
				return a, nil
			}
		case "up":
			a.status = ""
			if a.historyIdx > 0 {
				a.historyIdx--
				a.input.SetValue(a.history[a.historyIdx])
				a.input.CursorEnd()
				return a, nil
			}
		case "down":
			if a.historyIdx < len(a.history)-1 {
				a.historyIdx++
				a.input.SetValue(a.history[a.historyIdx])
				a.input.CursorEnd()
				return a, nil
			} else if a.historyIdx == len(a.history)-1 {
				a.historyIdx = len(a.history)
				a.input.SetValue("")
				return a, nil
			}
		case "pgup", "pgdown":
			a.viewport, cmd = a.viewport.Update(msg)
			return a, cmd
		case "enter":
			a.status = ""
			if a.pendingTool == nil && a.cancel == nil {
				return a.submit()
			}
		}

		if a.pendingTool != nil {
			a.status = ""
			return a.handleApprovalKey(msg)
		}

	case sessionEventMsg:
		switch event := msg.event.(type) {
		case runtime.StateChanged:
			a.state = event.State
			if !a.needsInput() {
				a.pendingTool = nil
				a.pendingRequest = runtime.PermissionRequest{}
				a.pendingToolIndex = 0
				a.pendingToolTotal = 0
			}
		case runtime.ToolApprovalRequested:
			call := event.ToolCall
			a.pendingTool = &call
			a.pendingRequest = event.Request
			a.pendingToolIndex = event.BatchIndex
			a.pendingToolTotal = event.BatchTotal
		case runtime.ToolApprovalCleared:
			a.pendingTool = nil
			a.pendingRequest = runtime.PermissionRequest{}
			a.pendingToolIndex = 0
			a.pendingToolTotal = 0
		case runtime.MessageAppended:
			a.messages = append(a.messages, event.Message)
			if event.Message.Role == runtime.RoleAssistant {
				a.streamingMessage = nil
			}
		case runtime.SessionError:
			if !errors.Is(event.Err, context.Canceled) {
				a.err = event.Err.Error()
			}
		case runtime.StreamChunkReceived:
			a.streamingMessage = event.Message
		}
		a.viewport.SetContent(a.contentString())
		if a.viewport.AtBottom() {
			a.viewport.GotoBottom()
		}
		return a, waitForEvent(a.eventsCh)

	case submitDoneMsg:
		a.cancel = nil
		if msg.err != nil {
			if !errors.Is(msg.err, context.Canceled) {
				a.err = msg.err.Error()
			}
		}
		a.viewport.SetContent(a.contentString())
		if a.viewport.AtBottom() {
			a.viewport.GotoBottom()
		}
		return a, nil
	}

	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a App) helpView() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(1, 2).
		Width(45)

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render("Commands & Shortcuts")

	items := []string{
		a.styles.CommandLabel.Render("/clear") + "    Reset conversation",
		a.styles.CommandLabel.Render("/sessions") + " List saved sessions",
		a.styles.CommandLabel.Render("/resume") + "   Resume saved session",
		a.styles.CommandLabel.Render("/compact") + "  Compact context",
		a.styles.CommandLabel.Render("/undo") + "     Restore checkpoint",
		a.styles.CommandLabel.Render("/permissions") + " Show permission policy",
		a.styles.CommandLabel.Render("/help") + "     Show this menu",
		a.styles.CommandLabel.Render("/quit") + "     Exit application",
		"",
		"enter        Submit message",
		"up/down      Cycle history",
		"pgup/pgdn    Scroll history",
		"ctrl+l       Clear viewport",
		"ctrl+y       Copy last code block",
		"ctrl+c/esc   Quit / Cancel",
		"?            Toggle help",
		"",
		"Tool Approval:",
		"y            Approve once",
		"a            Always allow",
		"n            Deny call",
	}

	content := title + "\n\n" + strings.Join(items, "\n")
	return helpStyle.Render(content)
}

func formatSessions(summaries []runtime.SessionSummary) string {
	if len(summaries) == 0 {
		return "No saved sessions"
	}
	var lines []string
	for _, summary := range summaries {
		lines = append(lines, fmt.Sprintf("%s  %s  %s/%s", summary.ID, summary.Title, summary.Provider, summary.Model))
	}
	return strings.Join(lines, "\n")
}

func formatMemory(paths []string) string {
	if len(paths) == 0 {
		return "No instruction or memory files loaded"
	}
	var b strings.Builder
	b.WriteString("Loaded memory sources:\n")
	for _, path := range paths {
		b.WriteString("- ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func formatPermissions(info TUIInfo) string {
	mode := firstNonEmpty(info.PermissionMode, "ask")
	backend := firstNonEmpty(info.SandboxBackend, "local")
	sandbox := "Sandbox: local command execution"
	if backend == "opensandbox" && info.OpenSandboxID != "" {
		sandbox = fmt.Sprintf("Sandbox: opensandbox id=%s cli=%s cwd=%s", info.OpenSandboxID, firstNonEmpty(info.OpenSandboxCLI, "osb"), firstNonEmpty(info.OpenSandboxCWD, "/workspace"))
	} else if backend == "opensandbox" {
		sandbox = "Sandbox: opensandbox configured but opensandbox_id is missing"
	}
	return fmt.Sprintf(
		"Permission mode: %s\nTools: %s\nApproval: %s\nCWD: %s\n%s\nEnable OpenSandbox in ~/.superagent/settings.json with sandbox.backend=opensandbox and sandbox.opensandbox_id=<id>",
		mode,
		onOff(!info.NoTools),
		onOff(info.AutoApprove),
		info.CWD,
		sandbox,
	)
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a App) View() string {
	if !a.ready {
		return "\n  Initializing..."
	}

	header := a.headerView()
	footer := a.footerView()

	a.viewport.Height = a.height - lipgloss.Height(header) - lipgloss.Height(footer)

	mainView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		a.viewport.View(),
		footer,
	)

	if a.showHelp {
		help := a.helpView()
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, help, lipgloss.WithWhitespaceChars(" "), lipgloss.WithWhitespaceForeground(lipgloss.Color("8")))
	}

	return mainView
}

func (a App) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return a, nil
	}

	if len(a.history) == 0 || a.history[len(a.history)-1] != text {
		a.history = append(a.history, text)
	}
	a.historyIdx = len(a.history)

	if strings.HasPrefix(text, "/") {
		parts := strings.Fields(text)
		cmd := parts[0]
		switch cmd {
		case "/memory":
			a.err = ""
			a.status = formatMemory(a.info.MemoryPaths)
			a.input.SetValue("")
			return a, nil
		case "/permissions":
			a.err = ""
			if len(parts) >= 3 && parts[1] == "mode" {
				mode := runtime.PermissionMode(parts[2])
				if err := a.session.SetPermissionMode(mode); err != nil {
					a.err = "Permissions failed: " + err.Error()
				} else {
					a.info.PermissionMode = string(mode)
					a.info.AutoApprove = mode == runtime.PermissionModeBypass
					a.status = formatPermissions(a.info)
				}
			} else {
				a.status = formatPermissions(a.info)
			}
			a.input.SetValue("")
			return a, nil
		case "/clear", "/reset":
			if err := a.session.Reset(); err != nil {
				a.err = "Reset failed: " + err.Error()
			} else {
				a.err = ""
			}
			a.refreshSnapshot()
			a.lastActivity = "Conversation reset"
			a.input.SetValue("")
			return a, nil
		case "/sessions":
			summaries, err := a.session.Sessions()
			if err != nil {
				a.err = "Sessions failed: " + err.Error()
			} else {
				a.err = ""
				a.status = formatSessions(summaries)
			}
			a.input.SetValue("")
			return a, nil
		case "/resume":
			if len(parts) < 2 {
				a.err = "Usage: /resume <id>"
			} else if err := a.session.Resume(runtime.SessionID(parts[1])); err != nil {
				a.err = "Resume failed: " + err.Error()
			} else {
				a.err = ""
				a.status = "Resumed " + parts[1]
				a.refreshSnapshot()
			}
			a.input.SetValue("")
			return a, nil
		case "/rename":
			if len(parts) < 3 {
				a.err = "Usage: /rename <id> <title>"
			} else if err := a.session.Rename(runtime.SessionID(parts[1]), strings.TrimSpace(strings.TrimPrefix(text, parts[0]+" "+parts[1]))); err != nil {
				a.err = "Rename failed: " + err.Error()
			} else {
				a.err = ""
				a.status = "Renamed " + parts[1]
			}
			a.input.SetValue("")
			return a, nil
		case "/delete-session":
			if len(parts) < 2 {
				a.err = "Usage: /delete-session <id>"
			} else if err := a.session.DeleteSession(runtime.SessionID(parts[1])); err != nil {
				a.err = "Delete failed: " + err.Error()
			} else {
				a.err = ""
				a.status = "Deleted " + parts[1]
			}
			a.input.SetValue("")
			return a, nil
		case "/compact":
			summary := strings.TrimSpace(strings.TrimPrefix(text, parts[0]))
			if err := a.session.Compact(context.Background(), summary, 4); err != nil {
				a.err = "Compact failed: " + err.Error()
			} else {
				a.err = ""
				a.status = "Compacted conversation"
				a.refreshSnapshot()
			}
			a.input.SetValue("")
			return a, nil
		case "/undo":
			if err := a.session.Undo(); err != nil {
				a.err = "Undo failed: " + err.Error()
			} else {
				a.err = ""
				a.status = "Restored last checkpoint"
			}
			a.input.SetValue("")
			return a, nil
		case "/quit", "/exit":
			return a, tea.Quit
		case "/help":
			a.showHelp = true
			a.input.SetValue("")
			return a, nil
		}
	}

	a.err = ""
	a.lastActivity = text
	a.streamingMessage = nil
	a.state = runtime.StateWaitingLLM
	a.pendingTool = nil
	a.pendingRequest = runtime.PermissionRequest{}
	a.input.SetValue("")
	a.eventsCh = make(chan runtime.SessionEvent, 100)
	a.approvalsCh = make(chan runtime.ApprovalDecision, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.viewport.SetContent(a.contentString())
	a.viewport.GotoBottom()
	runCmd := func() tea.Msg {
		return submitDoneMsg{err: a.session.RunTurn(ctx, text, a.eventsCh, a.approvalsCh)}
	}
	return a, tea.Batch(waitForEvent(a.eventsCh), runCmd)
}

func (a App) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var action runtime.ApprovalDecision
	switch strings.ToLower(msg.String()) {
	case "y":
		action = runtime.ApproveOnce
	case "a":
		action = runtime.ApproveAlways
	case "n":
		action = runtime.DenyApproval
	default:
		return a, nil
	}
	a.streamingMessage = nil
	a.approvalsCh <- action
	return a, nil
}
