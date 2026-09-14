package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestDependencyRule(t *testing.T) {
	root := repositoryRoot(t)
	assertImportsRecursive(t, filepath.Join(root, "tui"), func(path string) bool {
		return !strings.HasPrefix(path, "super-agent/runtime")
	}, "TUI must depend on its Conversation port, not runtime")

	for _, directory := range []string{"llm", "tools"} {
		assertImports(t, filepath.Join(root, directory), func(path string) bool {
			return !strings.HasPrefix(path, "super-agent/runtime") || path == "super-agent/runtime/protocol"
		}, directory+" may depend only on runtime ports")
	}
	for _, directory := range []string{"store", "workspace", "project"} {
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

func TestTUIFeaturesDoNotImportEachOther(t *testing.T) {
	root := repositoryRoot(t)
	features, err := filepath.Glob(filepath.Join(root, "tui", "*"))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, feature := range features {
		info, err := os.Stat(feature)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			continue
		}
		found++
		assertImportsRecursive(t, feature, func(path string) bool {
			return !strings.HasPrefix(path, "super-agent/tui/")
		}, "TUI features must collaborate through typed messages, not feature imports")
	}
	if found == 0 {
		t.Fatal("no TUI feature directories found")
	}
}

func assertImports(t *testing.T, directory string, allowed func(string) bool, rule string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		assertFileImports(t, file, allowed, rule)
	}
}

func assertImportsRecursive(t *testing.T, directory string, allowed func(string) bool, rule string) {
	t.Helper()
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		assertFileImports(t, path, allowed, rule)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertFileImports(t *testing.T, file string, allowed func(string) bool, rule string) {
	t.Helper()
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
