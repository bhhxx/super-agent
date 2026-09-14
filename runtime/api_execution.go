package runtime

import "super-agent/runtime/execution"

// ErrApprovalDismissed reports that the interface gave up waiting for an
// approval decision; the engine cancels the run instead of failing it.
var ErrApprovalDismissed = execution.ErrApprovalDismissed

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

type ScheduledActionInput = execution.ScheduledActionInput
type ScheduledActionExecutor = execution.ScheduledActionExecutor
type DefaultScheduledActionExecutor = execution.DefaultScheduledActionExecutor
type ApprovalWaiter = execution.ApprovalWaiter
type ApprovalWaitFunc = execution.ApprovalWaitFunc

func NewDefaultScheduledActionExecutor(model Model, tools ToolRunner) *DefaultScheduledActionExecutor {
	return execution.NewDefaultScheduledActionExecutor(model, tools)
}

type ScheduledActionResult = execution.ScheduledActionResult
type ModelReplied = execution.ModelReplied
type ToolFinished = execution.ToolFinished
type ToolQueueChecked = execution.ToolQueueChecked
type ApprovalReceived = execution.ApprovalReceived

type RunID = execution.RunID
type ActionID = execution.ActionID
type QueuedAction = execution.QueuedAction
type ActionCompletion = execution.ActionCompletion
type ScheduledActionRunner = execution.ScheduledActionRunner
type DefaultScheduledActionRunner = execution.DefaultScheduledActionRunner

func NewDefaultScheduledActionRunner(executor ScheduledActionExecutor) *DefaultScheduledActionRunner {
	return execution.NewDefaultScheduledActionRunner(executor)
}

type ActionQueue = execution.ActionQueue

func NewActionQueue() *ActionQueue { return execution.NewActionQueue() }

type ActionResultResolver = execution.ActionResultResolver
type ActionResultInput = execution.ActionResultInput
type DefaultActionResultResolver = execution.DefaultActionResultResolver

func NewDefaultActionResultResolver(policy Policy, approvals ApprovalStore) *DefaultActionResultResolver {
	return execution.NewDefaultActionResultResolver(policy, approvals)
}

type RunController = execution.RunController
type DefaultRunController = execution.DefaultRunController

func NewDefaultRunController() *DefaultRunController { return execution.NewDefaultRunController() }
