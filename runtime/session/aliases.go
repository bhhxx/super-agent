package session

import (
	enginepkg "super-agent/runtime/engine"
	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/model"
)

type State = model.State

const (
	StateIdle            = model.StateIdle
	StateWaitingApproval = model.StateWaitingApproval
)

type Message = model.Message
type ToolCall = model.ToolCall
type PermissionRequest = model.PermissionRequest
type PermissionMode = execution.PermissionMode
type PermissionRules = execution.PermissionRules
type StreamChunk = model.StreamChunk
type Engine = enginepkg.Engine
type Snapshot = enginepkg.Snapshot
type UserMessageSubmitted = machine.UserMessageSubmitted

const (
	PermissionModeAsk = execution.PermissionModeAsk
)

const (
	RoleSystem = model.RoleSystem
	RoleTool   = model.RoleTool
)
