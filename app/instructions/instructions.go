package instructions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const MaxFileSize = 128 * 1024

type Source struct {
	Path string
	Kind string
}

type Bundle struct {
	Content string
	Sources []Source
}

func Load(cwd string) (Bundle, error) {
	var bundle Bundle
	home, err := os.UserHomeDir()
	if err != nil {
		return Bundle{}, err
	}
	userPath := filepath.Join(home, ".superagent", "AGENTS.md")
	if _, err := appendSource(&bundle, userPath, "user-memory"); err != nil {
		return Bundle{}, err
	}
	dirs, err := ancestorDirs(cwd)
	if err != nil {
		return Bundle{}, err
	}
	for _, dir := range dirs {
		agentPath := filepath.Join(dir, "AGENTS.md")
		if ok, err := appendSource(&bundle, agentPath, "project"); err != nil {
			return Bundle{}, err
		} else if ok {
			continue
		}
		claudePath := filepath.Join(dir, "CLAUDE.md")
		if _, err := appendSource(&bundle, claudePath, "claude-compat"); err != nil {
			return Bundle{}, err
		}
	}
	return bundle, nil
}

func appendSource(bundle *Bundle, path, kind string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	if info.Size() > MaxFileSize {
		return false, fmt.Errorf("instruction file %s is too large: %d bytes exceeds %d bytes", path, info.Size(), MaxFileSize)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := strings.TrimSpace(string(content))
	if text == "" {
		return true, nil
	}
	if bundle.Content != "" {
		bundle.Content += "\n\n"
	}
	bundle.Content += text
	bundle.Sources = append(bundle.Sources, Source{Path: path, Kind: kind})
	return true, nil
}

func ancestorDirs(cwd string) ([]string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	var reversed []string
	for {
		reversed = append(reversed, abs)
		if hasGitDir(abs) {
			break
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	reversed = trimAboveTopmostInstructionDir(reversed)
	dirs := make([]string, len(reversed))
	for i := range reversed {
		dirs[i] = reversed[len(reversed)-1-i]
	}
	return dirs, nil
}

func hasGitDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func trimAboveTopmostInstructionDir(reversed []string) []string {
	for i := len(reversed) - 1; i >= 0; i-- {
		if hasInstructionFile(reversed[i]) {
			return reversed[:i+1]
		}
	}
	if len(reversed) == 0 {
		return reversed
	}
	return reversed[:1]
}

func hasInstructionFile(dir string) bool {
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
