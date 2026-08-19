# Repository Details

## Architecture

The dependency rule and target package boundaries are defined in `docs/architecture.md`.

```text
main.go
  -> app.LoadConfig
  -> app.NewSession
       -> app/instructions.Load
       -> llm.NewModel
       -> tools.DefaultRegistry / tools.NoTools
       -> runtime.NewEngineWithExecutorAndPolicy
       -> store.OpenDefault / store.NewRepository
       -> workspace.Workspace
       -> runtime.CreatePersistentSession
  -> app.NewTUIConversation(session)
  -> tui.New(conversation port)
```

- `app/`: config and session assembly.
- `app/instructions/`: user-level spec and layered project instruction loading.
- `runtime/`: public aliases and constructors for runtime packages.
- `runtime/protocol/`: model and tool adapter contracts (`Message`, `ToolCall`, `Model`, `ToolRunner`).
- `runtime/permission/`: permission request and command classification vocabulary.
- `runtime/machine/`: state, events, mutations, effects, reducer, transitions.
- `runtime/execution/`: effect runner, executor, scheduler, outcome resolver, command analyzer, policy, approvals, run control.
- `runtime/engine/`: orchestration, state lock, lifecycle, dispatch, stale-result dropping.
- `runtime/session/`: application use cases, event output, and persistence/workspace ports.
- `store/`: durable JSONL session metadata, transcripts, checkpoints, compaction records.
- `workspace/`: filesystem checkpoint capture and undo restoration adapter.
- `llm/`: DeepSeek, OpenAI, Claude adapters.
- `tools/`: file tools, search, patch/write tools, bash runner, registry, no-tool mode.
- `tui/`: Bubble Tea inbound adapter with runtime-independent display DTOs and a `Conversation` input port.
- `app/tui_adapter.go`: composition-boundary conversion between runtime and TUI values.
- `tests/`: external package tests by module.

## Project Instructions

`app/instructions.Load` reads the optional user-level spec from `~/.superagent/AGENTS.md`, then searches upward from the current working directory and merges project instructions from root to leaf. `AGENTS.md` wins in a directory; `CLAUDE.md` is loaded as lower-priority compatibility guidance only when that directory has no `AGENTS.md`. Each instruction file is capped at 128 KiB and oversized files return a clear path-specific error.

`app.NewSession` appends the merged instruction bundle to the built-in system prompt and passes one `system` message to the runtime. OpenAI-compatible providers send it as a chat `system` message. Claude sends it through the Anthropic `system` field. `ResetContext` clears conversation state but preserves `system` messages. Persistent replay also preserves system messages across reset records.

## Session Persistence

Sessions are stored under `~/.superagent/sessions/<session-id>/` as `meta.json` plus `events.jsonl`. Metadata includes session id, turn id, timestamps, provider/model, cwd, title, instruction fingerprint, and instruction source paths. `runtime/session` owns durable event emission for messages, approvals, tool results, cancel, reset, errors, checkpoints, and compact records. The top-level `store` adapter replays messages for `/resume`.

During a turn the session emits a snapshot after every state-changing transition while effects drain, so the TUI header follows live states (for example `RunningTool` while a tool executes) instead of only snapshot points. The header shows raw state names such as `Idle` and `WaitingLLM`.

TUI commands:

- `/instructions`: show loaded instruction source paths.
- `/permissions`: show current permission mode and tool approval status.
- `/permissions mode <ask|accept-edits|plan|bypass>`: change the active session permission mode.
- `/sessions`: list saved sessions.
- `/resume <id>`: load a prior transcript into the current engine.
- `/rename <id> <title>`: update session title.
- `/delete-session <id>`: remove an inactive saved session.
- `/compact [summary]`: summarize with the model when no summary is supplied, replace older non-system context with one summary message, and store original messages.
- `/undo`: restore the latest non-empty checkpoint, truncate the stored transcript to that checkpoint, and reload the conversation so workspace and history stay consistent.

TUI keys: `Enter` submits when idle and cancels/restarts with steering input while a turn runs; `Tab` queues a follow-up during a run. `Ctrl+J`, `Shift+Enter`, or `Alt+Enter` inserts a newline. Typing `/` opens the command palette; arrows select and `Tab` or `Enter` completes a command. `Esc` clears input or cancels a run, `Ctrl+U` clears input, `Ctrl+C` cancels or quits, arrows otherwise navigate multiline input or recall single-line prompts without losing the current draft, and Page Up/Down scrolls.

The full slash-command palette shows command descriptions and argument hints; compact mode shows names only.

