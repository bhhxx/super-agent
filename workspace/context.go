package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Access string

const (
	AccessRead      Access = "read"
	AccessReadWrite Access = "read_write"
)

type Root struct {
	Path   string
	Access Access
}

// Context is the single filesystem access policy for one agent session.
// Access roots deliberately do not imply trust for configuration discovery.
type Context struct {
	primaryRoot string
	cwd         string
	roots       []Root
}

func NewContext(primaryRoot, cwd string, roots []Root) (*Context, error) {
	if primaryRoot == "" || cwd == "" {
		return nil, errors.New("workspace primary root and cwd are required")
	}
	primary, err := canonicalDirectory(primaryRoot)
	if err != nil {
		return nil, err
	}
	working, err := canonicalDirectory(cwd)
	if err != nil {
		return nil, err
	}
	normalized := make([]Root, 0, len(roots))
	for _, root := range roots {
		if root.Access != AccessRead && root.Access != AccessReadWrite {
			return nil, errors.New("invalid workspace root access")
		}
		path, err := canonicalDirectory(root.Path)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, Root{Path: path, Access: root.Access})
	}
	if len(normalized) == 0 {
		return nil, errors.New("workspace requires at least one root")
	}
	ctx := &Context{primaryRoot: primary, cwd: working, roots: normalized}
	if !ctx.canAccess(primary, false) {
		return nil, errors.New("workspace primary root is not readable")
	}
	if !ctx.canAccess(working, false) {
		return nil, errors.New("workspace cwd is not readable")
	}
	return ctx, nil
}

func NewDefaultContext(root string) (*Context, error) {
	return NewContext(root, root, []Root{{Path: root, Access: AccessReadWrite}})
}

func (c *Context) GetPrimaryRoot() string { return c.primaryRoot }
func (c *Context) GetCWD() string         { return c.cwd }

func (c *Context) GetRoots() []Root {
	return append([]Root(nil), c.roots...)
}

// ResolvePath resolves relative paths from the workspace cwd and canonicalizes
// symlinks. For a target that does not exist, it resolves the nearest existing
// ancestor before re-appending the remaining path components.
func (c *Context) ResolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.cwd, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return canonicalNearest(abs)
}

func (c *Context) CanRead(path string) bool {
	resolved, err := c.ResolvePath(path)
	return err == nil && c.canAccess(resolved, false)
}

func (c *Context) CanWrite(path string) bool {
	resolved, err := c.ResolvePath(path)
	return err == nil && c.canAccess(resolved, true)
}

func (c *Context) canAccess(path string, write bool) bool {
	for _, root := range c.roots {
		if write && root.Access != AccessReadWrite {
			continue
		}
		rel, err := filepath.Rel(root.Path, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
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
		return "", errors.New("workspace root is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func canonicalNearest(path string) (string, error) {
	suffix := ""
	current := filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Clean(filepath.Join(resolved, suffix)), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(filepath.Join(current, suffix)), nil
		}
		suffix = filepath.Join(filepath.Base(current), suffix)
		current = parent
	}
}
