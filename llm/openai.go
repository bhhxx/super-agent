package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"super-agent/runtime/protocol"
)

func NewOpenAI(cfg ProviderConfig) *OpenAIModel {
	cfg = withDefaults(cfg, ProviderConfig{Model: "gpt-4o"})
	return newOpenAIModel(cfg, true)
}

type OpenAIModel struct {
	client openai.Client
	model  string
	usage  bool
}

// httpClient bounds how long a provider may take to produce response
// headers without capping the streaming body: a stalled connection fails
// within two minutes instead of hanging the turn forever, while a long
// generation keeps streaming for as long as the provider keeps sending.
// A replaced DefaultTransport (tests, custom agents) is passed through
// untouched rather than cloned.
func httpClient() *http.Client {
	transport := http.DefaultTransport
	if base, ok := transport.(*http.Transport); ok {
		clone := base.Clone()
		clone.ResponseHeaderTimeout = 2 * time.Minute
		transport = clone
	}
	return &http.Client{Transport: transport}
}

func newOpenAIModel(cfg ProviderConfig, requestUsage bool) *OpenAIModel {
	opts := []option.RequestOption{
		option.WithHTTPClient(httpClient()),
		option.WithHeader("X-Title", "SuperAgent"),
	}
	// An empty key must not shadow the SDK's environment fallback
	// (OPENAI_API_KEY), so only send a header when configured.
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &OpenAIModel{
		client: openai.NewClient(opts...),
		model:  cfg.Model,
		usage:  requestUsage,
	}
}

func (m *OpenAIModel) Next(ctx context.Context, messages []protocol.Message, tools []protocol.ToolSpec, chunkFunc func(protocol.StreamChunk)) (protocol.ModelResponse, error) {
	params := openai.ChatCompletionNewParams{
		Model:    m.model,
		Messages: toOpenAIMessages(messages),
		Tools:    toOpenAITools(tools),
	}
	if m.usage {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
	}
	stream := m.client.Chat.Completions.NewStreaming(ctx, params)
	acc := openai.ChatCompletionAccumulator{}

	var reasoningBuilder strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if len(chunk.Choices) > 0 {
			deltaRaw := chunk.Choices[0].Delta
			var delta messageWithReasoning
			_ = json.Unmarshal([]byte(deltaRaw.RawJSON()), &delta)

			rc := delta.ReasoningContent
			if rc == "" {
				rc = delta.Reasoning
			}
			if rc == "" {
				rc = delta.Thinking
			}

			if rc != "" {
				reasoningBuilder.WriteString(rc)
			}

			if chunkFunc != nil && (delta.Content != "" || rc != "") {
				chunkFunc(protocol.StreamChunk{
					ContentDelta:          delta.Content,
					ReasoningContentDelta: rc,
				})
			}
		}
	}
	if err := stream.Err(); err != nil {
		return protocol.ModelResponse{}, err
	}
	if len(acc.Choices) == 0 {
		return protocol.ModelResponse{}, errors.New("llm returned no choices")
	}

	message := acc.Choices[0].Message
	// A length finish means the provider cut the response mid-stream. A
	// half-emitted tool-call arguments string would otherwise reach the
	// tool executor as if it were valid JSON, so fail loudly instead.
	if acc.Choices[0].FinishReason == "length" {
		return protocol.ModelResponse{}, errors.New("llm output truncated by the token limit (finish_reason=length); raise the model's max output or shorten the conversation")
	}
	if message.Refusal != "" {
		return protocol.ModelResponse{}, errors.New("llm refused the request: " + message.Refusal)
	}

	finalRC := reasoningBuilder.String()
	response := protocol.ModelResponse{
		Content:          message.Content,
		ReasoningContent: finalRC,
	}
	if u := acc.Usage; u.PromptTokens > 0 || u.CompletionTokens > 0 {
		response.Usage = &protocol.Usage{
			InputTokens:  u.PromptTokens,
			OutputTokens: u.CompletionTokens,
			TotalTokens:  u.TotalTokens,
		}
	}
	if len(message.ToolCalls) > 0 {
		calls := make([]protocol.ToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			calls = append(calls, protocol.ToolCall{
				ID:    call.ID,
				Name:  call.Function.Name,
				Input: call.Function.Arguments,
			})
		}
		response.ToolCalls = calls
	}
	return response, nil
}

func toOpenAITools(tools []protocol.ToolSpec) []openai.ChatCompletionToolUnionParam {
	params := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		params = append(params, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: openai.String(tool.Description),
			Parameters:  shared.FunctionParameters(tool.Parameters),
		}))
	}
	return params
}

func toOpenAIMessages(messages []protocol.Message) []openai.ChatCompletionMessageParamUnion {
	params := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case protocol.RoleSystem:
			params = append(params, openai.SystemMessage(msg.Content))
		case protocol.RoleAssistant:
			params = append(params, assistantMessage(msg.Content, msg.ReasoningContent, msg.ToolCalls))
		case protocol.RoleTool:
			params = append(params, openai.ToolMessage(msg.Content, msg.ToolCallID))
		default:
			params = append(params, openAIUserMessage(msg))
		}
	}
	return params
}

func openAIUserMessage(message protocol.Message) openai.ChatCompletionMessageParamUnion {
	if len(message.Attachments) == 0 {
		return openai.UserMessage(message.Content)
	}
	parts := []openai.ChatCompletionContentPartUnionParam{openai.TextContentPart(message.Content)}
	for _, attachment := range message.Attachments {
		if strings.HasPrefix(attachment.MIME, "image/") {
			parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: "data:" + attachment.MIME + ";base64," + attachment.Data, Detail: "auto"}))
		} else {
			parts = append(parts, openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{FileData: openai.String(attachment.Data), Filename: openai.String(attachment.Name)}))
		}
	}
	return openai.UserMessage(parts)
}

func assistantMessage(content, reasoningContent string, toolCalls []*protocol.ToolCall) openai.ChatCompletionMessageParamUnion {
	msg := openai.AssistantMessage(content)
	// reasoning_content must be passed back for DeepSeek thinking mode with
	// tools (the API rejects the turn without it). Plain OpenAI tolerates
	// the unknown message field today; the contract is unversioned.
	if reasoningContent != "" {
		msg.OfAssistant.SetExtraFields(map[string]any{
			"reasoning_content": reasoningContent,
		})
	}
	if len(toolCalls) > 0 {
		oaiToolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(toolCalls))
		for _, tc := range toolCalls {
			oaiToolCalls = append(oaiToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Input,
					},
				},
			})
		}
		msg.OfAssistant.ToolCalls = oaiToolCalls
	}
	return msg
}

// messageWithReasoning decodes the reasoning field names that
// OpenAI-compatible providers actually use (DeepSeek's reasoning_content and
// reasoning, plus thinking), which the typed SDK delta does not model.
type messageWithReasoning struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	Reasoning        string `json:"reasoning"`
	Thinking         string `json:"thinking"`
}

func withDefaults(cfg, defaults ProviderConfig) ProviderConfig {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaults.BaseURL
	}
	if cfg.APIKey == "" {
		cfg.APIKey = defaults.APIKey
	}
	if cfg.Model == "" {
		cfg.Model = defaults.Model
	}
	return cfg
}
