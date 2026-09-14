package project

import (
	"errors"
	"os"
	"path/filepath"
)

// Project identifies the code project selected by the user. ID is stable for
// a canonical root in this first version; it is intentionally independent of
// workspace access roots.
type Project struct {
	ID   string
	Root string
}

// Resolve selects an explicit directory when supplied. Otherwise it walks
// from cwd to the filesystem root looking for .git and falls back to cwd.
func Resolve(explicit, cwd string) (Project, error) {
	if cwd == "" {
		return Project{}, errors.New("cwd is required")
	}
	start := cwd
	if explicit != "" {
		if filepath.IsAbs(explicit) {
			start = explicit
		} else {
			start = filepath.Join(cwd, explicit)
		}
	}
	root, err := canonicalDirectory(start)
	if err != nil {
		return Project{}, err
	}
	if explicit == "" {
		for current := root; ; current = filepath.Dir(current) {
			if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
				root = current
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return Project{}, err
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
		}
	}
	return Project{ID: root, Root: root}, nil
}

func canonicalDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("project root is not a directory")
	}
	return filepath.Clean(resolved), nil
}
