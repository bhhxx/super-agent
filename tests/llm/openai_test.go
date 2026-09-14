package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	model := NewOpenAI(ProviderConfig{
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
	if resp.Content != "hello from llm" {
		t.Fatalf("Content = %q", resp.Content)
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

func TestOpenAIModelSendsSystemMessage(t *testing.T) {
	var requestBody struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	_, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleSystem, Content: "project instructions"},
		{Role: runtime.RoleUser, Content: "hi"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if len(requestBody.Messages) != 2 {
		t.Fatalf("messages = %+v, want two", requestBody.Messages)
	}
	if requestBody.Messages[0].Role != "system" || requestBody.Messages[0].Content != "project instructions" {
		t.Fatalf("system message = %+v", requestBody.Messages[0])
	}
}

func TestOpenAIModelSendsImageAndFileAttachments(t *testing.T) {
	var content []struct {
		Type     string `json:"type"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
		File struct {
			Filename string `json:"filename"`
			FileData string `json:"file_data"`
		} `json:"file"`
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
		if err := json.Unmarshal(body.Messages[0].Content, &content); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "key", Model: "model"})
	_, err := model.Next(context.Background(), []runtime.Message{{Role: runtime.RoleUser, Content: "inspect", Attachments: []runtime.Attachment{{Name: "pixel.png", MIME: "image/png", Data: "aW1hZ2U="}, {Name: "note.txt", MIME: "text/plain", Data: "dGV4dA=="}}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 3 || !strings.HasPrefix(content[1].ImageURL.URL, "data:image/png;base64,") || content[2].File.Filename != "note.txt" || content[2].File.FileData != "dGV4dA==" {
		t.Fatalf("content = %+v", content)
	}
}

func TestOpenAIModelUsesSDKDefaultBaseURLWhenConfigBaseURLIsEmpty(t *testing.T) {
	unsetEnv(t, "OPENAI_BASE_URL")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var sawDefaultBaseURL bool
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme == "https" && req.URL.Host == "api.openai.com" && req.URL.Path == "/v1/chat/completions" {
			sawDefaultBaseURL = true
		}
		body := io.NopCloser(strings.NewReader("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       body,
			Request:    req,
		}, nil
	})

	model := NewOpenAI(ProviderConfig{
		APIKey: "test-key",
		Model:  "test-model",
	})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "hi"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if !sawDefaultBaseURL {
		t.Fatal("request did not use OpenAI SDK default base URL")
	}
	if resp.Content != "ok" {
		t.Fatalf("Content = %q, want ok", resp.Content)
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

	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestOpenAIModelLeavesToolRiskToRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\": \"chatcmpl-test\", \"object\": \"chat.completion.chunk\", \"created\": 1, \"model\": \"test-model\", \"choices\": [{\"index\": 0, \"delta\": {\"role\": \"assistant\", \"content\": \"\", \"tool_calls\": [{\"index\": 0, \"id\": \"call_1\", \"type\": \"function\", \"function\": {\"name\": \"bash\", \"arguments\": \"{\\\"command\\\":\\\"ls\\\"}\"}}]}, \"finish_reason\": \"tool_calls\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "list files"},
	}, []runtime.ToolSpec{{Name: "bash", Risky: true}}, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "bash" {
		t.Fatalf("ToolCalls = %+v", resp.ToolCalls)
	}
}

func TestOpenAIModelReturnsAllToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"first\",\"arguments\":\"{}\"}},{\"index\":1,\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"second\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
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
}

func TestOpenAIModelFailsOnTruncatedToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"main.go\\\",\\\"content\\\":\\\"package\"}}]},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{
		{Role: runtime.RoleUser, Content: "write the file"},
	}, []runtime.ToolSpec{{Name: "write_file", Risky: true}}, nil)
	if err == nil {
		t.Fatalf("Next succeeded with %+v, want a truncation error: a half-emitted tool call must not reach the executor", resp)
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("error = %v, want a truncation error", err)
	}
}

func TestOpenAIModelReportsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !body.StreamOptions.IncludeUsage {
			t.Fatal("request did not set stream_options.include_usage")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	model := NewOpenAI(ProviderConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model"})
	resp, err := model.Next(context.Background(), []runtime.Message{{Role: runtime.RoleUser, Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 || resp.Usage.TotalTokens != 18 {
		t.Fatalf("Usage = %+v, want provider-reported counts", resp.Usage)
	}
}

func TestOpenAIModelSkipsAuthHeaderWithoutAPIKey(t *testing.T) {
	unsetEnv(t, "OPENAI_API_KEY")
	sawAuthHeader := false
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "" {
			sawAuthHeader = true
		}
		body := io.NopCloser(strings.NewReader("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       body,
			Request:    req,
		}, nil
	})

	model := NewOpenAI(ProviderConfig{Model: "test-model"})
	if _, err := model.Next(context.Background(), []runtime.Message{{Role: runtime.RoleUser, Content: "hi"}}, nil, nil); err != nil {
		t.Fatalf("Next failed: %v", err)
	}
	if sawAuthHeader {
		t.Fatal("request sent an Authorization header without a configured API key; the SDK env fallback must stay intact")
	}
}
