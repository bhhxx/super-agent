package llm

import (
	"os"
)

// NewDeepSeek returns an OpenAIModel configured for DeepSeek API.
func NewDeepSeek() *OpenAIModel {
	return NewOpenAIModel(Config{
		BaseURL: envDefault("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		APIKey:  envDefault("DEEPSEEK_API_KEY", os.Getenv("OPENAI_API_KEY")),
		Model:   envDefault("DEEPSEEK_MODEL", "deepseek-reasoner"),
	})
}
