package llm

// NewDeepSeek returns an OpenAIModel configured for DeepSeek API.
func NewDeepSeek(cfg Config) *OpenAIModel {
	cfg = withDefaults(cfg, Config{
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-reasoner",
	})
	return NewOpenAIModel(cfg)
}
