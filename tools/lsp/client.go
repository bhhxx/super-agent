package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"super-agent/runtime/protocol"
)

type ServerConfig struct {
	Name, Command, LanguageID, Root string
	Args, Extensions                []string
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type client struct {
	config      ServerConfig
	process     *exec.Cmd
	stdin       io.WriteCloser
	mu          sync.Mutex
	writeMu     sync.Mutex
	pending     map[int64]chan response
	diagnostics map[string]json.RawMessage
	nextID      atomic.Int64
	done        chan struct{}
	err         error
}

type WorkspaceContext interface {
	GetCWD() string
	ResolvePath(string) (string, error)
	CanRead(string) bool
}

type Manager struct {
	mu        sync.RWMutex
	workspace WorkspaceContext
	root      string
	configs   []ServerConfig
	clients   []*client
}

func Connect(ctx context.Context, workspace WorkspaceContext, configs []ServerConfig) (*Manager, error) {
	manager := &Manager{workspace: workspace, configs: append([]ServerConfig(nil), configs...)}
	clients, err := connectClients(ctx, manager.configs, workspace.GetCWD())
	if err != nil {
		return nil, err
	}
	manager.root = workspace.GetCWD()
	manager.clients = clients
	return manager, nil
}

func connectClients(ctx context.Context, configs []ServerConfig, root string) ([]*client, error) {
	clients := make([]*client, 0, len(configs))
	for _, config := range configs {
		config.Root = root
		connected, err := start(ctx, config)
		if err != nil {
			for _, client := range clients {
				_ = client.Close()
			}
			return nil, fmt.Errorf("connect LSP %s: %w", config.Name, err)
		}
		clients = append(clients, connected)
	}
	return clients, nil
}

func start(ctx context.Context, config ServerConfig) (*client, error) {
	if strings.TrimSpace(config.Name) == "" || strings.TrimSpace(config.Command) == "" {
		return nil, errors.New("name and command are required")
	}
	command := exec.CommandContext(context.WithoutCancel(ctx), config.Command, config.Args...)
	command.Dir = config.Root
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Server stderr is dropped, not forwarded: the TUI owns the terminal, and
	// a chatty language server (gopls, rust-analyzer) would garble it.
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, err
	}
	client := &client{config: config, process: command, stdin: stdin, pending: map[int64]chan response{}, diagnostics: map[string]json.RawMessage{}, done: make(chan struct{})}
	go client.readLoop(stdout)
	rootURI := fileURI(config.Root)
	if _, err := client.request(ctx, "initialize", map[string]any{"processId": os.Getpid(), "rootUri": rootURI, "capabilities": map[string]any{}}); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *client) readLoop(reader io.Reader) {
	buffer := bufio.NewReader(reader)
	defer close(c.done)
	for {
		length, err := readHeader(buffer)
		if err != nil {
			c.fail(err)
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(buffer, body); err != nil {
			c.fail(err)
			return
		}
		var envelope struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			continue
		}
		if envelope.ID != nil {
			c.mu.Lock()
			channel := c.pending[*envelope.ID]
			delete(c.pending, *envelope.ID)
			c.mu.Unlock()
			if channel != nil {
				channel <- response{Result: envelope.Result, Error: envelope.Error}
			}
			continue
		}
		if envelope.Method == "textDocument/publishDiagnostics" {
			var params struct {
				URI         string          `json:"uri"`
				Diagnostics json.RawMessage `json:"diagnostics"`
			}
			if json.Unmarshal(envelope.Params, &params) == nil {
				c.mu.Lock()
				c.diagnostics[params.URI] = params.Diagnostics
				c.mu.Unlock()
			}
		}
	}
}

func readHeader(reader *bufio.Reader) (int, error) {
	const maxMessageBytes = 32 << 20
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			length, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:")))
			if err != nil {
				return 0, err
			}
		}
	}
	if length <= 0 {
		return 0, errors.New("invalid LSP content length")
	}
	// The length is server-controlled; cap it so a buggy or hostile server
	// cannot make the agent allocate unbounded memory.
	if length > maxMessageBytes {
		return 0, fmt.Errorf("LSP message of %d bytes exceeds the %d byte limit", length, maxMessageBytes)
	}
	return length, nil
}

func (c *client) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	channel := make(chan response, 1)
	c.mu.Lock()
	c.pending[id] = channel
	c.mu.Unlock()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case reply := <-channel:
		if reply.Error != nil {
			return nil, fmt.Errorf("LSP %s: %s", method, reply.Error.Message)
		}
		return reply.Result, nil
	case <-c.done:
		return nil, firstError(c.err, errors.New("LSP server stopped"))
	case <-ctx.Done():
		// Drop the registration; the buffered channel means a late reply
		// from the read loop cannot block.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *client) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// writeTimeout bounds one wire write. A language server that stops reading
// its stdin would otherwise block the writer forever while holding the
// write lock, wedging every future request before its context could fire.
const writeTimeout = 30 * time.Second

