package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"super-agent/runtime/protocol"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type ClaudeModel struct {
	client anthropic.Client
	model  string
}

func NewClaude(cfg ProviderConfig) *ClaudeModel {
	cfg = withDefaults(cfg, ProviderConfig{Model: "claude-3-7-sonnet-20250219"})
	return newClaudeModel(cfg)
}

func newClaudeModel(cfg ProviderConfig) *ClaudeModel {
	opts := []option.RequestOption{
		option.WithHTTPClient(httpClient()),
	}
	// An empty key must not shadow the SDK's environment fallback
	// (ANTHROPIC_API_KEY), so only send a header when configured.
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
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

func (m *ClaudeModel) Next(ctx context.Context, messages []protocol.Message, tools []protocol.ToolSpec, chunkFunc func(protocol.StreamChunk)) (protocol.ModelResponse, error) {
	system, conversation := splitSystemMessages(messages)
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(m.model),
		MaxTokens: int64(8192),
		Messages:  toClaudeMessages(conversation),
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	if len(tools) > 0 {
		params.Tools = toClaudeTools(tools)
	}

	stream := m.client.Messages.NewStreaming(ctx, params)
	var finalAnswer string
	var reasoningContent string
	var toolCalls []protocol.ToolCall
	var currentToolUseID string
	var currentToolUseName string
	var currentToolUseInput string
	var stopReason anthropic.StopReason
	var usage protocol.Usage

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "message_start":
			usage.InputTokens = event.Message.Usage.InputTokens
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
					chunkFunc(protocol.StreamChunk{
						ContentDelta: event.Delta.Text,
					})
				}
			case "thinking_delta":
				reasoningContent += event.Delta.Thinking
				if chunkFunc != nil {
					chunkFunc(protocol.StreamChunk{
						ReasoningContentDelta: event.Delta.Thinking,
					})
				}
			case "input_json_delta":
				currentToolUseInput += event.Delta.PartialJSON
			}
		case "content_block_stop":
			if currentToolUseID != "" {
				toolCalls = append(toolCalls, protocol.ToolCall{
					ID:    currentToolUseID,
					Name:  currentToolUseName,
					Input: currentToolUseInput,
				})
				currentToolUseID = ""
				currentToolUseName = ""
				currentToolUseInput = ""
			}
		case "message_delta":
			stopReason = event.Delta.StopReason
			usage.OutputTokens = event.Usage.OutputTokens
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
	}

	if err := stream.Err(); err != nil {
		return protocol.ModelResponse{}, err
	}

	// A max_tokens stop means the provider cut the response mid-stream. A
	// half-emitted input_json_delta would otherwise reach the tool executor
	// as if it were valid JSON, so fail loudly instead.
	if stopReason == "max_tokens" {
		return protocol.ModelResponse{}, errors.New("llm output truncated by the token limit (stop_reason=max_tokens); raise the model's max output or shorten the conversation")
	}

	response := protocol.ModelResponse{
		Content:          finalAnswer,
		ReasoningContent: reasoningContent,
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		response.Usage = &usage
	}
	if len(toolCalls) > 0 {
		response.ToolCalls = toolCalls
	}
	return response, nil
}

func splitSystemMessages(messages []protocol.Message) (string, []protocol.Message) {
	var system string
	conversation := make([]protocol.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == protocol.RoleSystem {
			if system != "" {
				system += "\n\n"
			}
			system += msg.Content
			continue
		}
		conversation = append(conversation, msg)
	}
	return system, conversation
}

func toClaudeMessages(messages []protocol.Message) []anthropic.MessageParam {
	var result []anthropic.MessageParam
	for _, msg := range messages {
		switch msg.Role {
		case protocol.RoleUser:
			blocks := []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)}
			for _, attachment := range msg.Attachments {
				switch {
				case strings.HasPrefix(attachment.MIME, "image/"):
					blocks = append(blocks, anthropic.NewImageBlockBase64(attachment.MIME, attachment.Data))
				case attachment.MIME == "application/pdf":
					blocks = append(blocks, anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{Data: attachment.Data}))
				case strings.HasPrefix(attachment.MIME, "text/"):
					content, err := base64.StdEncoding.DecodeString(attachment.Data)
					if err == nil {
						blocks = append(blocks, anthropic.NewTextBlock("Attachment "+attachment.Name+":\n"+string(content)))
					}
				default:
					blocks = append(blocks, anthropic.NewTextBlock("Attached file: "+attachment.Name+" ("+attachment.MIME+")"))
				}
			}
			result = append(result, anthropic.NewUserMessage(blocks...))
		case protocol.RoleAssistant:
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
		case protocol.RoleTool:
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

// toClaudeTools passes the whole tool schema through: known keys populate
// the typed fields and every other top-level keyword ($defs, $ref, oneOf,
// …) rides along via ExtraFields. Cherry-picking properties/required here
// silently degraded MCP schemas that rely on shared definitions.
func toClaudeTools(tools []protocol.ToolSpec) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam
	for _, t := range tools {
		schema := anthropic.ToolInputSchemaParam{}
		extras := map[string]any{}
		for key, value := range t.Parameters {
			switch key {
			case "properties":
				schema.Properties = value
			case "required":
				schema.Required = interfaceToStringSlice(value)
			case "type":
				// Anthropic custom tools require the root type "object",
				// which is the marshaled default; nothing to override.
			default:
				extras[key] = value
			}
		}
		if len(extras) > 0 {
			schema.ExtraFields = extras
		}
		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: schema,
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
