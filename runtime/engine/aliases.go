package engine

import (
	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/model"
)

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
)

type Message = model.Message
type ToolCall = model.ToolCall
type ToolCallBatch = model.ToolCallBatch
type PermissionRequest = model.PermissionRequest
type ToolSpec = model.ToolSpec
type Model = model.Model
type ToolRunner = model.ToolRunner
type StreamChunk = model.StreamChunk

type Event = machine.Event
type UserMessageSubmitted = machine.UserMessageSubmitted
type ApprovalGranted = machine.ApprovalGranted
type ApprovalAlwaysGranted = machine.ApprovalAlwaysGranted
type ApprovalDenied = machine.ApprovalDenied
type ErrorOccurred = machine.ErrorOccurred
type CancelRequested = machine.CancelRequested
type ResetRequested = machine.ResetRequested
type EngineReady = machine.EngineReady
type TransitionResult = machine.TransitionResult
type Mutation = machine.Mutation
type AppendStreamingAssistant = machine.AppendStreamingAssistant
type EngineState = machine.EngineState
type Reducer = machine.Reducer
type DefaultReducer = machine.DefaultReducer

var Transition = machine.Transition

type ApprovalKey = execution.ApprovalKey
type ApprovalStore = execution.ApprovalStore
type Policy = execution.Policy
type PermissionMode = execution.PermissionMode
type PermissionRules = execution.PermissionRules
type EventClassifier = execution.EventClassifier
type EventClassifyInput = execution.EventClassifyInput
type EffectExecutor = execution.EffectExecutor
type ExecutionInput = execution.ExecutionInput
type ExecutionResult = execution.ExecutionResult
type ModelReplied = execution.ModelReplied
type EffectRunner = execution.EffectRunner
type QueuedEffect = execution.QueuedEffect
type EffectScheduler = execution.EffectScheduler
type ResultResolver = execution.ResultResolver
type ResultResolveInput = execution.ResultResolveInput
type RunID = execution.RunID
type RunController = execution.RunController
type DefaultResultResolver = execution.DefaultResultResolver
type DefaultEffectRunner = execution.DefaultEffectRunner
type DefaultEventClassifier = execution.DefaultEventClassifier
type CallModel = machine.CallModel

var NewApprovalKey = execution.NewApprovalKey
var NewMemoryApprovalStore = execution.NewMemoryApprovalStore
var NewDefaultEffectExecutor = execution.NewDefaultEffectExecutor
var NewDefaultEffectRunner = execution.NewDefaultEffectRunner
var NewDefaultEventClassifier = execution.NewDefaultEventClassifier
var NewDefaultPolicy = execution.NewDefaultPolicy
var NewPolicy = execution.NewPolicy
var ValidPermissionMode = execution.ValidPermissionMode
var NewDefaultRunController = execution.NewDefaultRunController
var NewEffectScheduler = execution.NewEffectScheduler
