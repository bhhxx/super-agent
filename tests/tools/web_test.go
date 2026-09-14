package tools_test

import (
	"context"
	"strings"
	"testing"

	"super-agent/runtime"
	. "super-agent/tools"
)

func TestBrowserRejectsPrivateAndNonHTTPAddresses(t *testing.T) {
	tool := BrowserFetchTool{}
	for _, input := range []string{`{"url":"http://127.0.0.1/private"}`, `{"url":"http://localhost/private"}`, `{"url":"file:///etc/passwd"}`} {
		_, err := tool.Run(context.Background(), runtime.ToolCall{Input: input})
		if err == nil || (!strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "HTTP(S)")) {
			t.Fatalf("input %s error = %v", input, err)
		}
	}
}

func TestWebToolsAreRiskyNetworkTools(t *testing.T) {
	registry := DefaultRegistry(testWorkspace(t))
	seen := map[string]bool{}
	for _, spec := range registry.Specs() {
		if spec.Name == "web_search" || spec.Name == "browser_fetch" {
			seen[spec.Name] = spec.Risky
		}
	}
	if !seen["web_search"] || !seen["browser_fetch"] {
		t.Fatalf("web specs = %+v", seen)
	}
}
