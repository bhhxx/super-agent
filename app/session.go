package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"super-agent/llm"
	"super-agent/runtime"
	"super-agent/runtime/store"
	"super-agent/tools"
)

func NewSession(cfg Config) (*runtime.Session, error) {
	model, err := llm.NewModel(cfg.Provider, cfg.ModelConfig)
	if err != nil {
		return nil, err
	}
	registry := tools.DefaultRegistry()
	toolRunner := runtime.ToolRunner(registry)
	if cfg.NoTools {
		toolRunner = tools.NoTools{}
	}
	initial, err := initialMessages()
	if err != nil {
		return nil, err
	}
	engine := runtime.NewEngine(model, toolRunner, initial)
	if cfg.AutoApproveTools {
		engine.EnableAutoApproveTools()
	}
	if err := engine.Ready(); err != nil {
		return nil, err
	}
	st, err := store.OpenDefault()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	meta, err := st.Create(store.Metadata{
		Provider:               cfg.Provider,
		Model:                  cfg.ModelConfig.Model,
		CWD:                    cwd,
		Title:                  filepath.Base(cwd),
		InstructionFingerprint: store.Fingerprint(initial),
	}, initial)
	if err != nil {
		return nil, err
	}
	if !cfg.NoTools {
		registry.SetCheckpointCallback(func(call runtime.ToolCall) {
			_ = appendCheckpoint(st, meta.ID, call)
		})
	}
	return runtime.NewPersistentSession(engine, st, meta), nil
}

func initialMessages() ([]runtime.Message, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	instructions, err := LoadProjectInstructions(cwd)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(SystemPrompt)
	if instructions != "" {
		if content != "" {
			content += "\n\n"
		}
		content += strings.TrimSpace(instructions)
	}
	if content == "" {
		return nil, nil
	}
	return []runtime.Message{{Role: runtime.RoleSystem, Content: content}}, nil
}

func appendCheckpoint(st *store.Store, id store.SessionID, call runtime.ToolCall) error {
	cp := store.Checkpoint{
		ID:     time.Now().UTC().Format("20060102T150405.000000000"),
		Reason: call.Name,
	}
	for _, path := range checkpointPaths(call) {
		snapshot, err := fileSnapshot(path)
		if err != nil {
			return err
		}
		cp.Files = append(cp.Files, snapshot)
	}
	return st.Append(id, store.Record{Type: store.EventCheckpoint, ToolCall: &call, Checkpoint: &cp})
}

func checkpointPaths(call runtime.ToolCall) []string {
	var paths []string
	switch call.Name {
	case "write_file", "apply_patch":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(call.Input), &args) == nil && args.Path != "" {
			paths = append(paths, args.Path)
		}
	case "format":
		var args struct {
			Files []string `json:"files"`
		}
		if json.Unmarshal([]byte(call.Input), &args) == nil {
			paths = append(paths, args.Files...)
		}
	}
	return paths
}

func fileSnapshot(path string) (store.FileSnapshot, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.FileSnapshot{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return store.FileSnapshot{}, err
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return store.FileSnapshot{}, err
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return store.FileSnapshot{}, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return store.FileSnapshot{}, errors.New("checkpoint path is outside working directory")
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		return store.FileSnapshot{Path: abs, Exists: false}, nil
	}
	if err != nil {
		return store.FileSnapshot{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return store.FileSnapshot{}, err
	}
	return store.FileSnapshot{
		Path: abs, Exists: true, Content: string(content), Mode: uint32(info.Mode().Perm()),
	}, nil
}
