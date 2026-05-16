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

func NewModel(provider string) (runtime.Model, error) {
	switch provider {
	case "openai":
		return NewOpenAI(), nil
	case "claude":
		return NewClaude(), nil
	case "", "deepseek":
		return NewDeepSeek(), nil
	}
	return nil, errors.New("unknown llm provider: " + provider)
}
