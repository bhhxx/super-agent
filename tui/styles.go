package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

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
	primary := lipgloss.Color("4")
	secondary := lipgloss.Color("8")
	accent := lipgloss.Color("6")
	styles := Styles{
		Header:         lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(primary).Bold(true).Padding(0, 1),
		Status:         lipgloss.NewStyle().Foreground(accent).Italic(true),
		UserLabel:      lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),
		AgentLabel:     lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
		ToolLabel:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		CommandLabel:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
		Thinking:       lipgloss.NewStyle().Foreground(secondary).Italic(true),
		Error:          lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		Footer:         lipgloss.NewStyle().Foreground(secondary).Italic(true),
		Version:        lipgloss.NewStyle().Foreground(secondary).Align(lipgloss.Right),
		ViewportBorder: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(secondary),
		InputFocused:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1),
		ToolCard:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(secondary).Padding(0, 1),
	}
	styles.MarkdownRenderer, _ = glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(0))
	return styles
}
