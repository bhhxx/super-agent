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

func TestOpenAIModelSendsChatCompletion(t *testing.T) {
	var requestBody struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\": \"chatcmpl-test\", \"object\": \"chat.completion.chunk\", \"created\": 1, \"model\": \"test-model\", \"choices\": [{\"index\": 0, \"delta\": {\"role\": \"assistant\", \"content\": \"hello from llm\"}}]}\n\ndata: {\"id\": \"chatcmpl-test\", \"object\": \"chat.completion.chunk\", \"created\": 1, \"model\": \"test-model\", \"choices\": [{\"index\": 0, \"delta\": {}, \"finish_reason\": \"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAIModel(Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	})

	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "hi"},
	}, []runtime.ToolSpec{{
		Name:        "bash",
		Description: "Run a bash command after user approval.",
		Risky:       true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if resp.FinalAnswer != "hello from llm" {
		t.Fatalf("FinalAnswer = %q", resp.FinalAnswer)
	}
	if requestBody.Model != "test-model" {
		t.Fatalf("request model = %q", requestBody.Model)
	}
	if len(requestBody.Messages) != 1 || requestBody.Messages[0].Role != "user" || requestBody.Messages[0].Content != "hi" {
		t.Fatalf("messages = %+v", requestBody.Messages)
	}
	if len(requestBody.Tools) != 1 || requestBody.Tools[0].Type != "function" || requestBody.Tools[0].Function.Name != "bash" {
		t.Fatalf("tools = %+v", requestBody.Tools)
	}
}

func TestOpenAIModelReplaysReasoningContent(t *testing.T) {
	var requestBody struct {
		Messages []map[string]any `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\": \"chatcmpl-test\", \"object\": \"chat.completion.chunk\", \"created\": 1, \"model\": \"test-model\", \"choices\": [{\"index\": 0, \"delta\": {\"role\": \"assistant\", \"content\": \"final\", \"reasoning_content\": \"thinking\"}}]}\n\ndata: {\"id\": \"chatcmpl-test\", \"object\": \"chat.completion.chunk\", \"created\": 1, \"model\": \"test-model\", \"choices\": [{\"index\": 0, \"delta\": {}, \"finish_reason\": \"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAIModel(Config{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleAssistant, Content: "old", ReasoningContent: "old thinking"},
		{Role: runtime.RoleUser, Content: "next"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if resp.ReasoningContent != "thinking" {
		t.Fatalf("ReasoningContent = %q", resp.ReasoningContent)
	}
	if got := requestBody.Messages[0]["reasoning_content"]; got != "old thinking" {
		t.Fatalf("replayed reasoning_content = %v", got)
	}
}

func TestOpenAIModelMarksRiskyToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\": \"chatcmpl-test\", \"object\": \"chat.completion.chunk\", \"created\": 1, \"model\": \"test-model\", \"choices\": [{\"index\": 0, \"delta\": {\"role\": \"assistant\", \"content\": \"\", \"tool_calls\": [{\"index\": 0, \"id\": \"call_1\", \"type\": \"function\", \"function\": {\"name\": \"bash\", \"arguments\": \"{\\\"command\\\":\\\"ls\\\"}\"}}]}, \"finish_reason\": \"tool_calls\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAIModel(Config{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "list files"},
	}, []runtime.ToolSpec{{Name: "bash", Risky: true}}, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if resp.ToolCall == nil || resp.ToolCall.Name != "bash" || !resp.ToolCall.Risky {
		t.Fatalf("ToolCall = %+v", resp.ToolCall)
	}
}

func TestOpenAIModelReturnsAllToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"first\",\"arguments\":\"{}\"}},{\"index\":1,\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"second\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAIModel(Config{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "use tools"},
	}, []runtime.ToolSpec{{Name: "first"}, {Name: "second", Risky: true}}, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %+v, want two", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Name != "first" || resp.ToolCalls[1].Name != "second" {
		t.Fatalf("ToolCalls = %+v, want first then second", resp.ToolCalls)
	}
	if !resp.ToolCalls[1].Risky {
		t.Fatalf("second call was not marked risky: %+v", resp.ToolCalls[1])
	}
}
