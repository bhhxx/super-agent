package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (a App) helpView() string {
	helpStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6")).Padding(1, 2).Width(45)
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
		"", "enter        Submit / steer active turn", "tab          Queue while running", "ctrl+j       Insert newline", "up/down      History / move lines", "pgup/pgdn    Scroll history",
		"tab          Complete slash command", "up/down      Select slash command", "esc/ctrl+u   Clear input / Cancel", "ctrl+l       Clear viewport", "ctrl+y       Copy last code block", "ctrl+c       Quit / Cancel", "?            Toggle help",
		"", "Tool Approval:", "up/down      Select decision", "enter        Confirm decision", "1/y          Approve once", "2/a          Always allow", "3/n          Deny call",
	}
	return helpStyle.Render(title + "\n\n" + strings.Join(items, "\n"))
}

func formatSessions(summaries []SessionSummary) string {
	if len(summaries) == 0 {
		return "No saved sessions"
	}
	lines := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		lines = append(lines, fmt.Sprintf("%s  %s  %s/%s", summary.ID, summary.Title, summary.Provider, summary.Model))
	}
	return strings.Join(lines, "\n")
}

func formatInstructions(paths []string) string {
	if len(paths) == 0 {
		return "No instruction files loaded"
	}
	var result strings.Builder
	result.WriteString("Loaded instruction sources:\n")
	for _, path := range paths {
		result.WriteString("- ")
		result.WriteString(path)
		result.WriteByte('\n')
	}
	return strings.TrimSpace(result.String())
}

func formatPermissions(info TUIInfo) string {
	mode := firstNonEmpty(info.PermissionMode, "ask")
	return fmt.Sprintf("Permission mode: %s\nTools: %s\nApproval: %s\nCWD: %s", mode, onOff(!info.NoTools), onOff(info.AutoApprove), info.CWD)
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
	header, footer := a.headerView(), a.footerView()
	minViewportHeight := a.styles.ViewportBorder.GetVerticalFrameSize() + 1
	a.viewport.Height = max(minViewportHeight, a.height-lipgloss.Height(header)-lipgloss.Height(footer))
	main := lipgloss.JoinVertical(lipgloss.Left, header, a.viewport.View(), footer)
	if !a.showHelp {
		return main
	}
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, a.helpView(), lipgloss.WithWhitespaceChars(" "), lipgloss.WithWhitespaceForeground(lipgloss.Color("8")))
}
