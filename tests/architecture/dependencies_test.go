package architecture_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestDependencyRule(t *testing.T) {
	root := repositoryRoot(t)
	assertImports(t, filepath.Join(root, "tui"), func(path string) bool {
		return !strings.HasPrefix(path, "super-agent/runtime")
	}, "TUI must depend on its Conversation port, not runtime")

	for _, directory := range []string{"llm", "tools"} {
		assertImports(t, filepath.Join(root, directory), func(path string) bool {
			return !strings.HasPrefix(path, "super-agent/runtime") || path == "super-agent/runtime/protocol"
		}, directory+" may depend only on runtime ports")
	}
	for _, directory := range []string{"store", "workspace"} {
		assertImports(t, filepath.Join(root, directory), func(path string) bool {
			return !strings.HasPrefix(path, "super-agent/runtime") || path == "super-agent/runtime/protocol" || path == "super-agent/runtime/session"
		}, directory+" may depend only on runtime ports")
	}
	assertImports(t, filepath.Join(root, "runtime"), func(path string) bool {
		switch path {
		case "super-agent/store", "super-agent/workspace", "super-agent/llm", "super-agent/tools", "super-agent/tui":
			return false
		default:
			return true
		}
	}, "runtime facade must not depend on concrete adapters")

	assertImports(t, filepath.Join(root, "runtime", "session"), func(path string) bool {
		switch path {
		case "super-agent/store", "super-agent/tui", "os", "path/filepath":
			return false
		default:
			return true
		}
	}, "session must use repository and workspace ports")
}

func assertImports(t *testing.T, directory string, allowed func(string) bool, rule string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed(path) {
				t.Errorf("%s imports %q: %s", relative(rootForFile(file), file), path, rule)
			}
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func rootForFile(file string) string {
	directory := filepath.Dir(file)
	for filepath.Base(directory) != "super-agent" && filepath.Dir(directory) != directory {
		directory = filepath.Dir(directory)
	}
	return directory
}

func relative(root, path string) string {
	result, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return result
}
