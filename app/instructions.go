package app

import (
	"errors"
	"os"
	"path/filepath"
)

func LoadProjectInstructions(dir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err == nil {
		return string(content), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", err
}
