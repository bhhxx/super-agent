package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"super-agent/app/instructions"
	"super-agent/llm"
)

type Flags struct {
	AutoApproveTools bool
	NoTools          bool
}

type Config struct {
	Provider           string
	AutoApproveTools   bool
	NoTools            bool
	ModelConfig        llm.Config
	InstructionSources []string
}

type Settings struct {
	Provider  string                `json:"provider"`
	Providers map[string]llm.Config `json:"providers"`
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
	return Config{
		Provider:           provider,
		AutoApproveTools:   flags.AutoApproveTools || envTrue(lookup, "YOLO"),
		NoTools:            flags.NoTools || envTrue(lookup, "NO_TOOLS"),
		ModelConfig:        settings.Providers[provider],
		InstructionSources: instructionSourcePaths(bundle),
	}, nil
}

func envTrue(lookup func(string) (string, bool), key string) bool {
	value, ok := lookup(key)
	return ok && value == "true"
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
