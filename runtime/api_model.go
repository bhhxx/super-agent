package runtime

import (
	"super-agent/runtime/machine"
	"super-agent/runtime/protocol"
)

type State = machine.State

const (
	StateInitializing    = machine.StateInitializing
	StateIdle            = machine.StateIdle
	StateWaitingLLM      = machine.StateWaitingLLM
	StateWaitingApproval = machine.StateWaitingApproval
	StateRunningTool     = machine.StateRunningTool
	StateAdvancingQueue  = machine.StateAdvancingQueue
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
type ToolCallBatch = machine.ToolCallBatch
type ToolSpec = protocol.ToolSpec
type ModelResponse = protocol.ModelResponse
type StreamChunk = protocol.StreamChunk
type Model = protocol.Model
type ToolRunner = protocol.ToolRunner
