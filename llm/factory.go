package llm

import (
	"errors"
	"super-agent/runtime"
)

type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

type ModelFactory func() runtime.Model

type ModelRegistry struct {
	factories map[string]ModelFactory
}

func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{factories: make(map[string]ModelFactory)}
}

func (r *ModelRegistry) Register(provider string, factory ModelFactory) {
	r.factories[provider] = factory
}

func (r *ModelRegistry) New(provider string) (runtime.Model, error) {
	if provider == "" {
		provider = "deepseek"
	}
	factory, ok := r.factories[provider]
	if !ok {
		return nil, errors.New("unknown llm provider: " + provider)
	}
	return factory(), nil
}

func NewDefaultModelRegistry() *ModelRegistry {
	registry := NewModelRegistry()
	registry.Register("deepseek", func() runtime.Model { return NewDeepSeek() })
	registry.Register("openai", func() runtime.Model { return NewOpenAI() })
	registry.Register("claude", func() runtime.Model { return NewClaude() })
	return registry
}

func NewModel(provider string) (runtime.Model, error) {
	return defaultRegistry.New(provider)
}

var defaultRegistry = NewDefaultModelRegistry()
