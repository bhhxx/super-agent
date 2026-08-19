package session

import (
	enginepkg "super-agent/runtime/engine"
	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
)

type State = machine.State

const (
	StateIdle            = machine.StateIdle
	StateWaitingApproval = machine.StateWaitingApproval
)

type Message = protocol.Message
type ToolCall = protocol.ToolCall
type PermissionRequest = machine.PermissionRequest
type PermissionMode = execution.PermissionMode
type PermissionRules = execution.PermissionRules
type StreamChunk = protocol.StreamChunk
type Engine = enginepkg.Engine
type Snapshot = enginepkg.Snapshot
type UserMessageSubmitted = machine.UserMessageSubmitted

const (
	PermissionModeAsk    = execution.PermissionModeAsk
	PermissionModeBypass = execution.PermissionModeBypass
)

var ValidPermissionMode = execution.ValidPermissionMode

const (
	RoleSystem = protocol.RoleSystem
	RoleTool   = protocol.RoleTool
)
