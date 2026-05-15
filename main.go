package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"

	"super-agent/llm"
	"super-agent/runtime"
	"super-agent/tools"
	"super-agent/tui"
)

func main() {
	yoloFlag := flag.Bool("yolo", false, "Enable YOLO mode (autonomous execution)")
	noToolsFlag := flag.Bool("no-tools", false, "Disable tool calling")
	flag.Parse()

	_ = godotenv.Load()

	model := llm.NewModel()
	toolRunner := runtime.ToolRunner(tools.NewBashTools())
	if *noToolsFlag || os.Getenv("NO_TOOLS") == "true" {
		toolRunner = tools.NoTools{}
	}
	engine := runtime.NewEngine(model, toolRunner, nil)

	if *yoloFlag || os.Getenv("YOLO") == "true" {
		engine.EnableYOLO()
	}

	engine.Ready()
	if _, err := tea.NewProgram(tui.New(engine), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
