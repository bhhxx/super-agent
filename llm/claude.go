package llm

import (
	"context"
	"encoding/json"
	"os"

	"super-agent/runtime"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type ClaudeModel struct {
	client anthropic.Client
	model  string
}

func NewClaude() *ClaudeModel {
	return NewClaudeModel(Config{
		BaseURL: os.Getenv("ANTHROPIC_BASE_URL"),
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:   envDefault("ANTHROPIC_MODEL", "claude-3-7-sonnet-20250219"),
	})
}

func NewClaudeModel(cfg Config) *ClaudeModel {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	client := anthropic.NewClient(opts...)
	return &ClaudeModel{
		client: client,
		model:  cfg.Model,
	}
}

func (m *ClaudeModel) Next(ctx context.Context, messages []runtime.Message, tools []runtime.ToolSpec, chunkFunc func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(m.model),
		MaxTokens: int64(8192),
		Messages:  toClaudeMessages(messages),
	}

	if len(tools) > 0 {
		params.Tools = toClaudeTools(tools)
	}

	stream := m.client.Messages.NewStreaming(ctx, params)
	var finalAnswer string
	var reasoningContent string
	var toolCalls []runtime.ToolCall
	var currentToolUseID string
	var currentToolUseName string
	var currentToolUseInput string

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentToolUseID = event.ContentBlock.ID
				currentToolUseName = event.ContentBlock.Name
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				finalAnswer += event.Delta.Text
				if chunkFunc != nil {
					chunkFunc(runtime.StreamChunk{
						ContentDelta: event.Delta.Text,
					})
				}
			case "thinking_delta":
				reasoningContent += event.Delta.Thinking
				if chunkFunc != nil {
					chunkFunc(runtime.StreamChunk{
						ReasoningContentDelta: event.Delta.Thinking,
					})
				}
			case "input_json_delta":
				currentToolUseInput += event.Delta.PartialJSON
			}
		case "content_block_stop":
			if currentToolUseID != "" {
				toolCalls = append(toolCalls, runtime.ToolCall{
					ID:    currentToolUseID,
					Name:  currentToolUseName,
					Input: currentToolUseInput,
				})
				currentToolUseID = ""
				currentToolUseName = ""
				currentToolUseInput = ""
			}
		}
	}

	if err := stream.Err(); err != nil {
		return runtime.ModelResponse{}, err
	}

	if len(toolCalls) > 0 {
		return runtime.ModelResponse{
			Content:          finalAnswer,
			ReasoningContent: reasoningContent,
			ToolCalls:        toolCalls,
		}, nil
	}

	return runtime.ModelResponse{
		Content:          finalAnswer,
		ReasoningContent: reasoningContent,
	}, nil
}

func toClaudeMessages(messages []runtime.Message) []anthropic.MessageParam {
	var result []anthropic.MessageParam
	for _, msg := range messages {
		switch msg.Role {
		case runtime.RoleUser:
			result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case runtime.RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					var inputMap interface{}
					if err := json.Unmarshal([]byte(tc.Input), &inputMap); err != nil {
						inputMap = map[string]interface{}{}
					}
					blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, inputMap, tc.Name))
				}
			}
			if len(blocks) > 0 {
				result = append(result, anthropic.MessageParam{
					Role:    anthropic.MessageParamRole("assistant"),
					Content: blocks,
				})
			}
		case runtime.RoleTool:
			result = append(result, anthropic.MessageParam{
				Role: anthropic.MessageParamRole("user"),
				Content: []anthropic.ContentBlockParamUnion{
					anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false),
				},
			})
		}
	}

	return mergeAdjacentMessages(result)
}

func mergeAdjacentMessages(messages []anthropic.MessageParam) []anthropic.MessageParam {
	if len(messages) == 0 {
		return messages
	}
	var merged []anthropic.MessageParam
	var current anthropic.MessageParam

	for i, msg := range messages {
		if i == 0 {
			current = msg
			continue
		}
		if current.Role == msg.Role {
			current.Content = append(current.Content, msg.Content...)
		} else {
			merged = append(merged, current)
			current = msg
		}
	}
	merged = append(merged, current)
	return merged
}

func toClaudeTools(tools []runtime.ToolSpec) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam
	for _, t := range tools {
		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: t.Parameters["properties"],
					Required:   interfaceToStringSlice(t.Parameters["required"]),
				},
			},
		})
	}
	return result
}

func interfaceToStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]interface{}); ok {
		var res []string
		for _, item := range arr {
			if str, ok := item.(string); ok {
				res = append(res, str)
			}
		}
		return res
	}
	if arr, ok := v.([]string); ok {
		return arr
	}
	return nil
}
