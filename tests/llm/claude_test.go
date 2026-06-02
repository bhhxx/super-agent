package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	model := NewClaudeModel(Config{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
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
