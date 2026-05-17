package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"

	"super-agent/app"
	"super-agent/llm"
	"super-agent/tui"
)

func main() {
	autoApproveToolsFlag := flag.Bool("yolo", false, "Auto-approve tool execution")
	noToolsFlag := flag.Bool("no-tools", false, "Disable tool calling")
	flag.Parse()

	_ = godotenv.Load()

	cfg := app.LoadConfig(app.Flags{
		AutoApproveTools: *autoApproveToolsFlag,
		NoTools:          *noToolsFlag,
	}, os.LookupEnv)
	session, err := app.NewSession(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	if _, err := tea.NewProgram(tui.New(session, tui.TUIInfo{
		Provider:    cfg.Provider,
		ModelName:   llm.ModelDisplayName(cfg.Provider),
		AutoApprove: cfg.AutoApproveTools,
		NoTools:     cfg.NoTools,
		CWD:         cwd,
	}), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
