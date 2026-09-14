package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"super-agent/runtime/protocol"
)

const maxToolOutputLines = 200

type ReadFileTool struct{ workspace WorkspaceContext }
type ListFilesTool struct{ workspace WorkspaceContext }
type SearchTool struct{ workspace WorkspaceContext }
type ApplyPatchTool struct{ workspace WorkspaceContext }
type WriteFileTool struct{ workspace WorkspaceContext }

func (ReadFileTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "read_file",
		Description: "Read a workspace file, optionally with start_line and end_line.",
		Parameters: objectSchema(map[string]any{
			"path":       map[string]any{"type": "string"},
			"start_line": map[string]any{"type": "integer"},
			"end_line":   map[string]any{"type": "integer"},
		}, []string{"path"}),
	}
}

func (t ReadFileTool) Run(_ context.Context, call protocol.ToolCall) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	path, rel, err := resolveReadable(t.workspace, args.Path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if isBinary(content) {
		return "", fmt.Errorf("refusing to read binary file: %s", rel)
	}
	return numberedLines(string(content), args.StartLine, args.EndLine), nil
}

func (ListFilesTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "list_files",
		Description: "List workspace files under path, optionally filtered by glob pattern.",
		Parameters: objectSchema(map[string]any{
			"path":    map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string"},
		}, nil),
	}
}

func (t ListFilesTool) Run(_ context.Context, call protocol.ToolCall) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if call.Input != "" {
		if err := decodeArgs(call.Input, &args); err != nil {
			return "", err
		}
	}
	if args.Path == "" {
		args.Path = "."
	}
	root, _, err := resolveReadable(t.workspace, args.Path)
	if err != nil {
		return "", err
	}
	files, err := collectFiles(t.workspace, root, args.Pattern)
	if err != nil {
		return "", err
	}
	return strings.Join(limitLines(files), "\n"), nil
}

func (SearchTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "search",
		Description: "Search text in workspace files. Query is a regular expression.",
		Parameters: objectSchema(map[string]any{
			"query": map[string]any{"type": "string"},
			"path":  map[string]any{"type": "string"},
		}, []string{"query"}),
	}
}

func (t SearchTool) Run(_ context.Context, call protocol.ToolCall) (string, error) {
	var args struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	if args.Query == "" {
		return "", errors.New("query is required")
	}
	if args.Path == "" {
		args.Path = "."
	}
	re, err := regexp.Compile(args.Query)
	if err != nil {
		return "", err
	}
	root, _, err := resolveReadable(t.workspace, args.Path)
	if err != nil {
		return "", err
	}
	matches, err := searchFiles(t.workspace, root, re)
	if err != nil {
		return "", err
	}
	return strings.Join(limitLines(matches), "\n"), nil
}

func (ApplyPatchTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "apply_patch",
		Description: "Replace old_text with new_text in a workspace file.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"path":        map[string]any{"type": "string"},
			"old_text":    map[string]any{"type": "string"},
			"new_text":    map[string]any{"type": "string"},
			"replace_all": map[string]any{"type": "boolean"},
		}, []string{"path", "old_text", "new_text"}),
	}
}

func (t ApplyPatchTool) Run(_ context.Context, call protocol.ToolCall) (string, error) {
	var args struct {
		Path       string `json:"path"`
		OldText    string `json:"old_text"`
		NewText    string `json:"new_text"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	path, rel, err := resolveWritable(t.workspace, args.Path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(content)
	if !strings.Contains(text, args.OldText) {
		return "", errors.New("old_text not found")
	}
	count := 1
	if args.ReplaceAll {
		count = -1
	}
	updated := strings.Replace(text, args.OldText, args.NewText, count)
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return "", err
	}
	return "patched " + rel, nil
}

func (WriteFileTool) Spec() protocol.ToolSpec {
	return protocol.ToolSpec{
		Name:        "write_file",
		Description: "Write content to a workspace file, creating parent directories.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}, []string{"path", "content"}),
	}
}

func (t WriteFileTool) Run(_ context.Context, call protocol.ToolCall) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	path, rel, err := resolveWritable(t.workspace, args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return "", err
	}
	return "wrote " + rel, nil
}

func decodeArgs(input string, target any) error {
	if err := json.Unmarshal([]byte(input), target); err != nil {
		return errors.New("invalid JSON input")
	}
	return nil
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func numberedLines(content string, start, end int) string {
	content = strings.TrimSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > end || start > len(lines) {
		return ""
	}
	var out []string
	for i := start; i <= end; i++ {
		out = append(out, fmt.Sprintf("%d: %s", i, lines[i-1]))
	}
	return strings.Join(limitLines(out), "\n")
}

func collectFiles(workspace WorkspaceContext, root, pattern string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		_, rel, err := resolveReadable(workspace, path)
		if err != nil {
			return err
		}
		if pattern != "" {
			ok, err := filepath.Match(pattern, filepath.Base(path))
			if err != nil || !ok {
				return err
			}
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func searchFiles(workspace WorkspaceContext, root string, re *regexp.Regexp) ([]string, error) {
	files, err := collectFiles(workspace, root, "")
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, rel := range files {
		path, _, err := resolveReadable(workspace, rel)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if isBinary(content) {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		lineNo := 1
		for scanner.Scan() {
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, lineNo, line))
			}
			lineNo++
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func isBinary(content []byte) bool {
	return strings.Contains(string(content), "\x00")
}

func limitLines(lines []string) []string {
	if len(lines) <= maxToolOutputLines {
		return lines
	}
	return append(lines[:maxToolOutputLines], "... truncated")
}
