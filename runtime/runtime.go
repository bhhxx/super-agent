package runtime

import (
	"super-agent/runtime/engine"
	"super-agent/runtime/execution"
	"super-agent/runtime/machine"
	"super-agent/runtime/model"
	"super-agent/runtime/session"
	"super-agent/runtime/store"
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
	RoleSystem    = model.RoleSystem
	RoleUser      = model.RoleUser
	RoleAssistant = model.RoleAssistant
	RoleTool      = model.RoleTool
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
type UserMessageSubmitted = machine.UserMessageSubmitted
type AssistantMessageReceived = machine.AssistantMessageReceived
type ToolCallsReceived = machine.ToolCallsReceived
type ToolBatchReceived = machine.ToolBatchReceived
type ToolCallAvailable = machine.ToolCallAvailable
type ToolCallNeedsApproval = machine.ToolCallNeedsApproval
type ToolCallReadyToRun = machine.ToolCallReadyToRun
type ToolBatchFinished = machine.ToolBatchFinished
type ToolResultReceived = machine.ToolResultReceived
type ApprovalGranted = machine.ApprovalGranted
type ApprovalAlwaysGranted = machine.ApprovalAlwaysGranted
type ApprovalDenied = machine.ApprovalDenied
type ErrorOccurred = machine.ErrorOccurred
type CancelRequested = machine.CancelRequested
type ResetRequested = machine.ResetRequested
type EngineReady = machine.EngineReady

var AllEvents = machine.AllEvents

type Mutation = machine.Mutation
type AppendUserMessage = machine.AppendUserMessage
type AppendAssistantMessage = machine.AppendAssistantMessage
type AppendToolResult = machine.AppendToolResult
type AppendStreamingAssistant = machine.AppendStreamingAssistant
type FlushStreamingAssistant = machine.FlushStreamingAssistant
type SetPendingTool = machine.SetPendingTool
type SetToolCallBatch = machine.SetToolCallBatch
type AdvanceToolCallBatch = machine.AdvanceToolCallBatch
type ClearPendingTool = machine.ClearPendingTool
type ClearPendingEffects = machine.ClearPendingEffects
type ClearToolCallBatch = machine.ClearToolCallBatch
type ResetContext = machine.ResetContext

var AllMutations = machine.AllMutations

type Effect = machine.Effect
type CallModel = machine.CallModel
type RunTool = machine.RunTool
type ProcessNextToolCall = machine.ProcessNextToolCall

var AllEffects = machine.AllEffects

type TransitionResult = machine.TransitionResult

func Transition(state State, event Event) (TransitionResult, error) {
	return machine.Transition(state, event)
}

type EngineState = machine.EngineState
type Reducer = machine.Reducer
type DefaultReducer = machine.DefaultReducer

type ToolDecision = execution.ToolDecision

const (
	DecisionNeedsApproval = execution.DecisionNeedsApproval
	DecisionRunDirectly   = execution.DecisionRunDirectly
	DecisionDenied        = execution.DecisionDenied
)

type PermissionMode = execution.PermissionMode

const (
	PermissionModeAsk         = execution.PermissionModeAsk
	PermissionModeAcceptEdits = execution.PermissionModeAcceptEdits
	PermissionModePlan        = execution.PermissionModePlan
	PermissionModeBypass      = execution.PermissionModeBypass
)

type CommandClass = execution.CommandClass
type PermissionRules = execution.PermissionRules
type PermissionRequest = execution.PermissionRequest
type ToolPolicyInput = execution.ToolPolicyInput
type Policy = execution.Policy
type DefaultPolicy = execution.DefaultPolicy

func NewDefaultPolicy() *DefaultPolicy {
	return execution.NewDefaultPolicy()
}

func NewPolicy(mode PermissionMode, rules PermissionRules) *DefaultPolicy {
	return execution.NewPolicy(mode, rules)
}

func ValidPermissionMode(mode PermissionMode) bool {
	return execution.ValidPermissionMode(mode)
}

type ApprovalKey = execution.ApprovalKey
type ApprovalStore = execution.ApprovalStore
type MemoryApprovalStore = execution.MemoryApprovalStore

func NewMemoryApprovalStore() *MemoryApprovalStore {
	return execution.NewMemoryApprovalStore()
}

func NewApprovalKey(call ToolCall) ApprovalKey {
	return execution.NewApprovalKey(call)
}

type EventClassifier = execution.EventClassifier
type EventClassifyInput = execution.EventClassifyInput
type DefaultEventClassifier = execution.DefaultEventClassifier

func NewDefaultEventClassifier(policy Policy, approvals ApprovalStore) *DefaultEventClassifier {
	return execution.NewDefaultEventClassifier(policy, approvals)
}

type ExecutionInput = execution.ExecutionInput
type EffectExecutor = execution.EffectExecutor
type DefaultEffectExecutor = execution.DefaultEffectExecutor

func NewDefaultEffectExecutor(model Model, tools ToolRunner) *DefaultEffectExecutor {
	return execution.NewDefaultEffectExecutor(model, tools)
}

type ExecutionResult = execution.ExecutionResult
type ModelReplied = execution.ModelReplied
type ToolFinished = execution.ToolFinished
type ToolQueueChecked = execution.ToolQueueChecked

type RunID = execution.RunID
type EffectID = execution.EffectID
type QueuedEffect = execution.QueuedEffect
type EffectOutcome = execution.EffectOutcome
type EffectRunner = execution.EffectRunner
type DefaultEffectRunner = execution.DefaultEffectRunner

func NewDefaultEffectRunner(executor EffectExecutor) *DefaultEffectRunner {
	return execution.NewDefaultEffectRunner(executor)
}

type EffectScheduler = execution.EffectScheduler

func NewEffectScheduler() *EffectScheduler {
	return execution.NewEffectScheduler()
}

type ResultResolver = execution.ResultResolver
type ResultResolveInput = execution.ResultResolveInput
type DefaultResultResolver = execution.DefaultResultResolver

type RunController = execution.RunController
type DefaultRunController = execution.DefaultRunController

func NewDefaultRunController() *DefaultRunController {
	return execution.NewDefaultRunController()
}

type Engine = engine.Engine

func NewEngine(model Model, tools ToolRunner, initial []Message) *Engine {
	return engine.NewEngine(model, tools, initial)
}

func NewEngineWithExecutor(executor EffectExecutor, initial []Message) *Engine {
	return engine.NewEngineWithExecutor(executor, initial)
}

func NewEngineWithExecutorAndPolicy(executor EffectExecutor, policy Policy, initial []Message) *Engine {
	return engine.NewEngineWithExecutorAndPolicy(executor, policy, initial)
}

func NewEngineWithComponents(runner EffectRunner, resolver ResultResolver, classifier EventClassifier, reducer Reducer, runs RunController, approvals ApprovalStore, initial []Message) *Engine {
	return engine.NewEngineWithComponents(runner, resolver, classifier, reducer, runs, approvals, initial)
}

type ApprovalDecision = session.ApprovalDecision

const (
	ApproveOnce   = session.ApproveOnce
	ApproveAlways = session.ApproveAlways
	DenyApproval  = session.DenyApproval
)

type SessionEvent = session.SessionEvent
type StateChanged = session.StateChanged
type ToolApprovalRequested = session.ToolApprovalRequested
type ToolApprovalCleared = session.ToolApprovalCleared
type StreamChunkReceived = session.StreamChunkReceived
type MessageAppended = session.MessageAppended
type SessionError = session.SessionError
type Snapshot = session.Snapshot
type Session = session.Session

func NewSession(engine *Engine) *Session {
	return session.NewSession(engine)
}

func NewPersistentSession(engine *Engine, st *store.Store, meta store.Metadata) *Session {
	return session.NewPersistentSession(engine, st, meta)
}

type SessionID = store.SessionID
type TurnID = store.TurnID
type SessionMetadata = store.Metadata
type SessionSummary = store.Summary
type SessionRecord = store.Record
type SessionCheckpoint = store.Checkpoint
