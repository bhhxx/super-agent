package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"super-agent/tui/approval"
	"super-agent/tui/attachments"
	"super-agent/tui/commands"
	"super-agent/tui/composer"
	"super-agent/tui/transcript"
)

type StartupInfo struct {
	ModelName        string
	PermissionMode   string
	NoTools          bool
	CWD              string
	InstructionPaths []string
}

// App is the composition root and the global router. It owns application
// lifecycle, terminal dimensions, layout, and the cross-feature status line;
// every user capability lives in the feature that owns it.
type App struct {
	snapshot        SnapshotPort
	turnPort        TurnPort
	commands        commands.Model
	composer        composer.Model
	approval        approval.Model
	attachments     attachments.Model
	transcript      transcript.Model
	styles          Styles
	info            StartupInfo
	ready           bool
	width           int
	height          int
	showHelp        bool
	err             string
	status          string
	cancel          context.CancelFunc
	notificationsCh chan ConversationNotification
	approvalsCh     chan ApprovalDecision
	agentStatus     AgentStatus
	turn            int
	writeClipboard  func(string) error
	printOutput     func(string) tea.Cmd
	scrollback      []string
}

type Option func(*App)

func WithClipboardWriter(write func(string) error) Option {
	return func(a *App) { a.writeClipboard = write }
}

func WithOutputPrinter(print func(string) tea.Cmd) Option {
	return func(a *App) { a.printOutput = print }
}

type submitDoneMsg struct{ err error }
type conversationNotificationMsg struct {
	notification ConversationNotification
	turn         int
}

func waitForNotification(ch <-chan ConversationNotification, turn int) tea.Cmd {
	return func() tea.Msg {
		notification, ok := <-ch
		if !ok {
			return nil
		}
		return conversationNotificationMsg{notification: notification, turn: turn}
	}
}

func New(session Conversation, info StartupInfo, options ...Option) App {
	styles := DefaultStyles()

	app := App{
		snapshot: session, turnPort: session,
		commands: commands.New(
			commands.Config{CWD: info.CWD, InstructionPaths: info.InstructionPaths, NoTools: info.NoTools},
			commands.Ports{Sessions: session, Permissions: session, MCP: session, Agents: session,
				Memory: session, Workspace: session, Extensions: session},
		),
		attachments: attachments.New(session), styles: styles, info: info,
		notificationsCh: make(chan ConversationNotification, 100), approvalsCh: make(chan ApprovalDecision, 1),
		agentStatus: AgentStatus{Label: "Idle"}, writeClipboard: defaultClipboardWrite,
		printOutput: func(content string) tea.Cmd { return tea.Println(content) },
	}
	app.composer = composer.New(composerCommands(app.commands.Palette()))
	app.transcript = transcript.New(app.welcomeString(), transcript.Styles{
		Status: styles.Status, UserLabel: styles.UserLabel, ToolLabel: styles.ToolLabel,
		Thinking: styles.Thinking, Footer: styles.Footer, MarkdownRenderer: styles.MarkdownRenderer,
	})
	for _, option := range options {
		option(&app)
	}
	return app
}

// composerCommands maps the command feature's palette onto the composer's own
// entry type, so the composer never learns the command catalogue.
func composerCommands(palette []commands.Command) []composer.Command {
	entries := make([]composer.Command, 0, len(palette))
	for _, command := range palette {
		entries = append(entries, composer.Command{Name: command.Name, Description: command.Description})
	}
	return entries
}

func (a App) Init() tea.Cmd {
	return tea.Batch(a.composer.Init(), a.attachments.Init())
}

func (a App) infoBar() string {
	tools := "tools on"
	if a.info.NoTools {
		tools = "tools off"
	}
	mode := a.info.PermissionMode
	if mode == "" {
		mode = "ask"
	}
	separator := a.styles.Footer.Render(" · ")
	parts := []string{mode, a.info.ModelName, tools}
	for index, part := range parts {
		parts[index] = a.styles.Footer.Render(part)
	}
	return clampLines(a.width, strings.Join(parts, separator))
}

func (a App) needsInput() bool { return a.agentStatus.AwaitingApproval }

func (a App) welcomeString() string {
	parts := []string{a.info.ModelName}
	if location := displayCWD(a.info.CWD); location != "" {
		parts = append(parts, location)
	}
	if count := len(a.info.InstructionPaths); count > 0 {
		parts = append(parts, filepath.Base(a.info.InstructionPaths[count-1]))
	}
	return "Super Agent\n" + strings.Join(parts, " · ")
}

