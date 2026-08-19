package machine

import (
	"super-agent/runtime/permission"
	"super-agent/runtime/protocol"
)

type Role = protocol.Role

const (
	RoleSystem    = protocol.RoleSystem
	RoleUser      = protocol.RoleUser
	RoleAssistant = protocol.RoleAssistant
	RoleTool      = protocol.RoleTool
)

type Message = protocol.Message
type ToolCall = protocol.ToolCall
type ModelResponse = protocol.ModelResponse
type StreamChunk = protocol.StreamChunk
type CommandClass = permission.CommandClass
type PermissionRequest = permission.Request

const (
	CommandClassReadOnly    = permission.CommandClassReadOnly
	CommandClassWrite       = permission.CommandClassWrite
	CommandClassNetwork     = permission.CommandClassNetwork
	CommandClassDestructive = permission.CommandClassDestructive
	CommandClassUnknown     = permission.CommandClassUnknown
)
