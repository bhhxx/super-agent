package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"super-agent/tui/approval"
	"super-agent/tui/attachments"
	"super-agent/tui/commands"
	"super-agent/tui/composer"
	"super-agent/tui/transcript"
)

// Update routes one message and then commits whatever that message pushed out
// of the live window. Every path returns through here, so no branch can leave
// the window overfull.
func (a App) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, command := a.route(message)
	next.commitOverflow()
	return next, tea.Batch(next.flushScrollback(), command)
}

// route applies a message to the model it concerns.
func (a App) route(message tea.Msg) (App, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		return a.resize(message)
	case clipboardDoneMsg:
		return a.finishCopy(message)
	case attachments.Loaded, attachments.Attached:
		var outcome *attachments.Outcome
		a.attachments, outcome = a.attachments.Update(message)
		if outcome == nil {
			return a, nil
		}
		if outcome.Err != nil {
			a.err = "Attach failed: " + outcome.Err.Error()
			return a, nil
		}
		a.err = ""
		a.status = "Attached " + outcome.Attached.Name + " (" + outcome.Attached.MIME + ")"
		return a, nil
	case commands.CompactDone, commands.MCPDone:
		var outcome *commands.Outcome
		a.commands, outcome = a.commands.Update(message)
		return a.applyOutcome(outcome, nil)
	case tea.KeyMsg:
		return a.updateKey(message)
	case conversationNotificationMsg:
		if message.turn != a.turn {
			// A stale listener on a replaced turn channel delivered a
			// leftover notification after a new turn started. Drop it and keep
			// listening on the current channel.
			return a, waitForNotification(a.notificationsCh, a.turn)
		}
		return a.updateConversationNotification(message.notification)
	case submitDoneMsg:
		return a.finishSubmit(message.err)
	default:
		var command tea.Cmd
		a.composer, _, command = a.composer.Update(message)
		return a, command
	}
}

func (a App) resize(message tea.WindowSizeMsg) (App, tea.Cmd) {
	a.width, a.height = max(1, message.Width), max(1, message.Height)
	a.ready = true
	a.composer.SetWidth(a.width)
	a.composer.SetCompactPalette(a.height < 18)
	a.transcript.SetWidth(a.width)
	return a, nil
}

func (a App) updateKey(message tea.KeyMsg) (App, tea.Cmd) {
	if a.showHelp {
		if message.String() == "?" || message.String() == "esc" {
			a.showHelp = false
		}
		return a, nil
	}
	if a.approval.Active() && message.String() != "ctrl+c" && message.String() != "esc" {
		var decision approval.Decision
		var submitted bool
		a.approval, decision, submitted = a.approval.Update(message)
		if submitted {
			a.transcript.ClearStreaming()
			a.approvalsCh <- decision
		}
		return a, nil
	}
	switch message.String() {
	case "ctrl+c":
		if a.approval.Active() || a.cancel != nil {
			a.cancelRun(true)
			return a, nil
		}
		return a, tea.Quit
	case "esc":
		if a.approval.Active() || a.cancel != nil {
			a.cancelRun(true)
			return a, nil
		}
	case "ctrl+l":
		a.err = ""
		a.status = ""
		return a, tea.ClearScreen
	case "ctrl+y":
	case "?":
		if a.composer.Value() == "" {
			a.showHelp = true
			a.status = ""
			return a, nil
		}
	case "pgup", "pgdown":
		return a, nil
	}
	var transcriptIntent *transcript.Intent
	var consumed bool
	a.transcript, transcriptIntent, consumed = a.transcript.Update(message)
	if consumed {
		if transcriptIntent == nil {
			return a, nil
		}
		if transcriptIntent.Error != "" {
			a.err, a.status = transcriptIntent.Error, ""
			return a, nil
		}
		a.err = ""
		return a, a.copyCommand(transcriptIntent.CopyText)
	}
	a.composer.SetTurnRunning(a.cancel != nil)
	var intent *composer.Intent
	var command tea.Cmd
	a.composer, intent, command = a.composer.Update(message)
	if intent == nil {
		return a, command
	}
	switch intent.Kind {
	case composer.Submit:
		return a.submitText(intent.Text)
	case composer.Queue:
		return a.queueInput(intent.Text)
	case composer.Steer:
		return a.steerInput(intent.Text)
	case composer.Clear:
		a.status = "Input cleared"
	}
	return a, command
}

