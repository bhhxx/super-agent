package session

import "encoding/json"

func (s *Session) Checkpoint(call ToolCall) error {
	if s.repository == nil || s.workspace == nil {
		return nil
	}
	paths := checkpointPaths(call)
	if len(paths) == 0 {
		return nil
	}
	files, err := s.workspace.Capture(paths)
	if err != nil {
		return err
	}
	return s.repository.SaveCheckpoint(s.metaID(), call, files)
}

func checkpointPaths(call ToolCall) []string {
	switch call.Name {
	case "write_file", "apply_patch":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(call.Input), &args) == nil && args.Path != "" {
			return []string{args.Path}
		}
	case "format":
		var args struct {
			Files []string `json:"files"`
		}
		if json.Unmarshal([]byte(call.Input), &args) == nil {
			return args.Files
		}
	}
	return nil
}