func (a App) footerView() string {
	var rendered strings.Builder
	if a.err != "" {
		rendered.WriteString(a.styles.Error.Render(" !! error: "+a.err) + "\n")
	} else if a.status != "" {
		rendered.WriteString(a.styles.Status.Render(" "+compactStatus(a.status, 3)) + "\n")
	} else {
		rendered.WriteByte('\n')
	}
	if view := a.attachments.View(); view != "" {
		rendered.WriteString(view + "\n")
	}
	if view := a.approval.View(a.info.CWD); view != "" {
		rendered.WriteString(view + "\n")
	}
	rendered.WriteString(a.composer.View() + "\n")
	rendered.WriteString(a.infoBar())
	return clampLines(a.width, rendered.String())
}

func (a *App) refreshSnapshot() {
	snapshot := a.snapshot.Snapshot()
	a.agentStatus = snapshot.AgentStatus
	a.transcript.Replace(snapshot.Messages)
	a.transcript.SetBusy(snapshot.AgentStatus.Busy)
	a.approval.Clear()
	if snapshot.PendingPermission != nil && snapshot.PendingTool != nil {
		call := snapshot.PendingTool
		request := snapshot.PendingPermission
		a.approval.Open(approval.Request{ToolName: call.Name, Input: call.Input, CommandClass: request.CommandClass,
			CWD: request.CWD, TouchedPaths: request.TouchedPaths, Reason: request.Reason,
			BatchIndex: snapshot.PendingToolBatchIndex, BatchTotal: snapshot.PendingToolBatchTotal})
	}
	a.transcript.SetStreaming(snapshot.StreamingMessage)
}

// applyOutcome carries out a command feature's requests. It only routes and
// applies: the feature owns the semantics, the root owns the effects and the
// wiring to the features a command reaches across.
func (a App) applyOutcome(outcome *commands.Outcome, command tea.Cmd) (App, tea.Cmd) {
	if outcome == nil {
		return a, command
	}
	if outcome.Err != "" {
		a.err = outcome.Err
	} else {
		a.err = ""
		if outcome.Status != "" {
			a.status = outcome.Status
		}
	}
	if outcome.StatusBar != nil {
		if outcome.StatusBar.ModelName != "" {
			a.info.ModelName = outcome.StatusBar.ModelName
		}
		a.info.PermissionMode = outcome.StatusBar.PermissionMode
	}
	if outcome.RefreshSnapshot {
		a.refreshSnapshot()
	}
	if outcome.Quit {
		return a, tea.Quit
	}
	if outcome.ShowHelp {
		a.showHelp = true
	}
	if outcome.AttachPath != "" {
		return a, a.attachments.Attach(outcome.AttachPath)
	}
	if outcome.Prompt != "" {
		return a.submitPrompt(outcome.Prompt)
	}
	if outcome.Output != "" {
		a.queueScrollback(outcome.Output)
	}
	return a, command
}

// queueScrollback adds content to this update's terminal output. Everything an
// update queues leaves as a single write, so a command's divider always lands
// above the transcript overflow it separates.
func (a *App) queueScrollback(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	a.scrollback = append(a.scrollback, strings.TrimRight(content, "\n"))
}

// flushScrollback returns the command that writes this update's queued output.
// It returns nil when the update queued nothing, which is the common case.
func (a *App) flushScrollback() tea.Cmd {
	if len(a.scrollback) == 0 {
		return nil
	}
	content := strings.Join(a.scrollback, "\n")
	a.scrollback = nil
	print := a.printOutput
	if print == nil {
		print = func(content string) tea.Cmd { return tea.Println(content) }
	}
	return print(content)
}

// commitOverflow hands the transcript blocks that no longer fit the live window
// to terminal scrollback. The live window is sized from the footer that is on
// screen right now, so a menu or a growing composer commits what it pushes out
// in the same update.
func (a *App) commitOverflow() {
	if !a.ready {
		return
	}
	evicted := a.transcript.Commit(a.transcriptRows())
	if evicted == "" {
		return
	}
	a.queueScrollback(clampLines(a.width, evicted))
}

// transcriptRows reports the rows left for the live window once the footer and
// the blank line above it are accounted for.
func (a App) transcriptRows() int {
	return a.height - lineCount(a.footerView()) - 1
}

// lineCount reports how many terminal rows a rendered block occupies.
func lineCount(block string) int {
	if block == "" {
		return 0
	}
	return strings.Count(block, "\n") + 1
}

func displayCWD(cwd string) string {
	if home, err := os.UserHomeDir(); err == nil {
		return strings.Replace(cwd, home, "~", 1)
	}
	return cwd
}

func compactStatus(value string, maxLines int) string {
	lines := strings.Split(value, "\n")
	if len(lines) <= maxLines {
		return value
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n… %d more lines", len(lines)-maxLines)
}
