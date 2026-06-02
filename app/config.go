package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"super-agent/app/instructions"
	"super-agent/llm"
	"super-agent/runtime"
)

type Flags struct {
	AutoApproveTools bool
	NoTools          bool
	PermissionMode   string
}

type Config struct {
	Provider           string
	AutoApproveTools   bool
	NoTools            bool
	PermissionMode     runtime.PermissionMode
	PermissionRules    runtime.PermissionRules
	ModelConfig        llm.Config
	InstructionSources []string
}

type Settings struct {
	Provider    string                `json:"provider"`
	Providers   map[string]llm.Config `json:"providers"`
	Permissions PermissionSettings    `json:"permissions"`
	Sandbox     SandboxSettings       `json:"sandbox"`
}

type PermissionSettings struct {
	Mode                 string   `json:"mode"`
	AllowTools           []string `json:"allow_tools"`
	DenyTools            []string `json:"deny_tools"`
	AllowCommandPrefixes []string `json:"allow_command_prefixes"`
	DenyCommandPrefixes  []string `json:"deny_command_prefixes"`
	AllowPaths           []string `json:"allow_paths"`
	DenyPaths            []string `json:"deny_paths"`
	AllowEnv             []string `json:"allow_env"`
	DenyEnv              []string `json:"deny_env"`
	Network              string   `json:"network"`
}

type SandboxSettings struct {
	Backend        string `json:"backend"`
	OpenSandboxID  string `json:"opensandbox_id"`
	OpenSandboxCLI string `json:"opensandbox_cli"`
	OpenSandboxCWD string `json:"opensandbox_cwd"`
}

func DefaultSettings() Settings {
	return Settings{
		Provider: "deepseek",
		Providers: map[string]llm.Config{
			"deepseek": {
				BaseURL: "https://api.deepseek.com",
				APIKey:  "sk-...",
				Model:   "deepseek-reasoner",
			},
			"openai": {
				APIKey: "sk-...",
				Model:  "gpt-4o",
			},
			"claude": {
				APIKey: "sk-ant-...",
				Model:  "claude-3-7-sonnet-20250219",
			},
		},
	}
}

func LoadConfig(flags Flags, lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	settings, err := LoadSettings()
	if err != nil {
		return Config{}, err
	}
	provider := settings.Provider
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	bundle, err := instructions.Load(cwd)
	if err != nil {
		return Config{}, err
	}
	mode := runtime.PermissionMode(firstNonEmpty(flags.PermissionMode, settings.Permissions.Mode, "ask"))
	if flags.AutoApproveTools || envTrue(lookup, "YOLO") {
		mode = runtime.PermissionModeBypass
	}
	rules := runtime.PermissionRules{
		AllowTools:     settings.Permissions.AllowTools,
		DenyTools:      settings.Permissions.DenyTools,
		AllowPrefixes:  settings.Permissions.AllowCommandPrefixes,
		DenyPrefixes:   settings.Permissions.DenyCommandPrefixes,
		AllowPaths:     settings.Permissions.AllowPaths,
		DenyPaths:      settings.Permissions.DenyPaths,
		AllowEnv:       settings.Permissions.AllowEnv,
		DenyEnv:        settings.Permissions.DenyEnv,
		Network:        firstNonEmpty(settings.Permissions.Network, "deny"),
		OpenSandboxCLI: firstNonEmpty(settings.Sandbox.OpenSandboxCLI, "osb"),
		OpenSandboxCWD: settings.Sandbox.OpenSandboxCWD,
	}
	if settings.Sandbox.Backend == "opensandbox" {
		rules.OpenSandboxID = settings.Sandbox.OpenSandboxID
	}
	return Config{
		Provider:           provider,
		AutoApproveTools:   mode == runtime.PermissionModeBypass,
		NoTools:            flags.NoTools || envTrue(lookup, "NO_TOOLS"),
		PermissionMode:     mode,
		PermissionRules:    rules,
		ModelConfig:        settings.Providers[provider],
		InstructionSources: instructionSourcePaths(bundle),
	}, nil
}

func envTrue(lookup func(string) (string, bool), key string) bool {
	value, ok := lookup(key)
	return ok && value == "true"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func LoadSettings() (Settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Settings{}, err
	}
	return LoadSettingsFile(filepath.Join(home, ".superagent", "settings.json"))
}

func LoadSettingsFile(path string) (Settings, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		var settings Settings
		if err := json.Unmarshal(content, &settings); err != nil {
			return Settings{}, err
		}
		if settings.Providers == nil {
			settings.Providers = map[string]llm.Config{}
		}
		return settings, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		settings := DefaultSettings()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return Settings{}, err
		}
		content, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return Settings{}, err
		}
		content = append(content, '\n')
		if err := os.WriteFile(path, content, 0600); err != nil {
			return Settings{}, err
		}
		return settings, nil
	}
	return Settings{}, err
}
