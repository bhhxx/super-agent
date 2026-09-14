package llm

// NewDeepSeek returns an OpenAIModel configured for DeepSeek API. Usage is
// not requested explicitly: DeepSeek-compatible endpoints vary on
// stream_options support, and DeepSeek streams carry usage itself where
// available.
func NewDeepSeek(cfg ProviderConfig) *OpenAIModel {
	cfg = withDefaults(cfg, ProviderConfig{
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-reasoner",
	})
	return newOpenAIModel(cfg, false)
}
