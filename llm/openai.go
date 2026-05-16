package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"super-agent/runtime"
)

func NewOpenAI() *OpenAIModel {
	return NewOpenAIModel(Config{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
		Model:   envDefault("OPENAI_MODEL", "gpt-4o"),
	})
}

type OpenAIModel struct {
	client openai.Client
	model  string
}

func NewOpenAIModel(cfg Config) *OpenAIModel {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithHeader("X-Title", "SuperAgent"),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &OpenAIModel{
		client: openai.NewClient(opts...),
		model:  cfg.Model,
	}
}

func (m *OpenAIModel) Next(ctx context.Context, messages []runtime.Message, tools []runtime.ToolSpec, chunkFunc func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	params := openai.ChatCompletionNewParams{
		Model:    m.model,
		Messages: toOpenAIMessages(messages),
		Tools:    toOpenAITools(tools),
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
				chunkFunc(runtime.StreamChunk{
					ContentDelta:          delta.Content,
					ReasoningContentDelta: rc,
				})
			}
		}
	}
	if err := stream.Err(); err != nil {
		return runtime.ModelResponse{}, err
	}
	if len(acc.Choices) == 0 {
		return runtime.ModelResponse{}, errors.New("llm returned no choices")
	}

	message := acc.Choices[0].Message

	finalRC := reasoningBuilder.String()
	if finalRC == "" {
		finalRC = reasoningText(message.RawJSON())
	}

	if len(message.ToolCalls) > 0 {
		calls := make([]runtime.ToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			calls = append(calls, runtime.ToolCall{
				ID:    call.ID,
				Name:  call.Function.Name,
				Input: call.Function.Arguments,
				Risky: isRiskyTool(call.Function.Name, tools),
			})
		}
		return runtime.ModelResponse{
			FinalAnswer:      message.Content,
			ReasoningContent: finalRC,
			ToolCalls:        calls,
		}, nil
	}
	return runtime.ModelResponse{
		FinalAnswer:      message.Content,
		ReasoningContent: finalRC,
	}, nil
}

func isRiskyTool(name string, tools []runtime.ToolSpec) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Risky
		}
	}
	return false
}

func toOpenAITools(tools []runtime.ToolSpec) []openai.ChatCompletionToolUnionParam {
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

func toOpenAIMessages(messages []runtime.Message) []openai.ChatCompletionMessageParamUnion {
	params := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case runtime.RoleAssistant:
			params = append(params, assistantMessage(msg.Content, msg.ReasoningContent, msg.ToolCalls))
		case runtime.RoleTool:
			params = append(params, openai.ToolMessage(msg.Content, msg.ToolCallID))
		default:
			params = append(params, openai.UserMessage(msg.Content))
		}
	}
	return params
}

func assistantMessage(content, reasoningContent string, toolCalls []*runtime.ToolCall) openai.ChatCompletionMessageParamUnion {
	msg := openai.AssistantMessage(content)
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

type messageWithReasoning struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	Reasoning        string `json:"reasoning"`
	Thinking         string `json:"thinking"`
}

func reasoningText(rawJSON string) string {
	var msg messageWithReasoning
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		return ""
	}
	switch {
	case msg.ReasoningContent != "":
		return msg.ReasoningContent
	case msg.Reasoning != "":
		return msg.Reasoning
	default:
		return msg.Thinking
	}
}

func envDefault(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}
