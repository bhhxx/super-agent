package machine

import "super-agent/runtime/model"

type State = model.State

const (
	StateInitializing    = model.StateInitializing
	StateIdle            = model.StateIdle
	StateWaitingLLM      = model.StateWaitingLLM
	StateWaitingApproval = model.StateWaitingApproval
	StateRunningTool     = model.StateRunningTool
	StateAdvancingQueue  = model.StateAdvancingQueue
)

type Role = model.Role

const (
	RoleUser      = model.RoleUser
	RoleAssistant = model.RoleAssistant
	RoleTool      = model.RoleTool
)

type Message = model.Message
type ToolCall = model.ToolCall
type ToolCallBatch = model.ToolCallBatch
type ModelResponse = model.ModelResponse
type StreamChunk = model.StreamChunk
