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
	autoApproveToolsFlag := flag.Bool("yolo", false, "Auto-approve tool execution") // 读取命令行参数
	noToolsFlag := flag.Bool("no-tools", false, "Disable tool calling")
	approvalModeFlag := flag.String("approval-mode", "", "Permission mode: ask, accept-edits, plan, bypass")
	cwdFlag := flag.String("cwd", "", "Project directory (defaults to nearest Git root)")
	flag.Parse()

	_ = godotenv.Load() // 加载环境变量

	cfg, err := app.LoadConfig(app.Flags{ // 组合命令行参数和环境变量到 config
		AutoApproveTools: *autoApproveToolsFlag,
		NoTools:          *noToolsFlag,
		PermissionMode:   *approvalModeFlag,
		CWD:              *cwdFlag,
	}, os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	session, mcpController, agentController, err := app.NewSessionWithExtensions(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer session.Close()
	profile := agentController.Current()
	if _, err := tea.NewProgram(tui.New(app.NewTUIConversation(session, mcpController, agentController), tui.StartupInfo{
		ModelName:        llm.ModelDisplayName(profile.Provider, llm.ProviderConfig{Model: profile.Model}),
		PermissionMode:   string(profile.PermissionMode),
		NoTools:          cfg.NoTools,
		CWD:              cfg.Workspace.GetCWD(),
		InstructionPaths: cfg.InstructionSources,
	})).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
