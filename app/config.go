package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"super-agent/app/instructions"
	"super-agent/llm"
	"super-agent/project"
	"super-agent/runtime"
	"super-agent/tools"
	lsptools "super-agent/tools/lsp"
	mcptools "super-agent/tools/mcp"
	workspaceadapter "super-agent/workspace"
)

type Flags struct {
	AutoApproveTools bool
	NoTools          bool
	PermissionMode   string
	CWD              string
}

type Config struct {
	Provider           string
	NoTools            bool
	PermissionMode     runtime.PermissionMode
	PermissionRules    runtime.PermissionRules
	Sandbox            tools.SandboxConfig
	MCPServers         []mcptools.ServerConfig
	LSPServers         []lsptools.ServerConfig
	ModelConfig        llm.ProviderConfig
	ProviderConfigs    map[string]llm.ProviderConfig
	Instructions       instructions.Bundle
	InstructionSources []string
	Agents             map[string]AgentSettings
	Agent              string
	Extensions         Extensions
	TelemetryPath      string
	Project            project.Project
	Workspace          *workspaceadapter.Context
	ConfigRoot         string
}

type Settings struct {
	Provider    string                        `json:"provider"`
	Providers   map[string]llm.ProviderConfig `json:"providers"`
	Permissions PermissionSettings            `json:"permissions"`
	Sandbox     SandboxSettings               `json:"sandbox"`
	MCPServers  map[string]MCPServerSettings  `json:"mcp_servers"`
	LSPServers  map[string]LSPServerSettings  `json:"lsp_servers"`
	Agent       string                        `json:"agent"`
	Agents      map[string]AgentSettings      `json:"agents"`
	Extensions  ExtensionSettings             `json:"extensions"`
	Telemetry   TelemetrySettings             `json:"telemetry"`
}

type TelemetrySettings struct {
	LogPath string `json:"log_path"`
}

type AgentSettings struct {
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
	Tools          []string `json:"tools,omitempty"`
}

type LSPServerSettings struct {
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	Extensions []string `json:"extensions"`
	LanguageID string   `json:"language_id"`
}

type MCPServerSettings struct {
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	Env                   map[string]string `json:"env"`
	CWD                   string            `json:"cwd"`
	ConnectTimeoutSeconds int               `json:"connect_timeout_seconds"`
	CallTimeoutSeconds    int               `json:"call_timeout_seconds"`
}

