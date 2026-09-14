package tools_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"super-agent/runtime/protocol"
	lsptools "super-agent/tools/lsp"
	"super-agent/workspace"
)

func TestLSPToolsQueryConfiguredServer(t *testing.T) {
	if os.Getenv("SUPER_AGENT_LSP_HELPER") == "1" {
		runLSPHelper()
		os.Exit(0)
	}
	t.Setenv("SUPER_AGENT_LSP_HELPER", "1")
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workspaceContext, err := workspace.NewDefaultContext(root)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := lsptools.Connect(context.Background(), workspaceContext, []lsptools.ServerConfig{{
		Name: "fake", Command: os.Args[0], Args: []string{"-test.run=TestLSPToolsQueryConfiguredServer"}, Extensions: []string{"go"}, LanguageID: "go", Root: root,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	tools := manager.Tools()
	if len(tools) != 5 {
		t.Fatalf("tools = %d", len(tools))
	}
	for _, tool := range tools {
		input := `{"path":"main.go","line":1,"column":1,"query":"Main"}`
		result, err := tool.Run(context.Background(), protocol.ToolCall{Name: tool.Spec().Name, Input: input})
		if err != nil {
			t.Fatalf("%s failed: %v", tool.Spec().Name, err)
		}
		if result == "" || result == "null" {
			t.Fatalf("%s result = %q", tool.Spec().Name, result)
		}
	}
}

func runLSPHelper() {
	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readLSPMessage(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &request) != nil {
			continue
		}
		if request.Method == "exit" {
			return
		}
		if request.Method == "textDocument/didOpen" {
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(request.Params, &params)
			writeLSPMessage(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": params.TextDocument.URI, "diagnostics": []any{map[string]any{"message": "fake diagnostic"}}}})
			continue
		}
		if request.ID == nil {
			continue
		}
		result := any([]any{map[string]any{"name": "Main"}})
		if request.Method == "initialize" || request.Method == "shutdown" {
			result = map[string]any{"capabilities": map[string]any{}}
		}
		writeLSPMessage(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}
}

func readLSPMessage(reader *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			length, err = strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
			if err != nil {
				return nil, err
			}
		}
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func writeLSPMessage(message any) {
	body, _ := json.Marshal(message)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
}
