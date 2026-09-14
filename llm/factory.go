package llm

import (
	"errors"
	"super-agent/runtime/protocol"
)

type ProviderConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

type ModelFactory func(ProviderConfig) protocol.Model

type ModelRegistry struct {
	factories map[string]ModelFactory
}

func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{factories: make(map[string]ModelFactory)}
}

func (r *ModelRegistry) RegisterConfigured(provider string, factory ModelFactory) {
	r.factories[provider] = factory
}

func (r *ModelRegistry) Create(provider string, cfg ProviderConfig) (protocol.Model, error) {
	if provider == "" {
		provider = "deepseek"
	}
	factory, ok := r.factories[provider]
	if !ok {
		return nil, errors.New("unknown llm provider: " + provider)
	}
	return factory(cfg), nil
}

func NewDefaultModelRegistry() *ModelRegistry {
	registry := NewModelRegistry()
	registry.RegisterConfigured("deepseek", func(cfg ProviderConfig) protocol.Model { return NewDeepSeek(cfg) })
	registry.RegisterConfigured("openai", func(cfg ProviderConfig) protocol.Model { return NewOpenAI(cfg) })
	registry.RegisterConfigured("claude", func(cfg ProviderConfig) protocol.Model { return NewClaude(cfg) })
	return registry
}

func NewModel(provider string, cfg ProviderConfig) (protocol.Model, error) {
	return defaultRegistry.Create(provider, cfg)
}

func ModelDisplayName(provider string, cfg ProviderConfig) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	if provider == "" {
		provider = "deepseek"
	}
	switch provider {
	case "deepseek":
		return "deepseek-reasoner"
	case "openai":
		return "gpt-4o"
	case "claude":
		return "claude-3-7-sonnet-20250219"
	default:
		return provider
	}
}

var defaultRegistry = NewDefaultModelRegistry()