type SandboxSettings struct {
	Mode         string `json:"mode"`
	CPUSeconds   int    `json:"cpu_seconds"`
	MemoryMB     int64  `json:"memory_mb"`
	MaxProcesses int    `json:"max_processes"`
	MaxOpenFiles int    `json:"max_open_files"`
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

func DefaultSettings() Settings {
	return Settings{
		Provider: "deepseek",
		Providers: map[string]llm.ProviderConfig{
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
		Permissions: PermissionSettings{
			Mode:    "ask",
			Network: "deny",
		},
		Sandbox: SandboxSettings{
			Mode:         "strict",
			CPUSeconds:   120,
			MemoryMB:     1024,
			MaxProcesses: 128,
			MaxOpenFiles: 256,
		},
		MCPServers: map[string]MCPServerSettings{},
		LSPServers: map[string]LSPServerSettings{},
		Agent:      "build",
		Agents:     map[string]AgentSettings{},
		Extensions: ExtensionSettings{Commands: map[string]string{}, Hooks: map[string][]string{}},
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
	processCWD, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	selectedProject, err := project.Resolve(flags.CWD, processCWD)
	if err != nil {
		return Config{}, err
	}
	workspaceContext, err := workspaceadapter.NewDefaultContext(selectedProject.Root)
	if err != nil {
		return Config{}, err
	}
	cwd := workspaceContext.GetCWD()
	telemetryPath := settings.Telemetry.LogPath
	if telemetryPath == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return Config{}, homeErr
		}
		telemetryPath = filepath.Join(home, ".superagent", "telemetry.jsonl")
	} else if !filepath.IsAbs(telemetryPath) {
		telemetryPath = filepath.Join(cwd, telemetryPath)
	}
	bundle, err := instructions.Load(cwd)
	if err != nil {
		return Config{}, err
	}
	extensions, err := loadExtensions(settings.Extensions, cwd)
	if err != nil {
		return Config{}, err
	}
	mode := runtime.PermissionMode(firstNonEmpty(flags.PermissionMode, settings.Permissions.Mode, "ask"))
	// The YOLO environment variable is a fallback for when no explicit
	// mode was requested; an explicit --approval-mode flag always wins so
	// a checked-in .env cannot silently disable permission prompts.
	if flags.AutoApproveTools && flags.PermissionMode != "" {
		return Config{}, errors.New("--yolo and --approval-mode are mutually exclusive; use --approval-mode bypass instead of --yolo")
	}
	if flags.AutoApproveTools || (flags.PermissionMode == "" && envTrue(lookup, "YOLO")) {
		mode = runtime.PermissionModeBypass
	}
	if !runtime.ValidPermissionMode(mode) {
		return Config{}, errors.New("invalid permission mode: " + string(mode))
	}
	sandboxMode := tools.SandboxMode(settings.Sandbox.Mode)
	if !tools.ValidSandboxMode(sandboxMode) {
		return Config{}, errors.New("invalid sandbox mode: " + settings.Sandbox.Mode)
	}
	rules := runtime.PermissionRules{
		AllowTools:    settings.Permissions.AllowTools,
		DenyTools:     settings.Permissions.DenyTools,
		AllowPrefixes: settings.Permissions.AllowCommandPrefixes,
		DenyPrefixes:  settings.Permissions.DenyCommandPrefixes,
		AllowPaths:    settings.Permissions.AllowPaths,
		DenyPaths:     settings.Permissions.DenyPaths,
		AllowEnv:      settings.Permissions.AllowEnv,
		DenyEnv:       settings.Permissions.DenyEnv,
		Network:       firstNonEmpty(settings.Permissions.Network, "deny"),
	}
	mcpServers := make([]mcptools.ServerConfig, 0, len(settings.MCPServers))
	mcpNames := make([]string, 0, len(settings.MCPServers))
	for name := range settings.MCPServers {
		mcpNames = append(mcpNames, name)
	}
	sort.Strings(mcpNames)
	for _, name := range mcpNames {
		server := settings.MCPServers[name]
		serverCWD := server.CWD
		if serverCWD == "" {
			serverCWD = cwd
		} else if !filepath.IsAbs(serverCWD) {
			serverCWD = filepath.Join(cwd, serverCWD)
		}
		mcpServers = append(mcpServers, mcptools.ServerConfig{
			Name: name, Command: server.Command, Args: server.Args, Env: server.Env, CWD: serverCWD,
			ConnectTimeout: time.Duration(server.ConnectTimeoutSeconds) * time.Second,
			CallTimeout:    time.Duration(server.CallTimeoutSeconds) * time.Second,
		})
	}
	lspServers := make([]lsptools.ServerConfig, 0, len(settings.LSPServers))
	for name, server := range settings.LSPServers {
		lspServers = append(lspServers, lsptools.ServerConfig{Name: name, Command: server.Command, Args: server.Args, Extensions: server.Extensions, LanguageID: server.LanguageID, Root: cwd})
	}
	providerConfig, err := resolveProviderConfig(settings, provider, lookup)
	if err != nil {
		return Config{}, err
	}
	sort.Slice(lspServers, func(i, j int) bool { return lspServers[i].Name < lspServers[j].Name })
	return Config{
		Provider:        provider,
		NoTools:         flags.NoTools || envTrue(lookup, "NO_TOOLS"),
		PermissionMode:  mode,
		PermissionRules: rules,
		Sandbox: tools.SandboxConfig{
			Mode:         sandboxMode,
			Workspace:    cwd,
			AllowNetwork: rules.Network == "allow",
			CPUSeconds:   settings.Sandbox.CPUSeconds,
			MemoryBytes:  settings.Sandbox.MemoryMB << 20,
			MaxProcesses: settings.Sandbox.MaxProcesses,
			MaxOpenFiles: settings.Sandbox.MaxOpenFiles,
		},
		MCPServers:         mcpServers,
		LSPServers:         lspServers,
		ModelConfig:        providerConfig,
		ProviderConfigs:    settings.Providers,
		Instructions:       bundle,
		InstructionSources: instructionSourcePaths(bundle),
		Agents:             settings.Agents,
		Agent:              firstNonEmpty(settings.Agent, "build"),
		Extensions:         extensions,
		TelemetryPath:      telemetryPath,
		Project:            selectedProject,
		Workspace:          workspaceContext,
		ConfigRoot:         selectedProject.Root,
	}, nil
}

// apikeyPlaceholder is the value DefaultSettings writes into a fresh
// settings.json. It is not a real credential, so it is treated as "unset" to
// let the provider's environment variable take over instead of being sent as
// a bearer token.
func apikeyPlaceholder(provider string) string {
	if provider == "claude" {
		return "sk-ant-..."
	}
	return "sk-..."
}

// resolveProviderConfig validates the selected provider at startup so a
// missing provider or credential fails loudly instead of surfacing as an
// HTTP 401 on the first turn.
func resolveProviderConfig(settings Settings, provider string, lookup func(string) (string, bool)) (llm.ProviderConfig, error) {
	config, ok := settings.Providers[provider]
	if !ok {
		return llm.ProviderConfig{}, errors.New("provider " + provider + " is not configured: add it to the providers map in settings.json")
	}
	if config.APIKey == apikeyPlaceholder(provider) {
		config.APIKey = ""
	}
	if config.APIKey == "" {
		envKey := strings.ToUpper(provider) + "_API_KEY"
		if value, ok := lookup(envKey); ok && value != "" {
			config.APIKey = value
			return config, nil
		}
		return llm.ProviderConfig{}, errors.New("provider " + provider + " has no api_key: set it in settings.json or export " + envKey)
	}
	return config, nil
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
	path, err := SettingsPath()
	if err != nil {
		return Settings{}, err
	}
	return LoadSettingsFile(path)
}

func SettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".superagent", "settings.json"), nil
}

