package execution

import (
	"super-agent/runtime/machine"
	"super-agent/runtime/model"
)

type Message = model.Message
type ToolCall = model.ToolCall
type ToolCallBatch = model.ToolCallBatch
type ToolSpec = model.ToolSpec
type ModelResponse = model.ModelResponse
type StreamChunk = model.StreamChunk
type Model = model.Model
type ToolRunner = model.ToolRunner

type Event = machine.Event
type AssistantMessageReceived = machine.AssistantMessageReceived
type ToolCallsReceived = machine.ToolCallsReceived
type ToolBatchReceived = machine.ToolBatchReceived
type ToolCallAvailable = machine.ToolCallAvailable
type ToolBatchFinished = machine.ToolBatchFinished
type ToolCallNeedsApproval = machine.ToolCallNeedsApproval
type ToolCallReadyToRun = machine.ToolCallReadyToRun
type ToolResultReceived = machine.ToolResultReceived

type Effect = machine.Effect
type CallModel = machine.CallModel
type RunTool = machine.RunTool
type ProcessNextToolCall = machine.ProcessNextToolCall
type AppendStreamingAssistant = machine.AppendStreamingAssistant
