package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "super-agent/app"
)

func TestLoadConfigCombinesFlagsEnvAndSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{
		"provider": "claude",
		"permissions": {
			"mode": "accept-edits",
			"network": "allow",
			"allow_command_prefixes": ["git status"]
		},
		"providers": {
			"claude": {
				"base_url": "https://claude.test",
				"api_key": "claude-key",
				"model": "claude-test"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"LLM_PROVIDER": "openai",
		"NO_TOOLS":     "true",
	}
	cfg, err := LoadConfig(Flags{AutoApproveTools: true}, lookup(env))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Provider != "claude" {
		t.Fatalf("provider = %q, want claude", cfg.Provider)
	}
	if cfg.ModelConfig.BaseURL != "https://claude.test" || cfg.ModelConfig.APIKey != "claude-key" || cfg.ModelConfig.Model != "claude-test" {
		t.Fatalf("ModelConfig = %+v", cfg.ModelConfig)
	}
	if !cfg.NoTools {
		t.Fatal("NoTools = false, want true")
	}
	if !cfg.AutoApproveTools {
		t.Fatal("AutoApproveTools = false, want true")
	}
	if cfg.PermissionMode != "bypass" {
		t.Fatalf("PermissionMode = %q, want bypass", cfg.PermissionMode)
	}
	if len(cfg.PermissionRules.AllowPrefixes) != 1 || cfg.PermissionRules.AllowPrefixes[0] != "git status" {
		t.Fatalf("AllowPrefixes = %+v", cfg.PermissionRules.AllowPrefixes)
	}
}

func TestLoadConfigReadsCustomAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"deepseek","providers":{"deepseek":{"model":"reasoner"}},"agent":"reviewer","agents":{"reviewer":{"prompt":"Review only.","permission_mode":"plan"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(Flags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent != "reviewer" || cfg.Agents["reviewer"].Prompt != "Review only." {
		t.Fatalf("agent config = %+v", cfg)
	}
}

func TestLoadConfigBuildsProjectAndWorkspaceFromExplicitDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	selected := t.TempDir()
	processCWD := t.TempDir()
	t.Chdir(processCWD)
	cfg, err := LoadConfig(Flags{CWD: selected}, nil)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := filepath.EvalSymlinks(selected)
	if cfg.Project.Root != canonical || cfg.ConfigRoot != canonical {
		t.Fatalf("project = %+v config root = %q, want %q", cfg.Project, cfg.ConfigRoot, canonical)
	}
	if cfg.Workspace.GetPrimaryRoot() != canonical || cfg.Workspace.GetCWD() != canonical || cfg.Sandbox.Workspace != canonical {
		t.Fatalf("workspace context or sandbox not wired to %q", canonical)
	}
}

func TestLoadConfigReadsLSPServers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"deepseek","providers":{"deepseek":{}},"lsp_servers":{"go":{"command":"gopls","args":["serve"],"extensions":["go"],"language_id":"go"}}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(Flags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LSPServers) != 1 || cfg.LSPServers[0].Command != "gopls" || cfg.LSPServers[0].LanguageID != "go" {
		t.Fatalf("LSP servers = %+v", cfg.LSPServers)
	}
}

func TestLoadConfigCombinesSkillsCommandsAndPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	if err := os.MkdirAll(filepath.Join(project, "skill"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "skill", "SKILL.md"), []byte("Use focused tests."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"commands":{"audit":"Audit $ARGUMENTS"},"hooks":{"after_turn":["go test ./..."]}}`
	if err := os.WriteFile(filepath.Join(project, "plugin", "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"deepseek","providers":{"deepseek":{}},"extensions":{"commands":{"explain":"Explain $ARGUMENTS"},"skills":["skill"],"plugins":["plugin"]}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(Flags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Extensions.Commands["audit"] == "" || cfg.Extensions.Commands["explain"] == "" || !strings.Contains(cfg.Extensions.SkillPrompt, "Use focused tests.") || len(cfg.Extensions.Hooks["after_turn"]) != 1 {
		t.Fatalf("extensions = %+v", cfg.Extensions)
	}
}

func TestLoadConfigUsesSettingsPermissionMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","permissions":{"mode":"plan"},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(Flags{}, lookup(nil))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.PermissionMode != "plan" {
		t.Fatalf("PermissionMode = %q, want plan", cfg.PermissionMode)
	}
}

func TestLoadConfigRejectsInvalidPermissionMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","permissions":{"mode":"root"},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(Flags{}, lookup(nil))

	if err == nil || !strings.Contains(err.Error(), "invalid permission mode: root") {
		t.Fatalf("err = %v, want invalid permission mode", err)
	}
}

func TestLoadConfigCreatesDefaultSettingsWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := LoadConfig(Flags{}, lookup(nil))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Provider != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", cfg.Provider)
	}
	if cfg.ModelConfig.Model != "deepseek-reasoner" {
		t.Fatalf("ModelConfig = %+v", cfg.ModelConfig)
	}
	if cfg.PermissionMode != "ask" || cfg.AutoApproveTools {
		t.Fatalf("default permissions = mode %q, auto-approve %t; want ask, false", cfg.PermissionMode, cfg.AutoApproveTools)
	}
	if cfg.Sandbox.Mode != "strict" || cfg.Sandbox.AllowNetwork {
		t.Fatalf("default sandbox = %+v, want strict with network denied", cfg.Sandbox)
	}
}

func TestLoadConfigMapsSandboxAndNetworkSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","permissions":{"network":"allow"},"sandbox":{"mode":"off","cpu_seconds":9,"memory_mb":64,"max_processes":7,"max_open_files":11},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(Flags{}, lookup(nil))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Sandbox.Mode != "off" || !cfg.Sandbox.AllowNetwork || cfg.Sandbox.CPUSeconds != 9 || cfg.Sandbox.MemoryBytes != 64<<20 || cfg.Sandbox.MaxProcesses != 7 || cfg.Sandbox.MaxOpenFiles != 11 {
		t.Fatalf("Sandbox = %+v", cfg.Sandbox)
	}
}

func TestLoadConfigRejectsInvalidSandboxMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","sandbox":{"mode":"maybe"},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(Flags{}, lookup(nil))
	if err == nil || !strings.Contains(err.Error(), "invalid sandbox mode: maybe") {
		t.Fatalf("err = %v, want invalid sandbox mode", err)
	}
}

func TestLoadConfigMapsMCPServersInNameOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","mcp_servers":{"z":{"command":"z-server"},"a":{"command":"a-server","args":["--stdio"],"env":{"TOKEN":"explicit"},"cwd":"nested","connect_timeout_seconds":3,"call_timeout_seconds":4}},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(Flags{}, lookup(nil))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.MCPServers) != 2 || cfg.MCPServers[0].Name != "a" || cfg.MCPServers[1].Name != "z" {
		t.Fatalf("MCPServers = %+v", cfg.MCPServers)
	}
	server := cfg.MCPServers[0]
	if server.Command != "a-server" || len(server.Args) != 1 || server.Env["TOKEN"] != "explicit" || !filepath.IsAbs(server.CWD) || server.ConnectTimeout != 3*time.Second || server.CallTimeout != 4*time.Second {
		t.Fatalf("server = %+v", server)
	}
}

func TestLoadSettingsFileCreatesTemplateWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".superagent", "settings.json")

	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatalf("LoadSettingsFile failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated settings: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("generated settings is empty")
	}
	if settings.Provider != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", settings.Provider)
	}
	if settings.Providers["deepseek"].Model != "deepseek-reasoner" {
		t.Fatalf("deepseek config = %+v", settings.Providers["deepseek"])
	}
	if settings.Permissions.Mode != "ask" || settings.Permissions.Network != "deny" {
		t.Fatalf("permissions template = %+v", settings.Permissions)
	}
	if !strings.Contains(string(content), `"permissions"`) {
		t.Fatalf("generated settings missing discoverable permissions section: %s", string(content))
	}
}

func TestLoadSettingsFileDoesNotOverwriteExistingSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	existing := `{"provider":"openai","providers":{"openai":{"api_key":"keep","model":"custom"}}}`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatalf("LoadSettingsFile failed: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != existing {
		t.Fatalf("settings was overwritten: %s", string(content))
	}
	if settings.Provider != "openai" || settings.Providers["openai"].APIKey != "keep" {
		t.Fatalf("settings = %+v", settings)
	}
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestYOLOEnvDoesNotOverrideExplicitApprovalMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadConfig(Flags{PermissionMode: "ask"}, lookup(map[string]string{"YOLO": "true"}))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.PermissionMode != "ask" {
		t.Fatalf("PermissionMode = %q, want ask: an explicit --approval-mode flag must win over YOLO", cfg.PermissionMode)
	}
	if cfg.AutoApproveTools {
		t.Fatal("AutoApproveTools = true, want false")
	}
}

func TestYOLOEnvEnablesBypassWithoutExplicitMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadConfig(Flags{}, lookup(map[string]string{"YOLO": "true"}))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.PermissionMode != "bypass" {
		t.Fatalf("PermissionMode = %q, want bypass", cfg.PermissionMode)
	}
	if !cfg.AutoApproveTools {
		t.Fatal("AutoApproveTools = false, want true")
	}
}
