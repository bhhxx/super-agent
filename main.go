package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"

	"super-agent/app"
	"super-agent/tui"
)

func main() {
	autoApproveToolsFlag := flag.Bool("yolo", false, "Auto-approve tool execution")
	noToolsFlag := flag.Bool("no-tools", false, "Disable tool calling")
	flag.Parse()

	_ = godotenv.Load()

	session, err := app.NewSession(app.LoadConfig(app.Flags{
		AutoApproveTools: *autoApproveToolsFlag,
		NoTools:          *noToolsFlag,
	}, os.LookupEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(tui.New(session), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
