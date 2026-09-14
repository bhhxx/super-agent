package workspace

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"super-agent/runtime/session"
)

type Workspace struct {
	mu      sync.RWMutex
	context *Context
}

func New(context *Context) *Workspace { return &Workspace{context: context} }

func (w *Workspace) GetPrimaryRoot() string {
	context, _ := w.current()
	if context == nil {
		return ""
	}
	return context.GetPrimaryRoot()
}

func (w *Workspace) GetCWD() string {
	context, _ := w.current()
	if context == nil {
		return ""
	}
	return context.GetCWD()
}

func (w *Workspace) ResolvePath(path string) (string, error) {
	context, err := w.current()
	if err != nil {
		return "", err
	}
	return context.ResolvePath(path)
}

func (w *Workspace) CanRead(path string) bool {
	context, err := w.current()
	return err == nil && context.CanRead(path)
}

func (w *Workspace) CanWrite(path string) bool {
	context, err := w.current()
	return err == nil && context.CanWrite(path)
}

func (w *Workspace) Spec() session.WorkspaceSpec {
	context, err := w.current()
	if err != nil {
		return session.WorkspaceSpec{}
	}
	return specFromContext(context)
}

func (w *Workspace) Validate(spec session.WorkspaceSpec) error {
	_, err := contextFromSpec(spec)
	return err
}

// Canonicalize resolves every path in spec against the current filesystem and
// returns the canonical description. Session persistence uses it exactly once
// to upgrade legacy metadata, whose saved cwd never promised a canonical path.
// New specs are validated strictly and never pass through here.
func (w *Workspace) Canonicalize(spec session.WorkspaceSpec) (session.WorkspaceSpec, error) {
	context, err := buildContext(spec)
	if err != nil {
		return session.WorkspaceSpec{}, err
	}
	return specFromContext(context), nil
}

func (w *Workspace) Activate(spec session.WorkspaceSpec) error {
	context, err := contextFromSpec(spec)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.context = context
	w.mu.Unlock()
	return nil
}

func (w *Workspace) current() (*Context, error) {
	if w == nil {
		return nil, errors.New("workspace context is not configured")
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.context == nil {
		return nil, errors.New("workspace context is not configured")
	}
	return w.context, nil
}

func contextFromSpec(spec session.WorkspaceSpec) (*Context, error) {
	context, err := buildContext(spec)
	if err != nil {
		return nil, err
	}
	if !samePath(spec.PrimaryRoot, context.GetPrimaryRoot()) || !samePath(spec.CWD, context.GetCWD()) {
		return nil, errors.New("saved workspace primary root or cwd resolves to a different path")
	}
	resolvedRoots := context.GetRoots()
	for i := range spec.Roots {
		if !samePath(spec.Roots[i].Path, resolvedRoots[i].Path) {
			return nil, errors.New("saved workspace root resolves to a different path")
		}
	}
	return context, nil
}

func buildContext(spec session.WorkspaceSpec) (*Context, error) {
	if !filepath.IsAbs(spec.PrimaryRoot) || !filepath.IsAbs(spec.CWD) || len(spec.Roots) == 0 {
		return nil, errors.New("workspace spec requires absolute primary root, cwd, and roots")
	}
	roots := make([]Root, len(spec.Roots))
	for i, root := range spec.Roots {
		if !filepath.IsAbs(root.Path) {
			return nil, errors.New("workspace spec root is not absolute")
		}
		access, err := fromSessionAccess(root.Access)
		if err != nil {
			return nil, err
		}
		roots[i] = Root{Path: root.Path, Access: access}
	}
	return NewContext(spec.PrimaryRoot, spec.CWD, roots)
}

func specFromContext(context *Context) session.WorkspaceSpec {
	roots := context.GetRoots()
	specRoots := make([]session.WorkspaceRootSpec, len(roots))
	for i, root := range roots {
		specRoots[i] = session.WorkspaceRootSpec{Path: root.Path, Access: toSessionAccess(root.Access)}
	}
	return session.WorkspaceSpec{PrimaryRoot: context.GetPrimaryRoot(), CWD: context.GetCWD(), Roots: specRoots}
}

func samePath(saved, resolved string) bool {
	rel, err := filepath.Rel(filepath.Clean(saved), filepath.Clean(resolved))
	return err == nil && rel == "."
}

func toSessionAccess(access Access) session.WorkspaceAccessMode {
	if access == AccessReadWrite {
		return session.WorkspaceAccessReadWrite
	}
	return session.WorkspaceAccessRead
}

func fromSessionAccess(access session.WorkspaceAccessMode) (Access, error) {
	switch access {
	case session.WorkspaceAccessRead:
		return AccessRead, nil
	case session.WorkspaceAccessReadWrite:
		return AccessReadWrite, nil
	default:
		return "", errors.New("invalid saved workspace access mode")
	}
}

func (w *Workspace) ReadAttachment(path string) (session.Attachment, error) {
	absolute, err := w.readable(path)
	if err != nil {
		return session.Attachment{}, err
	}
	if _, err := capture(absolute); err != nil {
		return session.Attachment{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return session.Attachment{}, err
	}
	if info.Size() > 10<<20 {
		return session.Attachment{}, errors.New("attachment exceeds 10 MiB")
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return session.Attachment{}, err
	}
	mime := http.DetectContentType(content)
	return session.Attachment{Name: filepath.Base(absolute), MIME: mime, Data: base64.StdEncoding.EncodeToString(content)}, nil
}

func (w *Workspace) WriteExport(relative string, content []byte) (string, error) {
	path, err := w.writable(relative)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func (w *Workspace) Capture(paths []string) ([]session.FileSnapshot, error) {
	files := make([]session.FileSnapshot, 0, len(paths))
	for _, path := range paths {
		absolute, err := w.writable(path)
		if err != nil {
			return nil, err
		}
		file, err := capture(absolute)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func capture(path string) (session.FileSnapshot, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return session.FileSnapshot{Path: path, Exists: false}, nil
	}
	if err != nil {
		return session.FileSnapshot{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return session.FileSnapshot{}, err
	}
	return session.FileSnapshot{Path: path, Exists: true, Content: string(content), Mode: uint32(info.Mode().Perm())}, nil
}

func (w *Workspace) Restore(files []session.FileSnapshot) error {
	for _, file := range files {
		path, err := w.writable(file.Path)
		if err != nil {
			return err
		}
		if !file.Exists {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		mode := os.FileMode(file.Mode)
		if mode == 0 {
			mode = 0644
		}
		if err := os.WriteFile(path, []byte(file.Content), mode); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workspace) readable(path string) (string, error) {
	resolved, err := w.ResolvePath(path)
	if err != nil {
		return "", err
	}
	if !w.CanRead(resolved) {
		return "", errors.New("path is outside readable workspace roots")
	}
	return resolved, nil
}

func (w *Workspace) writable(path string) (string, error) {
	resolved, err := w.ResolvePath(path)
	if err != nil {
		return "", err
	}
	if !w.CanWrite(resolved) {
		return "", errors.New("path is outside writable workspace roots")
	}
	return resolved, nil
}