The footer previews up to three queued follow-ups in execution order and summarizes any remaining count.
A `states:` line lists the current turn's state transitions with consecutive repeats collapsed (for example `WaitingLLM → AdvancingQueue → WaitingApproval → RunningTool`); the history resets when a new turn starts and the line hides in compact mode.
Manual cancellation with `Esc` or `Ctrl+C` clears queued follow-ups; steering cancellation preserves them.
Below 18 terminal rows, the footer enters compact mode: queue details collapse, the slash palette shows three scrolling choices, and viewport/input dimensions remain positive.
Tool approval is a selectable menu: arrows or `j`/`k` move, `Enter` confirms, and `1`/`y`, `2`/`a`, `3`/`n` remain direct shortcuts. Submitted decisions ignore repeated keys until the runtime advances.

## Default Tools

`tools.DefaultRegistry` exposes `read_file`, `list_files`, `search`, `apply_patch`, `write_file`, `run_command`, `go_test`, `format`, `git_status`, `git_diff`, and `bash`. File-oriented tools use structured JSON inputs and reject paths outside the current working directory. `run_command` supports cwd, timeout, and output limits. `git_status` and `git_diff` are read-only. `apply_patch`, `write_file`, `run_command`, `go_test`, `format`, and `bash` are risky tools and require policy approval unless the active mode allows them.

## Runtime Rule

```text
QueuedEffect { RunID, EffectID, Effect }
  -> EffectRunner.Run -> EffectOutcome
  -> stale RunID check
  -> OutcomeResolver.Resolve -> transition Event
  -> SnapshotFrom(EngineState) -> validated MachineSnapshot
  -> Transition(snapshot, event)
  -> TransitionResult { NextState, Mutations, Effects }
  -> Reducer.Reduce on cloned state -> ValidateState
  -> atomic EngineState + scheduler commit
```

`OutcomeResolver` maps model/tool outcomes directly to events accepted by the transition table. It starts tool batches and classifies each queued call into `ToolCallNeedsApproval`, `ToolCallReadyToRun`, or a policy denial error. A batch is the context unit; a call is the approval and execution unit. `runtime/execution` owns command classification, protected path checks, network default-deny behavior, and structured permission requests.

## Runtime Terms

- `State`: current runtime phase.
- `Event`: fact that triggers a transition.
- `Mutation`: synchronous internal state change.
- `Effect`: requested work such as model calls, tool execution, or queue processing.
- `MachineSnapshot`: validated read-only view containing only transition guards.
- `Transition`: pure state-machine decision with state, call, and queue guards.
- `Reducer`: applies mutations to a cloned `EngineState`, validates it, and describes scheduler operations.
- `OutcomeResolver`: turns `ExecutionResult` into transition-ready events and applies tool policy.
- `Policy`: permission mode, allow/deny rules, command classification, and approval decision.
- `ApprovalStore`: stores always-allow and auto-approve state.
- `RunController`: owns run id, cancel function, and stale-result checks.
- `EffectRunner`: executes effects and returns owned outcomes.
- `Engine`: scheduler, state lock, lifecycle, dispatch, effect drain, stale dropping.
- `Session`: channel boundary for UI events and approvals.

## Runtime Package Boundaries

- `runtime/machine/transition.go`: pure context-aware transition table.
- `runtime/machine/snapshot.go`: machine snapshot construction and state invariants.
- `runtime/machine/reducer.go`: side-effect-free transactional mutation reduction.
- `runtime/engine/engine.go`: engine construction and dependencies.
- Engine files name `machine`, `execution`, and `protocol` types explicitly; the package has no internal alias facade.
- `runtime/engine/commands.go`: lifecycle, approval, policy, and context commands.
- `runtime/engine/effect_loop.go`: transition dispatch and queued-effect draining.
- `runtime/engine/query.go`: state queries and snapshots.
- `runtime/execution/effect_executor.go`: calls the model or tool runner.
- `runtime/execution/outcome_resolver.go`: maps execution results to transition-ready events and classifies tool calls.
- `runtime/session/session.go`: serializes turns and coordinates application use cases.
- `runtime/session/events.go`: application event protocol.
- `runtime/session/turn.go`: turn execution and approval flow.
- `runtime/session/history.go`: resume, rename, delete, compact, and undo use cases.
- `runtime/session/persistence.go`: repository notifications.
- `runtime/session/repository.go`: persistence and workspace ports, including checkpoint creation, `CheckpointState`, and `TruncateAfter`.
- `store/repository.go`: `runtime/session.Repository` JSONL adapter.
- `workspace/workspace.go`: `runtime/session.Workspace` filesystem adapter.
- `tui/commands.go`: slash-command handling and turn submission.
- `tui/update.go`: Bubble Tea message routing and UI state updates.
- `tui/actions.go`: cancellation and clipboard actions.
- `tui/view.go`: top-level layout and informational views.
- `tui/styles.go`: visual theme construction.
- `store/store.go`: writes and replays durable session records. All access is serialized; creation writes the transcript first and `meta.json` last so partial failures cannot leave orphan sessions; undo uses `CheckpointUndo` (skipping empty checkpoints) and an atomic `TruncateAfter`.
- `runtime/api_*.go`: compatibility facade grouped by model, machine, execution, engine, and session. It exposes session persistence ports and metadata without importing concrete adapters. The pre-facade `ToolCallsReceived`, `ToolCallAvailable`, `EventClassifier`, and `ResultResolver` names were intentionally retired in favor of `ToolBatchReceived`/`ToolCallNeedsApproval` and `OutcomeResolver` and are not re-exported.
- `runtime/execution/command_analyzer.go`: shell command classification and metadata extraction.

