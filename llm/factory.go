package llm

import (
	"os"
	"super-agent/runtime"
)

type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

func NewModel() runtime.Model {
	provider := os.Getenv("LLM_PROVIDER")
	switch provider {
	case "openai":
		return NewOpenAI()
	case "claude":
		return NewClaude()
	default:
		return NewDeepSeek()
	}
}