func LoadSettingsFile(path string) (Settings, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		var settings Settings
		if err := json.Unmarshal(content, &settings); err != nil {
			return Settings{}, err
		}
		if settings.Providers == nil {
			settings.Providers = map[string]llm.ProviderConfig{}
		}
		if settings.Agents == nil {
			settings.Agents = map[string]AgentSettings{}
		}
		normalizeSettings(&settings)
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

func SaveSettingsFile(path string, settings Settings) error {
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func normalizeSettings(settings *Settings) {
	if settings.Agent == "" {
		settings.Agent = "build"
	}
	if settings.Agents == nil {
		settings.Agents = map[string]AgentSettings{}
	}
	if settings.LSPServers == nil {
		settings.LSPServers = map[string]LSPServerSettings{}
	}
	if settings.Permissions.Mode == "" {
		settings.Permissions.Mode = "ask"
	}
	if settings.Permissions.Network == "" {
		settings.Permissions.Network = "deny"
	}
	defaults := DefaultSettings().Sandbox
	if settings.Sandbox.Mode == "" {
		settings.Sandbox.Mode = defaults.Mode
	}
	if settings.Sandbox.CPUSeconds <= 0 {
		settings.Sandbox.CPUSeconds = defaults.CPUSeconds
	}
	if settings.Sandbox.MemoryMB <= 0 {
		settings.Sandbox.MemoryMB = defaults.MemoryMB
	}
	if settings.Sandbox.MaxProcesses <= 0 {
		settings.Sandbox.MaxProcesses = defaults.MaxProcesses
	}
	if settings.Sandbox.MaxOpenFiles <= 0 {
		settings.Sandbox.MaxOpenFiles = defaults.MaxOpenFiles
	}
}
