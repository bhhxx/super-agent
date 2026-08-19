package tui

import (
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

func ExtractCodeBlocks(content string) []string {
	var blocks []string
	lines := strings.Split(content, "\n")
	inBlock := false
	var current strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inBlock {
				blocks = append(blocks, strings.TrimSuffix(current.String(), "\n"))
				current.Reset()
			}
			inBlock = !inBlock
			continue
		}
		if inBlock {
			current.WriteString(line + "\n")
		}
	}
	return blocks
}

func (a *App) copyLastCodeBlock() tea.Cmd {
	for i := len(a.messages) - 1; i >= 0; i-- {
		blocks := ExtractCodeBlocks(a.messages[i].Content)
		if len(blocks) == 0 {
			continue
		}
		if err := clipboard.WriteAll(blocks[len(blocks)-1]); err != nil {
			a.err = "Failed to copy: " + err.Error()
			a.status = ""
		} else {
			a.status = "Copied code block to clipboard!"
			a.err = ""
		}
		return nil
	}
	a.err = "No code blocks found to copy"
	a.status = ""
	return nil
}

func (a *App) cancelRun(clearQueue bool) {
	if a.pendingTool != nil {
		if err := a.session.Cancel(); err != nil {
			a.err = err.Error()
		}
	}
	if a.cancel != nil {
		a.cancel()
	}
	if clearQueue {
		a.queuedInputs = nil
		a.status = "Turn canceled"
	}
}
