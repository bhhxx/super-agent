//go:build unix

package tools

import (
	"os"
	"syscall"
)

// openWorkspaceFile opens a file that passed workspace containment. O_NOFOLLOW
// closes the final-component race: resolve-then-open spans two syscalls, and
// a path component swapped for a symlink in between would redirect the read
// or write outside the workspace. Deeper component swaps remain possible and
// are accepted as a documented residual risk.
func openWorkspaceFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

func writeFileNoFollow(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	_, err = file.Write(content)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
