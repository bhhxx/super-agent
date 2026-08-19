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
	autoApproveToolsFlag := flag.Bool("yolo", true, "Auto-approve tool execution") // 读取命令行参数
	noToolsFlag := flag.Bool("no-tools", false, "Disable tool calling")
	approvalModeFlag := flag.String("approval-mode", "", "Permission mode: ask, accept-edits, plan, bypass")
	flag.Parse()

	_ = godotenv.Load() // 加载环境变量

	cfg, err := app.LoadConfig(app.Flags{ // 组合命令行参数和环境变量到 config
		AutoApproveTools: *autoApproveToolsFlag,
		NoTools:          *noToolsFlag,
		PermissionMode:   *approvalModeFlag,
	}, os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	session, err := app.NewSession(cfg) // 一次会话
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	if _, err := tea.NewProgram(tui.New(app.NewTUIConversation(session), tui.TUIInfo{
		Provider:         cfg.Provider,
		ModelName:        llm.ModelDisplayName(cfg.Provider, cfg.ModelConfig),
		AutoApprove:      cfg.AutoApproveTools,
		PermissionMode:   string(cfg.PermissionMode),
		NoTools:          cfg.NoTools,
		CWD:              cwd,
		InstructionPaths: cfg.InstructionSources,
	}), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
