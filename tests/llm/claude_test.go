package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "super-agent/llm"
	"super-agent/runtime"
)

func TestClaudeModelSendsSystemMessage(t *testing.T) {
	var requestBody struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"test-model\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	model := NewClaude(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	_, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleSystem, Content: "project instructions"},
		{Role: runtime.RoleUser, Content: "hi"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if len(requestBody.System) != 1 || requestBody.System[0].Text != "project instructions" {
		t.Fatalf("system = %+v", requestBody.System)
	}
	if len(requestBody.Messages) != 1 || requestBody.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", requestBody.Messages)
	}
}

func TestClaudeModelSendsImageAttachment(t *testing.T) {
	var types []struct {
		Type string `json:"type"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body.Messages[0].Content, &types); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	model := NewClaude(ProviderConfig{BaseURL: server.URL, APIKey: "key", Model: "model"})
	_, err := model.Next(context.Background(), []runtime.Message{{Role: runtime.RoleUser, Content: "inspect", Attachments: []runtime.Attachment{{Name: "pixel.png", MIME: "image/png", Data: "aW1hZ2U="}}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 || types[0].Type != "text" || types[1].Type != "image" {
		t.Fatalf("content types = %+v", types)
	}
}

func TestClaudeModelFailsOnMaxTokensStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Real Anthropic streams carry an event: line per SSE event; without
		// it the SDK decoder silently yields no events at all.
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"write_file\",\"input\":{}}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"main.go\\\"\"}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":8192}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	model := NewClaude(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "write the file"},
	}, []runtime.ToolSpec{{Name: "write_file", Risky: true}}, nil)
	if err == nil {
		t.Fatalf("Next succeeded with %+v, want a truncation error: a half-emitted tool input must not reach the executor", resp)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("error = %v, want a truncation error", err)
	}
}

func TestClaudeModelReportsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":7}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	model := NewClaude(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{{Role: runtime.RoleUser, Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 || resp.Usage.TotalTokens != 18 {
		t.Fatalf("Usage = %+v, want provider-reported counts", resp.Usage)
	}
}

func TestClaudeModelPassesThroughSchemaExtras(t *testing.T) {
	var requestBody struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	model := NewClaude(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	spec := runtime.ToolSpec{
		Name: "mcp_tool", Risky: true,
		Parameters: map[string]any{
			"type": "object",
			"$defs": map[string]any{
				"filters": map[string]any{"type": "object", "properties": map[string]any{"tag": map[string]any{"type": "string"}}},
			},
			"properties": map[string]any{
				"filter": map[string]any{"$ref": "#/$defs/filters"},
			},
			"required": []string{"filter"},
		},
	}
	if _, err := model.Next(context.Background(), []runtime.Message{{Role: runtime.RoleUser, Content: "hi"}}, []runtime.ToolSpec{spec}, nil); err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if len(requestBody.Tools) != 1 || requestBody.Tools[0].Name != "mcp_tool" {
		t.Fatalf("tools = %+v", requestBody.Tools)
	}
	schema := requestBody.Tools[0].InputSchema
	if _, ok := schema["$defs"]; !ok {
		t.Fatalf("input_schema = %+v, want $defs passed through instead of dropped", schema)
	}
	if _, ok := schema["properties"]; !ok {
		t.Fatalf("input_schema = %+v, want properties preserved", schema)
	}
}