## Transition Table

| State | Event | Next | Mutations | Effects |
|---|---|---|---|---|
| Initializing | EngineReady | Idle | - | - |
| Idle | UserMessageSubmitted | WaitingLLM | AppendUserMessage | CallModel |
| WaitingLLM | AssistantMessageReceived | Idle | AppendAssistantMessage | - |
| WaitingLLM | ToolBatchReceived | AdvancingQueue | AppendAssistantMessage, SetToolCallBatch | ProcessNextToolCall |
| WaitingApproval | ApprovalGranted | RunningTool | SetCurrentTool, ClearPendingTool | RunTool |
| WaitingApproval | ApprovalAlwaysGranted | RunningTool | SetCurrentTool, ClearPendingTool | RunTool |
| WaitingApproval | ApprovalDenied | AdvancingQueue | ClearPendingTool, AppendToolResult | ProcessNextToolCall |
| RunningTool | ToolResultReceived | AdvancingQueue | AppendToolResult, ClearCurrentTool | ProcessNextToolCall |
| AdvancingQueue | ToolBatchFinished | WaitingLLM | ClearToolCallBatch | CallModel |
| AdvancingQueue | ToolCallNeedsApproval | WaitingApproval | SetPendingTool, AdvanceToolCallBatch | - |
| AdvancingQueue | ToolCallReadyToRun | RunningTool | AdvanceToolCallBatch, SetCurrentTool | RunTool |
| any | ErrorOccurred | Idle | ClearPendingTool, ClearCurrentTool, ClearToolCallBatch, ClearPendingEffects | - |
| any | CancelRequested | Idle | ClearPendingTool, ClearCurrentTool, ClearToolCallBatch, ClearPendingEffects | - |
| any | ResetRequested | Idle | ResetContext | - |

## Git And PR Notes

- Use concise conventional commit messages, for example `fix: preserve reasoning replay`.
- Name branches by scope: `feat/session-events`, `fix/tool-approval`.
- PRs should include purpose, main files changed, test output, and local config notes.
- Add screenshots only for visible TUI changes.

## Config

`main.go` loads `.env` with `godotenv` for runtime switches such as `NO_TOOLS` and `YOLO`. LLM provider config is read from `~/.superagent/settings.json`, including provider name, API keys, base URLs, and model names. Permission config also lives there:

```json
{
  "permissions": {
    "mode": "ask",
    "network": "deny",
    "allow_tools": [],
    "deny_tools": [],
    "allow_command_prefixes": [],
    "deny_command_prefixes": [],
    "allow_paths": [],
    "deny_paths": [],
    "allow_env": [],
    "deny_env": []
  }
}
```

Supported modes are `ask`, `accept-edits`, `plan`, and `bypass`; `--yolo` maps to `bypass`. The `YOLO=true` environment variable enables bypass only when no explicit `--approval-mode` flag was passed, so a checked-in `.env` cannot silently disable permission prompts. Invalid modes fail config load. If the settings file is missing, the app creates a template on startup.

## Build

`./scripts/build-local.sh` builds the app and installs it as `/usr/local/bin/super-agent`. Set `SUPER_AGENT_INSTALL_DIR` to override the install directory for tests or automation.

## Documentation Maintenance

At the end of each work session, update `AGENTS.md` when project rules, architecture, commands, tests, or security guidance changed. Update this file when detailed architecture or runtime flow changed.
