//go:build !unix

package tools

import "os"

// openWorkspaceFile and writeFileNoFollow fall back to plain opens on
// platforms without O_NOFOLLOW; containment relies on the symlink
// resolution performed before these calls.
func openWorkspaceFile(path string) (*os.File, error) {
	return os.Open(path)
}

func writeFileNoFollow(path string, content []byte, mode os.FileMode) error {
	return os.WriteFile(path, content, mode)
}
