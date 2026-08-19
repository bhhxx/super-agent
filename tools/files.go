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

	runtime "super-agent/runtime/protocol"
)

const maxToolOutputLines = 200

type ReadFileTool struct{}
type ListFilesTool struct{}
type SearchTool struct{}
type ApplyPatchTool struct{}
type WriteFileTool struct{}

func (ReadFileTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "read_file",
		Description: "Read a workspace file, optionally with start_line and end_line.",
		Parameters: objectSchema(map[string]any{
			"path":       map[string]any{"type": "string"},
			"start_line": map[string]any{"type": "integer"},
			"end_line":   map[string]any{"type": "integer"},
		}, []string{"path"}),
	}
}

func (ReadFileTool) Run(_ context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	path, rel, err := workspacePath(args.Path)
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

func (ListFilesTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "list_files",
		Description: "List workspace files under path, optionally filtered by glob pattern.",
		Parameters: objectSchema(map[string]any{
			"path":    map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string"},
		}, nil),
	}
}

func (ListFilesTool) Run(_ context.Context, call runtime.ToolCall) (string, error) {
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
	root, _, err := workspacePath(args.Path)
	if err != nil {
		return "", err
	}
	files, err := collectFiles(root, args.Pattern)
	if err != nil {
		return "", err
	}
	return strings.Join(limitLines(files), "\n"), nil
}

func (SearchTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "search",
		Description: "Search text in workspace files. Query is a regular expression.",
		Parameters: objectSchema(map[string]any{
			"query": map[string]any{"type": "string"},
			"path":  map[string]any{"type": "string"},
		}, []string{"query"}),
	}
}

func (SearchTool) Run(_ context.Context, call runtime.ToolCall) (string, error) {
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
	root, _, err := workspacePath(args.Path)
	if err != nil {
		return "", err
	}
	matches, err := searchFiles(root, re)
	if err != nil {
		return "", err
	}
	return strings.Join(limitLines(matches), "\n"), nil
}

func (ApplyPatchTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
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

func (ApplyPatchTool) Run(_ context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Path       string `json:"path"`
		OldText    string `json:"old_text"`
		NewText    string `json:"new_text"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	path, rel, err := workspacePath(args.Path)
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

func (WriteFileTool) Spec() runtime.ToolSpec {
	return runtime.ToolSpec{
		Name:        "write_file",
		Description: "Write content to a workspace file, creating parent directories.",
		Risky:       true,
		Parameters: objectSchema(map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}, []string{"path", "content"}),
	}
}

func (WriteFileTool) Run(_ context.Context, call runtime.ToolCall) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeArgs(call.Input, &args); err != nil {
		return "", err
	}
	path, rel, err := workspacePath(args.Path)
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

func workspacePath(path string) (string, string, error) {
	if path == "" {
		return "", "", errors.New("path is required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("path is outside working directory")
	}
	return abs, filepath.ToSlash(rel), nil
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

func collectFiles(root, pattern string) ([]string, error) {
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
		_, rel, err := workspacePath(path)
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

func searchFiles(root string, re *regexp.Regexp) ([]string, error) {
	files, err := collectFiles(root, "")
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, rel := range files {
		path, _, err := workspacePath(rel)
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
