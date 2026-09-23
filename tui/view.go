package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (a App) helpView() string {
	helpStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6")).Padding(1, 2).Width(max(20, min(64, a.width-4)))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render("Commands & Shortcuts")
	items := []string{
		a.styles.CommandLabel.Render("/agent") + "    Select agent profile",
		a.styles.CommandLabel.Render("/attach") + "   Queue image or file",
		a.styles.CommandLabel.Render("/clear") + "    Reset conversation",
		a.styles.CommandLabel.Render("/sessions") + " List saved sessions",
		a.styles.CommandLabel.Render("/resume") + "   Resume saved session",
		a.styles.CommandLabel.Render("/compact") + "  Compact context",
		a.styles.CommandLabel.Render("/undo") + "     Restore checkpoint",
		a.styles.CommandLabel.Render("/permissions") + " Show permission policy",
		a.styles.CommandLabel.Render("/mcp") + "      Manage MCP servers",
		a.styles.CommandLabel.Render("/review") + "   Review current changes",
		a.styles.CommandLabel.Render("/export") + "   Export or share session",
		a.styles.CommandLabel.Render("/help") + "     Show this menu",
		a.styles.CommandLabel.Render("/quit") + "     Exit application",
		"", "enter        Submit / steer active turn", "tab          Queue while running", "ctrl+j       Insert newline", "up/down      History / move lines",
		"tab          Complete slash command", "up/down      Select slash command", "esc/ctrl+u   Clear input / Cancel", "ctrl+l       Clear screen", "ctrl+y       Copy last code block", "ctrl+o       Toggle latest tools", "alt+o        Toggle all tools", "ctrl+t       Toggle latest reasoning", "alt+t        Toggle all reasoning", "ctrl+c       Quit / Cancel", "?            Toggle help",
		"", "Tool Approval:", "up/down      Select decision", "enter        Confirm decision", "1/y          Approve once", "2/a          Always allow", "3/n          Deny call",
	}
	return helpStyle.Render(title + "\n\n" + strings.Join(items, "\n"))
}

// clampLines truncates every line of block to at most width columns so the
// terminal never hard-wraps it. Truncation is ANSI- and width-aware.
func clampLines(width int, block string) string {
	return lipgloss.NewStyle().MaxWidth(max(1, width)).Render(block)
}

func (a App) View() string {
	if !a.ready {
		return "\n  Initializing..."
	}
	if a.showHelp {
		return fillDynamicArea(a.width, a.height, a.helpView(), "")
	}
	return fillDynamicArea(a.width, a.height, a.transcript.View(), a.footerView())
}

// fillDynamicArea composes the live window and the footer into exactly height
// rows. The footer sits on the last row and blank rows fill the space the
// window does not use; without a footer the content starts at the top. Content
// taller than the area keeps its tail, which the transcript's own sizing leaves
// only to a block taller than the terminal.
//
// The frame fills the terminal on purpose. bubbletea's inline renderer scrolls
// by exactly the lines a commit prints only while the frame it repaints is that
// tall; a shorter frame absorbs the print, and the committed text never reaches
// scrollback.
func fillDynamicArea(width, height int, window, footer string) string {
	lines := make([]string, 0, max(0, height))
	if window = clampLines(width, window); window != "" {
		lines = append(lines, strings.Split(window, "\n")...)
		lines = append(lines, "")
	}
	if footer = clampLines(width, footer); footer != "" {
		lines = append(lines, strings.Split(footer, "\n")...)
	}
	if height <= 0 {
		return strings.Join(lines, "\n")
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	blank := make([]string, height-len(lines))
	if footer == "" {
		return strings.Join(append(lines, blank...), "\n")
	}
	return strings.Join(append(blank, lines...), "\n")
}
