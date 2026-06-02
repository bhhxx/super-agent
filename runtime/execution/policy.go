package execution

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

type ToolDecision int

const (
	DecisionNeedsApproval ToolDecision = iota
	DecisionRunDirectly
	DecisionDenied
)

type PermissionMode string

const (
	PermissionModeAsk         PermissionMode = "ask"
	PermissionModeAcceptEdits PermissionMode = "accept-edits"
	PermissionModePlan        PermissionMode = "plan"
	PermissionModeBypass      PermissionMode = "bypass"
)

type PermissionRules struct {
	AllowTools     []string `json:"allow_tools,omitempty"`
	DenyTools      []string `json:"deny_tools,omitempty"`
	AllowPrefixes  []string `json:"allow_command_prefixes,omitempty"`
	DenyPrefixes   []string `json:"deny_command_prefixes,omitempty"`
	AllowPaths     []string `json:"allow_paths,omitempty"`
	DenyPaths      []string `json:"deny_paths,omitempty"`
	AllowEnv       []string `json:"allow_env,omitempty"`
	DenyEnv        []string `json:"deny_env,omitempty"`
	Network        string   `json:"network,omitempty"`
	OpenSandboxID  string   `json:"opensandbox_id,omitempty"`
	OpenSandboxCLI string   `json:"opensandbox_cli,omitempty"`
	OpenSandboxCWD string   `json:"opensandbox_cwd,omitempty"`
}

type ToolPolicyInput struct {
	ToolSpecs []ToolSpec
	CWD       string
}

type Policy interface {
	ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision
	PermissionRequest(call ToolCall, input ToolPolicyInput) PermissionRequest
}

type DefaultPolicy struct {
	mode  PermissionMode
	rules PermissionRules
}

func NewDefaultPolicy() *DefaultPolicy {
	return NewPolicy(PermissionModeAsk, PermissionRules{})
}

func NewPolicy(mode PermissionMode, rules PermissionRules) *DefaultPolicy {
	if mode == "" {
		mode = PermissionModeAsk
	}
	rules.Network = strings.ToLower(strings.TrimSpace(rules.Network))
	if rules.OpenSandboxCLI == "" {
		rules.OpenSandboxCLI = "osb"
	}
	return &DefaultPolicy{mode: mode, rules: rules}
}

func (p *DefaultPolicy) Mode() PermissionMode {
	return p.mode
}

func (p *DefaultPolicy) Rules() PermissionRules {
	return p.rules
}

func (p *DefaultPolicy) ClassifyToolCall(call ToolCall, input ToolPolicyInput) ToolDecision {
	req := p.PermissionRequest(call, input)
	if p.matches(call.Name, p.rules.DenyTools) || p.matchesPrefix(req.Command, p.rules.DenyPrefixes) || p.touchesProtectedPath(req) || p.touchesDeniedPath(req) || p.usesDeniedEnv(req) {
		return DecisionDenied
	}
	if p.matches(call.Name, p.rules.AllowTools) || p.matchesPrefix(req.Command, p.rules.AllowPrefixes) || p.pathsAllowed(req) || p.envAllowed(req) {
		return DecisionRunDirectly
	}
	switch p.mode {
	case PermissionModeBypass:
		return DecisionRunDirectly
	case PermissionModePlan:
		if req.CommandClass == CommandClassReadOnly && !p.needsApproval(call, input.ToolSpecs) {
			return DecisionRunDirectly
		}
		return DecisionDenied
	case PermissionModeAcceptEdits:
		if req.CommandClass == CommandClassDestructive || p.networkDenied(req) {
			return DecisionNeedsApproval
		}
		if req.CommandClass == CommandClassReadOnly {
			return DecisionRunDirectly
		}
		if p.isWriteTool(call.Name) {
			return DecisionRunDirectly
		}
	}
	if p.needsApproval(call, input.ToolSpecs) || p.networkDenied(req) {
		return DecisionNeedsApproval
	}
	return DecisionRunDirectly
}

