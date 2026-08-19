package runtime

import "super-agent/runtime/execution"

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

func NewDefaultPolicy() *DefaultPolicy { return execution.NewDefaultPolicy() }
func NewPolicy(mode PermissionMode, rules PermissionRules) *DefaultPolicy {
	return execution.NewPolicy(mode, rules)
}
func ValidPermissionMode(mode PermissionMode) bool { return execution.ValidPermissionMode(mode) }

type ApprovalKey = execution.ApprovalKey
type ApprovalStore = execution.ApprovalStore
type MemoryApprovalStore = execution.MemoryApprovalStore

func NewMemoryApprovalStore() *MemoryApprovalStore { return execution.NewMemoryApprovalStore() }
func NewApprovalKey(call ToolCall) ApprovalKey     { return execution.NewApprovalKey(call) }

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

func NewEffectScheduler() *EffectScheduler { return execution.NewEffectScheduler() }

type OutcomeResolver = execution.OutcomeResolver
type OutcomeResolveInput = execution.OutcomeResolveInput
type DefaultOutcomeResolver = execution.DefaultOutcomeResolver

func NewDefaultOutcomeResolver(policy Policy, approvals ApprovalStore) *DefaultOutcomeResolver {
	return execution.NewDefaultOutcomeResolver(policy, approvals)
}

type RunController = execution.RunController
type DefaultRunController = execution.DefaultRunController

func NewDefaultRunController() *DefaultRunController { return execution.NewDefaultRunController() }
