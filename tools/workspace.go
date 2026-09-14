package tools

import (
	"errors"
	"path/filepath"
)

// WorkspaceContext is the narrow policy port required by built-in tools.
// workspace.Context is the concrete implementation assembled by app.
type WorkspaceContext interface {
	GetPrimaryRoot() string
	GetCWD() string
	ResolvePath(string) (string, error)
	CanRead(string) bool
	CanWrite(string) bool
}

func resolveReadable(workspace WorkspaceContext, path string) (string, string, error) {
	if workspace == nil {
		return "", "", errors.New("workspace is not configured")
	}
	resolved, err := workspace.ResolvePath(path)
	if err != nil {
		return "", "", err
	}
	if !workspace.CanRead(resolved) {
		return "", "", errors.New("path is outside readable workspace roots")
	}
	return resolved, displayPath(workspace, resolved), nil
}

func resolveWritable(workspace WorkspaceContext, path string) (string, string, error) {
	if workspace == nil {
		return "", "", errors.New("workspace is not configured")
	}
	resolved, err := workspace.ResolvePath(path)
	if err != nil {
		return "", "", err
	}
	if !workspace.CanWrite(resolved) {
		return "", "", errors.New("path is outside writable workspace roots")
	}
	return resolved, displayPath(workspace, resolved), nil
}

func displayPath(workspace WorkspaceContext, path string) string {
	rel, err := filepath.Rel(workspace.GetCWD(), path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
