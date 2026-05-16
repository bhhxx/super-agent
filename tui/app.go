package tui

import (
	"context"
	"errors"
	"fmt"
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

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0),
	)
	s.MarkdownRenderer = renderer

	return s
}

type App struct {
	session         Conversation
	input           textinput.Model
	viewport        viewport.Model
	spinner         spinner.Model
	styles          Styles
	history         []string
	historyIdx      int
	ready           bool
	width           int
	height          int
	showHelp        bool
	err             string
	status          string
	lastCmd         string
	cancel          context.CancelFunc
	eventsCh        chan runtime.SessionEvent
	approvalsCh     chan runtime.ConfirmationAction
	streamContent   string
	streamReasoning string
}

type Conversation interface {
	Snapshot() runtime.Snapshot
	Run(context.Context, string, chan<- runtime.SessionEvent, <-chan runtime.ConfirmationAction) error
	Cancel() error
	Reset()
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

func New(session Conversation) App {
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
		history:     []string{},
		eventsCh:    make(chan runtime.SessionEvent, 100),
		approvalsCh: make(chan runtime.ConfirmationAction, 1),
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
	snapshot := a.session.Snapshot()
	state := snapshot.State
	switch state {
	case runtime.StateWaitingLLM:
		icon = a.spinner.View()
	case runtime.StateWaitingApproval:
		icon = "✋"
	case runtime.StateRunningTool:
		icon = a.spinner.View()
	default:
		icon = "◇"
	}

	title := a.styles.Header.Render(" SUPER AGENT ")
	status := a.styles.Status.Render(fmt.Sprintf(" %s %s", icon, state))

	header := title + status
	version := a.styles.Version.Width(a.width - lipgloss.Width(header)).Render("v0.1.0")
	return header + version + "\n"
}

func (a App) footerView() string {
	var b strings.Builder

	if a.err != "" {
		b.WriteString(a.styles.Error.Render(" !! error: "+a.err) + "\n")
	}

	if a.status != "" {
		b.WriteString(a.styles.Status.Render(" "+a.status) + "\n")
	}

	if a.lastCmd != "" {
		b.WriteString(a.styles.Footer.Render(" Last: "+a.lastCmd) + "\n")
	}

	if call := a.session.Snapshot().PendingTool; call != nil {
		prompt := lipgloss.NewStyle().
			Background(lipgloss.Color("3")).
			Foreground(lipgloss.Color("0")).
			Padding(0, 1).
			Render(" ACTION REQUIRED ")
		b.WriteString(prompt + " approve " + lipgloss.NewStyle().Bold(true).Render(call.Name) + "? [y/a/n]\n")
	}

	count := fmt.Sprintf("%d/%d", len(a.input.Value()), a.input.CharLimit)
	scrollPercent := "  0%"
	if a.ready {
		scrollPercent = fmt.Sprintf("%3.f%%", a.viewport.ScrollPercent()*100)
	}

	stats := a.styles.Footer.Render(fmt.Sprintf("%s  •  %s", count, scrollPercent))
	inputView := a.styles.InputFocused.Width(a.width - 2).Render(a.input.View())
	b.WriteString(inputView + "\n")

	footerText := a.styles.Footer.Render(" esc: quit • ?: help • up/down: history • ctrl+l: clear • ctrl+y: copy code")
	padding := a.width - lipgloss.Width(footerText) - lipgloss.Width(stats)
	if padding < 0 {
		padding = 0
	}
	b.WriteString(footerText + strings.Repeat(" ", padding) + stats)

	return b.String()
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

func (a App) contentString() string {
	var b strings.Builder

	width := a.viewport.Width - 4
	if width <= 0 {
		width = 80
	}
	wrapStyle := lipgloss.NewStyle().Width(width).Padding(0, 1)

	snapshot := a.session.Snapshot()
	messages := snapshot.Messages
	for i, msg := range messages {
		var msgBlock strings.Builder

		role := strings.ToUpper(string(msg.Role))
		switch msg.Role {
		case "user":
			msgBlock.WriteString(a.styles.UserLabel.Render(role) + "\n")
		case "assistant":
			msgBlock.WriteString(a.styles.AgentLabel.Render(role) + "\n")
		case "tool":
			toolName := "unknown"
			for j := i - 1; j >= 0; j-- {
				if messages[j].Role == "assistant" {
					for _, tc := range messages[j].ToolCalls {
						if tc.ID == msg.ToolCallID {
							toolName = tc.Name
							break
						}
					}
					if toolName != "unknown" {
						break
					}
				}
			}
			msgBlock.WriteString(a.styles.ToolLabel.Render(fmt.Sprintf("%s (%s)", role, toolName)) + "\n")
		default:
			msgBlock.WriteString(lipgloss.NewStyle().Bold(true).Render(role) + "\n")
		}

		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				msgBlock.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("  ⚒  "+tc.Name+"("+tc.Input+")") + "\n")
			}
		}

		if msg.Role == "tool" {
			content := msg.Content
			if len(content) > 1000 {
				content = content[:1000] + "... (truncated)"
			}
			msgBlock.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(content) + "\n")
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

	if snapshot.State == runtime.StateWaitingLLM {
		var streamBlock strings.Builder
		streamBlock.WriteString(a.styles.AgentLabel.Render("AGENT") + "\n")

		if a.streamReasoning != "" {
			streamBlock.WriteString(a.styles.Thinking.Render(a.streamReasoning))
		}
		if a.streamContent != "" {
			if a.streamReasoning != "" {
				streamBlock.WriteString("\n\n")
			}
			streamBlock.WriteString(a.streamContent)
		}

		if a.streamReasoning != "" || a.streamContent != "" {
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
	messages := a.session.Snapshot().Messages
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
	if a.session.Snapshot().PendingTool != nil {
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
			if a.session.Snapshot().PendingTool != nil || a.cancel != nil {
				a.cancelRun()
				return a, nil
			}
			return a, tea.Quit
		case "ctrl+l":
			a.viewport.SetContent("")
			a.err = ""
			a.status = ""
			a.lastCmd = "Viewport cleared"
			return a, nil
		case "ctrl+y":
			return a, a.copyLastCodeBlock()
		case "esc":
			if a.session.Snapshot().PendingTool != nil || a.cancel != nil {
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
			if a.session.Snapshot().PendingTool == nil {
				return a.submit()
			}
		}

		if a.session.Snapshot().PendingTool != nil {
			a.status = ""
			return a.handleApprovalKey(msg)
		}

	case sessionEventMsg:
		switch event := msg.event.(type) {
		case runtime.SessionError:
			if !errors.Is(event.Err, context.Canceled) {
				a.err = event.Err.Error()
			}
		case runtime.StreamChunkReceived:
			a.streamContent += event.Chunk.ContentDelta
			a.streamReasoning += event.Chunk.ReasoningContentDelta
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
		case "/clear", "/reset":
			a.session.Reset()
			a.viewport.SetContent("")
			a.err = ""
			a.lastCmd = "Conversation reset"
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
	a.lastCmd = text
	a.streamContent = ""
	a.streamReasoning = ""
	a.input.SetValue("")
	a.eventsCh = make(chan runtime.SessionEvent, 100)
	a.approvalsCh = make(chan runtime.ConfirmationAction, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.viewport.SetContent(a.contentString())
	a.viewport.GotoBottom()
	runCmd := func() tea.Msg {
		return submitDoneMsg{err: a.session.Run(ctx, text, a.eventsCh, a.approvalsCh)}
	}
	return a, tea.Batch(waitForEvent(a.eventsCh), runCmd)
}

func (a App) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var action runtime.ConfirmationAction
	switch strings.ToLower(msg.String()) {
	case "y":
		action = runtime.ConfirmOnce
	case "a":
		action = runtime.ConfirmAlways
	case "n":
		action = runtime.ConfirmDeny
	default:
		return a, nil
	}
	a.streamContent = ""
	a.streamReasoning = ""
	a.approvalsCh <- action
	return a, nil
}
