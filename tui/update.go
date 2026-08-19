package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (a App) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case spinner.TickMsg:
		var command tea.Cmd
		a.spinner, command = a.spinner.Update(message)
		return a, command
	case tea.WindowSizeMsg:
		return a.resize(message)
	case tea.MouseMsg:
		var command tea.Cmd
		a.viewport, command = a.viewport.Update(message)
		return a, command
	case tea.KeyMsg:
		return a.updateKey(message)
	case sessionEventMsg:
		if message.turn != a.turn {
			// A stale listener on a replaced turn channel delivered a
			// leftover event after a new turn started. Drop it and keep
			// listening on the current channel.
			return a, waitForEvent(a.eventsCh, a.turn)
		}
		return a.updateSessionEvent(message.event)
	case submitDoneMsg:
		return a.finishSubmit(message.err)
	case compactDoneMsg:
		a.compacting = false
		if message.err != nil {
			a.err = "Compact failed: " + message.err.Error()
		} else {
			a.err = ""
			a.status = "Compacted conversation"
			a.refreshSnapshot()
		}
		return a, nil
	default:
		var command tea.Cmd
		a.input, command = a.input.Update(message)
		a.resizeInput()
		return a, command
	}
}

func (a App) resize(message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	a.width, a.height = max(1, message.Width), max(1, message.Height)
	headerHeight := lipgloss.Height(a.headerView())
	footerHeight := lipgloss.Height(a.footerView())
	minViewportHeight := a.styles.ViewportBorder.GetVerticalFrameSize() + 1
	viewportHeight := max(minViewportHeight, a.height-headerHeight-footerHeight)
	if a.ready {
		a.viewport.SetYOffset(0)
	}
	if !a.ready {
		a.viewport = viewport.New(a.width, viewportHeight)
		a.viewport.Style = a.styles.ViewportBorder
		a.ready = true
	} else {
		a.viewport.Width = a.width
		a.viewport.Height = viewportHeight
	}
	a.input.SetWidth(max(1, a.width-4))
	a.viewport.SetContent(a.contentString())
	a.viewport.GotoBottom()
	return a, nil
}

func (a App) updateKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.showHelp {
		if message.String() == "?" || message.String() == "esc" {
			a.showHelp = false
		}
		return a, nil
	}
	if a.pendingTool != nil && message.String() != "ctrl+c" && message.String() != "esc" {
		return a.handleApprovalKey(message)
	}
	switch message.String() {
	case "ctrl+c":
		if a.pendingTool != nil || a.cancel != nil {
			a.cancelRun(true)
			return a, nil
		}
		return a, tea.Quit
	case "esc":
		if a.pendingTool != nil || a.cancel != nil {
			a.cancelRun(true)
			return a, nil
		}
		if a.input.Value() != "" {
			a.input.SetValue("")
			a.resizeInput()
			a.historyIdx = len(a.history)
			a.historyDraft = ""
			a.status = "Input cleared"
		}
		return a, nil
	case "ctrl+u":
		a.input.SetValue("")
		a.resizeInput()
		a.historyIdx = len(a.history)
		a.historyDraft = ""
		a.status = "Input cleared"
		return a, nil
	case "ctrl+j", "shift+enter", "alt+enter":
		return a.insertNewline()
	case "tab":
		if a.cancel != nil && a.pendingTool == nil {
			return a.queueInput()
		}
		if completed, ok := a.completeSelectedSlashCommand(); ok {
			completed.status = ""
			return completed, nil
		}
		if completed, ok := completeSlashCommand(a.input.Value()); ok {
			a.input.SetValue(completed)
			a.resizeInput()
			a.input.CursorEnd()
			a.status = ""
			return a, nil
		}
	case "ctrl+l":
		a.viewport.SetContent("")
		a.err = ""
		a.status = ""
		a.lastActivity = "Viewport cleared"
		return a, nil
	case "ctrl+y":
		return a, a.copyLastCodeBlock()
	case "?":
		if a.input.Value() == "" {
			a.showHelp = true
			a.status = ""
			return a, nil
		}
	case "up":
		if matches := a.slashMatches(); len(matches) > 0 {
			if a.slashSelection > 0 {
				a.slashSelection--
			}
			return a, nil
		}
		if strings.Contains(a.input.Value(), "\n") {
			break
		}
		if a.historyIdx > 0 {
			a.status = ""
			if a.historyIdx == len(a.history) {
				a.historyDraft = a.input.Value()
			}
			a.historyIdx--
			a.input.SetValue(a.history[a.historyIdx])
			a.resizeInput()
			a.input.CursorEnd()
			return a, nil
		}
	case "down":
		if matches := a.slashMatches(); len(matches) > 0 {
			if a.slashSelection < len(matches)-1 {
				a.slashSelection++
			}
			return a, nil
		}
		if strings.Contains(a.input.Value(), "\n") {
			break
		}
		if a.historyIdx < len(a.history)-1 {
			a.historyIdx++
			a.input.SetValue(a.history[a.historyIdx])
			a.resizeInput()
			a.input.CursorEnd()
			return a, nil
		}
		if a.historyIdx == len(a.history)-1 {
			a.historyIdx = len(a.history)
			a.input.SetValue(a.historyDraft)
			a.resizeInput()
			a.input.CursorEnd()
			return a, nil
		}
	case "pgup", "pgdown":
		var command tea.Cmd
		a.viewport, command = a.viewport.Update(message)
		return a, command
	case "enter":
		a.status = ""
		if a.pendingTool == nil && a.cancel == nil {
			if completed, ok := a.completeSelectedSlashCommand(); ok {
				return completed, nil
			}
			return a.submit()
		}
		if a.pendingTool == nil {
			return a.steerInput()
		}
	}
	if a.pendingTool != nil {
		a.status = ""
		return a.handleApprovalKey(message)
	}
	var command tea.Cmd
	a.slashSelection = 0
	a.input, command = a.input.Update(message)
	a.resizeInput()
	return a, command
}

