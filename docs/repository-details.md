# Repository Details

## Architecture

```text
main.go
  -> app.LoadConfig
  -> app.NewSession
       -> app.LoadProjectInstructions
       -> llm.NewModel
       -> tools.NewRegistry / tools.NoTools
       -> runtime.NewEngine
       -> runtime.NewSession
  -> tui.New(session)
```

- `app/`: config and session assembly.
- `runtime/`: public aliases and constructors for runtime packages.
- `runtime/model/`: shared runtime types and interfaces.
- `runtime/machine/`: state, events, mutations, effects, reducer, transitions.
- `runtime/execution/`: effect runner, executor, scheduler, resolver, classifier, policy, approvals, run control.
- `runtime/engine/`: orchestration, state lock, lifecycle, dispatch, stale-result dropping.
- `runtime/session/`: channel boundary for TUI events and approvals.
- `llm/`: DeepSeek, OpenAI, Claude adapters.
- `tools/`: bash runner, registry, no-tool mode.
- `tui/`: Bubble Tea event loop and approval UI.
- `tests/`: external package tests by module.

## Project Instructions

`app.NewSession` reads `AGENTS.md` from the current working directory. When present, its contents become the initial `system` message passed to the runtime. OpenAI-compatible providers send it as a chat `system` message. Claude sends it through the Anthropic `system` field. `ResetContext` clears conversation state but preserves `system` messages.

## Runtime Rule

```text
QueuedEffect { RunID, EffectID, Effect }
  -> EffectRunner.Run -> EffectOutcome
  -> stale RunID check
  -> ResultResolver.Resolve -> raw Event
  -> EventClassifier.Classify -> classified Event
  -> Transition(state, event)
  -> TransitionResult { NextState, Mutations, Effects }
  -> Reducer.Apply -> enqueue owned effects
```

Tool approval is a pre-transition classification step. `ToolCallsReceived` starts one batch, while each tool call is classified separately through `ToolCallAvailable` into `ToolCallNeedsApproval` or `ToolCallReadyToRun`. A batch is the context unit; a call is the approval and execution unit.

## Runtime Terms

- `State`: current runtime phase.
- `Event`: fact that triggers a transition.
- `Mutation`: synchronous internal state change.
- `Effect`: requested work such as model calls, tool execution, or queue processing.
- `Transition`: pure state-machine decision.
- `Reducer`: applies mutations to `EngineState`.
- `ResultResolver`: turns `ExecutionResult` into raw runtime events.
- `EventClassifier`: turns raw tool events into approval or ready events.
- `Policy`: approval decision only.
- `ApprovalStore`: stores always-allow and auto-approve state.
- `RunController`: owns run id, cancel function, and stale-result checks.
- `EffectRunner`: executes effects and returns owned outcomes.
- `Engine`: scheduler, state lock, lifecycle, dispatch, effect drain, stale dropping.
- `Session`: channel boundary for UI events and approvals.

## Runtime Package Boundaries

- `runtime/machine/transition.go`: pure transition table.
- `runtime/machine/reducer.go`: applies `Mutation` values to `EngineState`.
- `runtime/engine/engine.go`: starts runs, dispatches events, drains queued effects.
- `runtime/execution/effect_executor.go`: calls the model or tool runner.
- `runtime/execution/result_resolver.go`: maps execution results to raw events.
- `runtime/execution/event_classifier.go`: converts tool availability into approval or execution events.
- `runtime/session/session.go`: serializes turns and emits UI-facing session events.
- `runtime/runtime.go`: re-exports runtime API surface for app/tests.

## Transition Table

| State | Event | Next | Mutations | Effects |
|---|---|---|---|---|
| Initializing | EngineReady | Idle | - | - |
| Idle | UserMessageSubmitted | WaitingLLM | AppendUserMessage | CallModel |
| WaitingLLM | AssistantMessageReceived | Idle | AppendAssistantMessage | - |
| WaitingLLM | ToolBatchReceived | AdvancingQueue | AppendAssistantMessage, SetToolCallBatch | ProcessNextToolCall |
| WaitingApproval | ApprovalGranted | RunningTool | ClearPendingTool | RunTool |
| WaitingApproval | ApprovalAlwaysGranted | RunningTool | ClearPendingTool | RunTool |
| WaitingApproval | ApprovalDenied | AdvancingQueue | ClearPendingTool, AppendToolResult | ProcessNextToolCall |
| RunningTool | ToolResultReceived | AdvancingQueue | AppendToolResult | ProcessNextToolCall |
| AdvancingQueue | ToolBatchFinished | WaitingLLM | ClearToolCallBatch | CallModel |
| AdvancingQueue | ToolCallNeedsApproval | WaitingApproval | SetPendingTool, AdvanceToolCallBatch | - |
| AdvancingQueue | ToolCallReadyToRun | RunningTool | AdvanceToolCallBatch | RunTool |
| any | ErrorOccurred | Idle | ClearPendingTool, ClearToolCallBatch, ClearPendingEffects | - |
| any | CancelRequested | Idle | ClearPendingTool, ClearToolCallBatch, ClearPendingEffects | - |
| any | ResetRequested | Idle | ResetContext | - |

## Git And PR Notes

- Use concise conventional commit messages, for example `fix: preserve reasoning replay`.
- Name branches by scope: `feat/session-events`, `fix/tool-approval`.
- PRs should include purpose, main files changed, test output, and local config notes.
- Add screenshots only for visible TUI changes.

## Config

`main.go` loads `.env` with `godotenv`. Supported variables include `LLM_PROVIDER`, provider API keys, provider base URLs, and model names. DeepSeek defaults to `DEEPSEEK_API_KEY` and falls back to `OPENAI_API_KEY`.

## Documentation Maintenance

At the end of each work session, update `AGENTS.md` when project rules, architecture, commands, tests, or security guidance changed. Update this file when detailed architecture or runtime flow changed.
