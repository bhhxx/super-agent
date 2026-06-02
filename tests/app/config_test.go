package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		"sandbox": {
			"backend": "opensandbox",
			"opensandbox_id": "sbx-1",
			"opensandbox_cli": "osb-dev",
			"opensandbox_cwd": "/workspace"
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
	if cfg.PermissionRules.OpenSandboxID != "sbx-1" || cfg.PermissionRules.OpenSandboxCLI != "osb-dev" || cfg.PermissionRules.OpenSandboxCWD != "/workspace" {
		t.Fatalf("PermissionRules sandbox = %+v", cfg.PermissionRules)
	}
	if len(cfg.PermissionRules.AllowPrefixes) != 1 || cfg.PermissionRules.AllowPrefixes[0] != "git status" {
		t.Fatalf("AllowPrefixes = %+v", cfg.PermissionRules.AllowPrefixes)
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

func TestLoadConfigRejectsInvalidSandboxBackend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","sandbox":{"backend":"docker"},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(Flags{}, lookup(nil))

	if err == nil || !strings.Contains(err.Error(), "invalid sandbox backend: docker") {
		t.Fatalf("err = %v, want invalid sandbox backend", err)
	}
}

func TestLoadConfigRequiresOpenSandboxID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".superagent")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settings := `{"provider":"openai","sandbox":{"backend":"opensandbox"},"providers":{"openai":{"api_key":"key","model":"model"}}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(Flags{}, lookup(nil))

	if err == nil || !strings.Contains(err.Error(), "sandbox.opensandbox_id is required") {
		t.Fatalf("err = %v, want missing opensandbox id", err)
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
	if settings.Sandbox.Backend != "local" || settings.Sandbox.OpenSandboxCLI != "osb" || settings.Sandbox.OpenSandboxCWD != "/workspace" {
		t.Fatalf("sandbox template = %+v", settings.Sandbox)
	}
	if !strings.Contains(string(content), `"sandbox"`) || !strings.Contains(string(content), `"permissions"`) {
		t.Fatalf("generated settings missing discoverable permissions/sandbox sections: %s", string(content))
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