func (a App) insertNewline() (tea.Model, tea.Cmd) {
	var command tea.Cmd
	a.input, command = a.input.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.resizeInput()
	return a, command
}

func (a *App) resizeInput() {
	height := strings.Count(a.input.Value(), "\n") + 1
	if height > 5 {
		height = 5
	}
	a.input.SetHeight(height)
}

func (a App) updateSessionEvent(event Event) (tea.Model, tea.Cmd) {
	switch event := event.(type) {
	case AgentStatusChanged:
		a.agentStatus = event.Status
		a.recordState(event.Status.Label)
		if !a.needsInput() {
			a.clearPendingTool()
		}
	case ToolApprovalRequested:
		call := event.ToolCall
		a.pendingTool, a.pendingRequest = &call, event.Request
		a.pendingToolIndex, a.pendingToolTotal = event.BatchIndex, event.BatchTotal
		a.approvalSelection, a.approvalSubmitted = 0, false
	case ToolApprovalCleared:
		a.clearPendingTool()
	case MessageAppended:
		a.messages = append(a.messages, event.Message)
		if event.Message.Role == RoleAssistant {
			a.streamingMessage = nil
		}
	case ConversationError:
		if event.Err != nil && !errors.Is(event.Err, context.Canceled) {
			a.err = event.Err.Error()
		}
	case StreamChunkReceived:
		a.streamingMessage = event.Message
	}
	a.refreshContent()
	return a, waitForEvent(a.eventsCh, a.turn)
}

func (a App) finishSubmit(err error) (tea.Model, tea.Cmd) {
	a.cancel = nil
	if err != nil && !errors.Is(err, context.Canceled) {
		a.err = err.Error()
	}
	a.refreshContent()
	if len(a.queuedInputs) > 0 {
		next := a.queuedInputs[0]
		a.queuedInputs = a.queuedInputs[1:]
		return a.startTurn(next)
	}
	return a, nil
}

func (a *App) clearPendingTool() {
	a.pendingTool = nil
	a.pendingRequest = PermissionRequest{}
	a.pendingToolIndex, a.pendingToolTotal = 0, 0
	a.approvalSelection, a.approvalSubmitted = 0, false
}

func (a *App) refreshContent() {
	wasAtBottom := a.viewport.AtBottom()
	a.viewport.SetContent(a.contentString())
	if wasAtBottom {
		a.viewport.GotoBottom()
	}
}