func (p *DefaultPolicy) PermissionRequest(call ToolCall, input ToolPolicyInput) PermissionRequest {
	req := PermissionRequest{
		ToolName:     call.Name,
		CommandClass: CommandClassUnknown,
		CWD:          input.CWD,
		Reason:       "unknown tool calls require approval",
	}
	if req.CWD == "" {
		req.CWD = "."
	}
	switch call.Name {
	case "bash":
		req.Command = jsonStringField(call.Input, "command")
		req = analyzeCommandRequest(req)
	case "run_command":
		req.Command = jsonStringField(call.Input, "command")
		req.CWD = firstNonEmpty(jsonStringField(call.Input, "cwd"), req.CWD)
		req = analyzeCommandRequest(req)
	case "go_test":
		req.Command = "go test"
		req.CWD = firstNonEmpty(jsonStringField(call.Input, "cwd"), req.CWD)
		req.CommandClass = CommandClassReadOnly
		req.Reason = "go test is read-only"
	case "git_status", "git_diff", "read_file", "list_files", "search":
		req.CommandClass = CommandClassReadOnly
		req.TouchedPaths = toolPaths(call)
		req.Reason = "read-only tool"
	case "write_file", "apply_patch", "format":
		req.CommandClass = CommandClassWrite
		req.TouchedPaths = toolPaths(call)
		req.Reason = "tool writes workspace files"
	default:
		if !p.needsApproval(call, input.ToolSpecs) {
			req.CommandClass = CommandClassReadOnly
			req.Reason = "tool spec is marked safe"
		}
	}
	return req
}

func (p *DefaultPolicy) needsApproval(call ToolCall, specs []ToolSpec) bool {
	return isRiskyTool(call.Name, specs)
}

func (p *DefaultPolicy) isWriteTool(name string) bool {
	switch name {
	case "write_file", "apply_patch", "format":
		return true
	default:
		return false
	}
}

func (p *DefaultPolicy) networkDenied(req PermissionRequest) bool {
	return p.rules.Network != "allow" && req.CommandClass == CommandClassNetwork
}