func (a App) updateConversationNotification(notification ConversationNotification) (App, tea.Cmd) {
	switch notification := notification.(type) {
	case AgentStatusChanged:
		a.agentStatus = notification.Status
		a.transcript.SetBusy(notification.Status.Busy)
		if !a.needsInput() {
			a.approval.Clear()
		}
	case ToolApprovalRequested:
		a.approval.Open(approval.Request{ToolName: notification.ToolCall.Name, Input: notification.ToolCall.Input, CommandClass: notification.Request.CommandClass, CWD: notification.Request.CWD, TouchedPaths: notification.Request.TouchedPaths, Reason: notification.Request.Reason, BatchIndex: notification.BatchIndex, BatchTotal: notification.BatchTotal})
	case ToolApprovalCleared:
		a.approval.Clear()
	case MessageAppended:
		a.transcript.Append(notification.Message)
		if notification.Message.Role == RoleAssistant {
			a.transcript.ClearStreaming()
		}
	case ConversationError:
		if notification.Err != nil && !errors.Is(notification.Err, context.Canceled) {
			a.err = notification.Err.Error()
		}
	case StreamChunkReceived:
		a.transcript.SetStreaming(notification.Message)
	}
	return a, waitForNotification(a.notificationsCh, a.turn)
}

// submitText routes submitted input to the feature that owns its meaning: the
// command feature for slash commands, the turn lifecycle for everything else.
func (a App) submitText(text string) (App, tea.Cmd) {
	if a.commands.Compacting() || a.commands.ManagingMCP() {
		a.status = "Background operation in progress…"
		return a, nil
	}
	if !commands.IsCommand(text) {
		return a.submitPrompt(text)
	}
	a.composer.ClearInput()
	model, outcome, command := a.commands.Handle(commands.Input{Text: text, Attachments: pendingAttachments(a.attachments.Items())})
	a.commands = model
	return a.applyOutcome(outcome, command)
}

func (a App) queueInput(text string) (App, tea.Cmd) {
	if a.commands.Compacting() {
		a.status = "Compacting conversation…"
		return a, nil
	}
	if commands.IsCommand(text) {
		a.err = "Slash commands are unavailable while a turn is running"
		return a, nil
	}
	a.composer.Enqueue(text)
	a.composer.ClearInput()
	a.status = "Message queued"
	return a, nil
}

func (a App) steerInput(text string) (App, tea.Cmd) {
	if commands.IsCommand(text) {
		a.err = "Slash commands are unavailable while a turn is running"
		return a, nil
	}
	a.composer.Prepend(text)
	a.composer.ClearInput()
	a.cancelRun(false)
	a.status = "Steering current turn"
	return a, nil
}

func pendingAttachments(items []attachments.Item) []commands.Attachment {
	if len(items) == 0 {
		return nil
	}
	result := make([]commands.Attachment, 0, len(items))
	for _, item := range items {
		result = append(result, commands.Attachment{Name: item.Name, MIME: item.MIME})
	}
	return result
}

func (a App) finishSubmit(err error) (App, tea.Cmd) {
	a.cancel = nil
	a.composer.SetTurnRunning(false)
	if err != nil && !errors.Is(err, context.Canceled) {
		a.err = err.Error()
	}
	if next, ok := a.composer.NextQueued(); ok {
		return a.submitPrompt(next)
	}
	return a, nil
}

func (a App) submitPrompt(text string) (App, tea.Cmd) {
	a.err = ""
	a.status = ""
	a.transcript.ClearStreaming()
	a.agentStatus = AgentStatus{Label: "Submitting", Busy: true}
	a.transcript.SetBusy(true)
	a.approval.Clear()
	a.composer.ClearInput()
	a.composer.SetTurnRunning(true)
	a.attachments.Set(nil)
	a.notificationsCh = make(chan ConversationNotification, 100)
	a.approvalsCh = make(chan ApprovalDecision, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.turn++
	run := func() tea.Msg {
		return submitDoneMsg{err: a.turnPort.RunTurn(ctx, text, a.notificationsCh, a.approvalsCh)}
	}
	return a, tea.Batch(waitForNotification(a.notificationsCh, a.turn), run)
}