func (c *client) write(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	// A dedicated write lock keeps message order on the wire without
	// sharing the pending-map mutex, so bookkeeping stays responsive.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	written := make(chan error, 1)
	go func() {
		_, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body)
		written <- err
	}()
	select {
	case err := <-written:
		return err
	case <-time.After(writeTimeout):
		// The pipe is wedged; this client is unrecoverable. Poison it so
		// later requests fail fast instead of piling onto the dead pipe.
		c.fail(errors.New("LSP server write timed out"))
		return errors.New("LSP server write timed out")
	}
}

func (c *client) fail(err error) { c.mu.Lock(); c.err = err; c.mu.Unlock() }

func (c *client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = c.request(ctx, "shutdown", nil)
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()
	if c.process.Process != nil {
		_ = c.process.Process.Kill()
	}
	return c.process.Wait()
}

func (m *Manager) Close() error {
	m.mu.Lock()
	clients := m.clients
	m.clients = nil
	m.mu.Unlock()
	var result error
	for _, client := range clients {
		result = errors.Join(result, client.Close())
	}
	return result
}

func (m *Manager) Tools() []Tool {
	if len(m.configs) == 0 {
		return nil
	}
	names := []string{"lsp_diagnostics", "lsp_symbols", "lsp_definition", "lsp_references", "lsp_outline"}
	result := make([]Tool, 0, len(names))
	for _, name := range names {
		result = append(result, Tool{name: name, manager: m})
	}
	return result
}

func (m *Manager) ensureWorkspace(ctx context.Context) error {
	desired := m.workspace.GetCWD()
	m.mu.RLock()
	current := m.root
	m.mu.RUnlock()
	if desired == current {
		return nil
	}
	clients, err := connectClients(ctx, m.configs, desired)
	if err != nil {
		return err
	}
	m.mu.Lock()
	old := m.clients
	m.clients = clients
	m.root = desired
	m.mu.Unlock()
	for _, client := range old {
		_ = client.Close()
	}
	return nil
}

func (m *Manager) clientsSnapshot() []*client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]*client(nil), m.clients...)
}

func (m *Manager) clientFor(clients []*client, path string) (*client, string, error) {
	absolute, err := m.workspace.ResolvePath(path)
	if err != nil {
		return nil, "", err
	}
	// ResolvePath canonicalizes symlinks, so the containment check below runs
	// on the real target rather than a lexical path that a workspace symlink
	// could point outside.
	if !m.workspace.CanRead(absolute) {
		return nil, "", errors.New("path is outside readable workspace roots")
	}
	extension := strings.TrimPrefix(filepath.Ext(absolute), ".")
	for _, client := range clients {
		for _, supported := range client.config.Extensions {
			if strings.TrimPrefix(supported, ".") == extension {
				return client, absolute, nil
			}
		}
	}
	return nil, "", errors.New("no LSP server configured for ." + extension)
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

type Tool struct {
	name    string
	manager *Manager
}

func (t Tool) Spec() protocol.ToolSpec {
	properties := map[string]any{"path": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer"}, "column": map[string]any{"type": "integer"}, "query": map[string]any{"type": "string"}}
	required := []string{"path"}
	if t.name == "lsp_symbols" {
		required = []string{"query"}
	}
	return protocol.ToolSpec{Name: t.name, Description: "Query the configured language server.", Parameters: map[string]any{"type": "object", "properties": properties, "required": required}}
}

func (t Tool) Run(ctx context.Context, call protocol.ToolCall) (string, error) {
	var input struct {
		Path, Query  string
		Line, Column int
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return "", err
	}
	if err := t.manager.ensureWorkspace(ctx); err != nil {
		return "", err
	}
	clients := t.manager.clientsSnapshot()
	if t.name == "lsp_symbols" {
		if len(clients) == 0 {
			return "", errors.New("no LSP servers configured")
		}
		result, err := clients[0].request(ctx, "workspace/symbol", map[string]any{"query": input.Query})
		return pretty(result), err
	}
	client, path, err := t.manager.clientFor(clients, input.Path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	uri := fileURI(path)
	if err := client.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": client.config.LanguageID, "version": 1, "text": string(content)}}); err != nil {
		return "", err
	}
	textDocument := map[string]any{"uri": uri}
	position := map[string]any{"line": max(0, input.Line-1), "character": max(0, input.Column-1)}
	var result json.RawMessage
	switch t.name {
	case "lsp_diagnostics":
		deadline := time.NewTimer(300 * time.Millisecond)
		defer deadline.Stop()
		select {
		case <-deadline.C:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		client.mu.Lock()
		result = append(json.RawMessage(nil), client.diagnostics[uri]...)
		client.mu.Unlock()
		if len(result) == 0 {
			result = json.RawMessage("[]")
		}
	case "lsp_outline":
		result, err = client.request(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": textDocument})
	case "lsp_definition":
		result, err = client.request(ctx, "textDocument/definition", map[string]any{"textDocument": textDocument, "position": position})
	case "lsp_references":
		result, err = client.request(ctx, "textDocument/references", map[string]any{"textDocument": textDocument, "position": position, "context": map[string]any{"includeDeclaration": true}})
	default:
		return "", errors.New("unknown LSP tool")
	}
	return pretty(result), err
}

func pretty(value json.RawMessage) string {
	if len(value) == 0 {
		return "null"
	}
	var output bytes.Buffer
	if json.Indent(&output, value, "", "  ") != nil {
		return string(value)
	}
	return output.String()
}