func (p *DefaultPolicy) touchesProtectedPath(req PermissionRequest) bool {
	for _, path := range req.TouchedPaths {
		clean := filepath.ToSlash(filepath.Clean(path))
		if clean == ".git" || strings.HasPrefix(clean, ".git/") || clean == ".env" || strings.Contains(clean, "/.env") || strings.Contains(clean, ".ssh/") || strings.Contains(clean, ".aws/") || strings.Contains(clean, ".config/gcloud/") {
			return true
		}
		if filepath.IsAbs(path) && req.CWD != "" {
			if rel, err := filepath.Rel(req.CWD, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

func (p *DefaultPolicy) touchesDeniedPath(req PermissionRequest) bool {
	for _, path := range req.TouchedPaths {
		if p.matchesPath(path, p.rules.DenyPaths) {
			return true
		}
	}
	return false
}

func (p *DefaultPolicy) usesDeniedEnv(req PermissionRequest) bool {
	for _, key := range req.EnvVars {
		if p.matches(key, p.rules.DenyEnv) {
			return true
		}
	}
	return false
}

func (p *DefaultPolicy) pathsAllowed(req PermissionRequest) bool {
	if len(req.TouchedPaths) == 0 || len(p.rules.AllowPaths) == 0 {
		return false
	}
	for _, path := range req.TouchedPaths {
		if !p.matchesPath(path, p.rules.AllowPaths) {
			return false
		}
	}
	return true
}

func (p *DefaultPolicy) envAllowed(req PermissionRequest) bool {
	if len(req.EnvVars) == 0 || len(p.rules.AllowEnv) == 0 {
		return false
	}
	for _, key := range req.EnvVars {
		if !p.matches(key, p.rules.AllowEnv) {
			return false
		}
	}
	return true
}

func (p *DefaultPolicy) matches(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == value {
			return true
		}
	}
	return false
}

func (p *DefaultPolicy) matchesPrefix(command string, prefixes []string) bool {
	command = strings.TrimSpace(command)
	for _, prefix := range prefixes {
		if strings.HasPrefix(command, strings.TrimSpace(prefix)) {
			return true
		}
	}
	return false
}

func (p *DefaultPolicy) matchesPath(path string, patterns []string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(filepath.Clean(pattern))
		if path == pattern || strings.HasPrefix(path, strings.TrimSuffix(pattern, "/")+"/") {
			return true
		}
	}
	return false
}

func isRiskyTool(name string, specs []ToolSpec) bool {
	for _, s := range specs {
		if s.Name == name {
			return s.Risky
		}
	}
	return true
}

func analyzeCommandRequest(req PermissionRequest) PermissionRequest {
	command := strings.TrimSpace(req.Command)
	req.TouchedPaths = append(req.TouchedPaths, commandPaths(command)...)
	req.EnvVars = commandEnv(command)
	switch {
	case command == "":
		req.CommandClass = CommandClassUnknown
		req.Reason = "empty command"
	case hasAnyToken(command, []string{"rm", "rmdir", "shred", "mkfs", "dd", "chmod", "chown", "sudo"}) || strings.Contains(command, "rm -rf"):
		req.CommandClass = CommandClassDestructive
		req.Reason = "destructive shell command"
	case hasAnyToken(command, []string{"curl", "wget", "ssh", "scp", "rsync", "nc", "ncat", "telnet", "ftp", "pip", "npm", "go"}) && containsNetworkIntent(command):
		req.CommandClass = CommandClassNetwork
		req.Reason = "network-capable shell command"
	case strings.Contains(command, ">") || strings.Contains(command, ">>") || strings.Contains(command, "| tee") || hasAnyToken(command, []string{"touch", "mkdir", "mv", "cp", "sed", "perl", "gofmt", "git"}):
		if strings.HasPrefix(command, "git status") || strings.HasPrefix(command, "git diff") || strings.HasPrefix(command, "git show") || strings.HasPrefix(command, "git log") || strings.HasPrefix(command, "git branch") {
			req.CommandClass = CommandClassReadOnly
			req.Reason = "read-only git command"
			break
		}
		req.CommandClass = CommandClassWrite
		req.Reason = "shell command may write files"
	default:
		req.CommandClass = CommandClassReadOnly
		req.Reason = "read-only shell command"
	}
	return req
}

func hasAnyToken(command string, tokens []string) bool {
	fields := strings.Fields(command)
	for _, field := range fields {
		field = strings.Trim(field, "'\";|&()")
		for _, token := range tokens {
			if field == token {
				return true
			}
		}
	}
	return false
}

func containsNetworkIntent(command string) bool {
	return strings.Contains(command, "://") || strings.Contains(command, " install") || strings.Contains(command, " get ") || strings.Contains(command, " clone ") || strings.Contains(command, "fetch")
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
	var env []string
	for _, field := range strings.Fields(command) {
		if idx := strings.Index(field, "="); idx > 0 && strings.ToUpper(field[:idx]) == field[:idx] {
			env = append(env, field[:idx])
		}
	}
	return env
}

func toolPaths(call ToolCall) []string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(call.Input), &raw); err != nil {
		return nil
	}
	var paths []string
	for _, key := range []string{"path", "cwd"} {
		if value, ok := raw[key].(string); ok && value != "" {
			paths = append(paths, value)
		}
	}
	if values, ok := raw["paths"].([]any); ok {
		for _, value := range values {
			if path, ok := value.(string); ok && path != "" {
				paths = append(paths, path)
			}
		}
	}
	if values, ok := raw["files"].([]any); ok {
		for _, value := range values {
			if path, ok := value.(string); ok && path != "" {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func jsonStringField(input, field string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return ""
	}
	value, _ := raw[field].(string)
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
