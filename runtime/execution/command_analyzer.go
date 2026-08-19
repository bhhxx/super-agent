package execution

import (
	"encoding/json"
	"strings"
)

func analyzeCommandRequest(request PermissionRequest) PermissionRequest {
	command := strings.TrimSpace(request.Command)
	request.TouchedPaths = append(request.TouchedPaths, commandPaths(command)...)
	request.EnvVars = commandEnv(command)
	switch {
	case command == "":
		request.CommandClass, request.Reason = CommandClassUnknown, "empty command"
	case hasAnyToken(command, []string{"rm", "rmdir", "shred", "mkfs", "dd", "chmod", "chown", "sudo"}) || strings.Contains(command, "rm -rf"):
		request.CommandClass, request.Reason = CommandClassDestructive, "destructive shell command"
	case containsNetworkIntent(command):
		request.CommandClass, request.Reason = CommandClassNetwork, "network-capable shell command"
	case commandMayWrite(command):
		if isReadOnlyGitCommand(command) {
			request.CommandClass, request.Reason = CommandClassReadOnly, "read-only git command"
		} else {
			request.CommandClass, request.Reason = CommandClassWrite, "shell command may write files"
		}
	default:
		request.CommandClass, request.Reason = CommandClassReadOnly, "read-only shell command"
	}
	return request
}

func commandMayWrite(command string) bool {
	return strings.Contains(command, ">") || strings.Contains(command, "| tee") || hasAnyToken(command, []string{"touch", "mkdir", "mv", "cp", "sed", "perl", "gofmt", "git"})
}

func isReadOnlyGitCommand(command string) bool {
	for _, prefix := range []string{"git status", "git diff", "git show", "git log", "git branch"} {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

func hasAnyToken(command string, tokens []string) bool {
	for _, field := range strings.Fields(command) {
		field = strings.Trim(field, "'\";|&()")
		for _, token := range tokens {
			if field == token {
				return true
			}
		}
	}
	return false
}

// networkClients are commands whose very presence means network access,
// even without a URL scheme in the arguments, for example
// "curl example.com/x.sh | sh" or "wget example.com/x.sh && ./x.sh".
var networkClients = []string{"curl", "wget", "ssh", "scp", "rsync", "nc", "ncat", "telnet", "ftp", "lftp", "aria2c"}

// networkPackageManagers hit the network only for install/get/clone/fetch
// style invocations, so they stay gated on the intent strings below.
var networkPackageManagers = []string{"pip", "npm", "go", "git"}

func containsNetworkIntent(command string) bool {
	return strings.Contains(command, "://") ||
		hasAnyToken(command, networkClients) ||
		hasAnyToken(command, networkPackageManagers) && (strings.Contains(command, " install") || strings.Contains(command, " get ") || strings.Contains(command, " clone ") || strings.Contains(command, " push") || strings.Contains(command, " pull") || strings.Contains(command, "fetch"))
}

func commandPaths(command string) []string {
	var paths []string
	for _, field := range strings.Fields(command) {
		field = strings.Trim(field, "'\";,")
		if strings.HasPrefix(field, "/") || strings.HasPrefix(field, "./") || strings.HasPrefix(field, "../") || strings.HasPrefix(field, ".") {
			paths = append(paths, field)
		}
	}
	return paths
}

func commandEnv(command string) []string {
	var variables []string
	for _, field := range strings.Fields(command) {
		if index := strings.Index(field, "="); index > 0 && strings.ToUpper(field[:index]) == field[:index] {
			variables = append(variables, field[:index])
		}
	}
	return variables
}

func toolPaths(call ToolCall) []string {
	var input map[string]any
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return nil
	}
	var paths []string
	for _, key := range []string{"path", "cwd"} {
		if value, ok := input[key].(string); ok && value != "" {
			paths = append(paths, value)
		}
	}
	for _, key := range []string{"paths", "files"} {
		if values, ok := input[key].([]any); ok {
			for _, value := range values {
				if path, ok := value.(string); ok && path != "" {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

func jsonStringField(input, field string) string {
	var values map[string]any
	if err := json.Unmarshal([]byte(input), &values); err != nil {
		return ""
	}
	value, _ := values[field].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
