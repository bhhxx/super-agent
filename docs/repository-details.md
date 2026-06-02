# Repository Details

## Architecture

```text
main.go
  -> app.LoadConfig
  -> app.NewSession
       -> llm.NewModel
       -> tools.NewRegistry / tools.NoTools
       -> runtime.NewEngine
       -> runtime.NewSession
  -> tui.New(session)
```

- `app/`: config and session assembly.
- `runtime/`: state, events, mutations, effects, transitions, engine, session boundary.
- `llm/`: DeepSeek, OpenAI, Claude adapters.
- `tools/`: bash runner, registry, no-tool mode.
- `tui/`: Bubble Tea event loop and approval UI.
- `tests/`: external package tests by module.

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

Tool approval is a pre-transition classification step. `EventClassifier` turns raw tool events such as `ToolCallsReceived` and `NextToolCallAvailable` into classified events such as `ToolCallBatchFirstNeedsApproval`, `ToolCallBatchFirstReadyToRun`, `QueuedToolCallNeedsApproval`, or `QueuedToolCallReadyToRun`. `Transition` consumes only classified runtime events.

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

## Transition Table

| State | Event | Next | Mutations | Effects |
|---|---|---|---|---|
| Initializing | EngineReady | Idle | - | - |
| Idle | UserMessageSubmitted | WaitingLLM | AppendUserMessage | CallModel |
| WaitingLLM | AssistantMessageReceived | Idle | AppendAssistantMessage | - |
| WaitingLLM | ToolCallBatchFirstNeedsApproval | WaitingApproval | AppendAssistantMessage, SetQueuedToolCalls, SetPendingTool | - |
| WaitingLLM | ToolCallBatchFirstReadyToRun | RunningTool | AppendAssistantMessage, SetQueuedToolCalls | RunTool |
| WaitingApproval | ApprovalGranted | RunningTool | ClearPendingTool | RunTool |
| WaitingApproval | ApprovalAlwaysGranted | RunningTool | ClearPendingTool | RunTool |
| WaitingApproval | ApprovalDenied | AdvancingQueue | ClearPendingTool, AppendToolResult | ProcessNextToolCall |
| RunningTool | ToolResultReceived | AdvancingQueue | AppendToolResult | ProcessNextToolCall |
| AdvancingQueue | NoMoreToolCalls | WaitingLLM | - | CallModel |
| AdvancingQueue | QueuedToolCallNeedsApproval | WaitingApproval | SetPendingTool, PopQueuedToolCall | - |
| AdvancingQueue | QueuedToolCallReadyToRun | RunningTool | PopQueuedToolCall | RunTool |
| any | ErrorOccurred | Idle | ClearPendingTool, ClearQueuedToolCalls, ClearPendingEffects | - |
| any | CancelRequested | Idle | ClearPendingTool, ClearQueuedToolCalls, ClearPendingEffects | - |
| any | ResetRequested | Idle | ResetContext | - |

## Git And PR Notes

- Use concise conventional commit messages, for example `fix: preserve reasoning replay`.
- Name branches by scope: `feat/session-events`, `fix/tool-approval`.
- PRs should include purpose, main files changed, test output, and local config notes.
- Add screenshots only for visible TUI changes.

## Config

`main.go` loads `.env` with `godotenv`. Supported variables include `LLM_PROVIDER`, provider API keys, provider base URLs, and model names. DeepSeek defaults to `DEEPSEEK_API_KEY` and falls back to `OPENAI_API_KEY`.
