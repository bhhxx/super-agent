# Repository Details

## Architecture

```text
main.go
  -> app.LoadConfig
  -> app.NewSession
       -> app/instructions.Load
       -> llm.NewModel
       -> tools.DefaultRegistry / tools.NoTools
       -> runtime.NewEngineWithExecutorAndPolicy
       -> runtime/store.OpenDefault
       -> runtime.NewPersistentSession
  -> tui.New(session)
```

- `app/`: config and session assembly.
- `app/instructions/`: user memory and layered project instruction loading.
- `runtime/`: public aliases and constructors for runtime packages.
- `runtime/model/`: shared runtime types and interfaces.
- `runtime/machine/`: state, events, mutations, effects, reducer, transitions.
- `runtime/execution/`: effect runner, executor, scheduler, resolver, classifier, policy, approvals, run control.
- `runtime/engine/`: orchestration, state lock, lifecycle, dispatch, stale-result dropping.
- `runtime/session/`: channel boundary for TUI events and approvals.
- `runtime/store/`: durable JSONL session metadata, transcripts, checkpoints, compaction records.
- `llm/`: DeepSeek, OpenAI, Claude adapters.
- `tools/`: file tools, search, patch/write tools, bash runner, registry, no-tool mode.
- `tui/`: Bubble Tea event loop and approval UI.
- `tests/`: external package tests by module.

## Project Instructions

`app/instructions.Load` reads optional user memory from `~/.superagent/AGENTS.md`, then searches upward from the current working directory and merges project instructions from root to leaf. `AGENTS.md` wins in a directory; `CLAUDE.md` is loaded as lower-priority compatibility guidance only when that directory has no `AGENTS.md`. Each instruction file is capped at 128 KiB and oversized files return a clear path-specific error.

`app.NewSession` appends the merged instruction bundle to the built-in system prompt and passes one `system` message to the runtime. OpenAI-compatible providers send it as a chat `system` message. Claude sends it through the Anthropic `system` field. `ResetContext` clears conversation state but preserves `system` messages. Persistent replay also preserves system messages across reset records.

## Session Persistence

Sessions are stored under `~/.superagent/sessions/<session-id>/` as `meta.json` plus `events.jsonl`. Metadata includes session id, turn id, timestamps, provider/model, cwd, title, instruction fingerprint, and instruction source paths. `runtime/session` owns durable event emission for messages, approvals, tool results, cancel, reset, errors, checkpoints, and compact records. `runtime/store` replays messages for `/resume`.

TUI commands:

- `/memory`: show loaded instruction and memory source paths.
- `/permissions`: show current permission mode and tool approval status.
- `/permissions mode <ask|accept-edits|plan|bypass>`: change the active session permission mode.
- `/sessions`: list saved sessions.
- `/resume <id>`: load a prior transcript into the current engine.
- `/rename <id> <title>`: update session title.
- `/delete-session <id>`: remove an inactive saved session.
- `/compact [summary]`: summarize with the model when no summary is supplied, replace older non-system context with one summary message, and store original messages.
- `/undo`: restore the latest checkpoint file snapshot.

## Default Tools

`tools.DefaultRegistry` exposes `read_file`, `list_files`, `search`, `apply_patch`, `write_file`, `run_command`, `go_test`, `format`, `git_status`, `git_diff`, and `bash`. File-oriented tools use structured JSON inputs and reject paths outside the current working directory. `run_command` supports cwd, timeout, and output limits. `git_status` and `git_diff` are read-only. `apply_patch`, `write_file`, `run_command`, `go_test`, `format`, and `bash` are risky tools and require policy approval unless the active mode allows them. When configured with an OpenSandbox id, `run_command` and `bash` execute through `osb command run <sandbox-id> -o raw -- bash -lc <command>`.

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

Tool approval is a pre-transition classification step. `ToolCallsReceived` starts one batch, while each tool call is classified separately through `ToolCallAvailable` into `ToolCallNeedsApproval`, `ToolCallReadyToRun`, or a policy denial error. A batch is the context unit; a call is the approval and execution unit. `runtime/execution` owns command classification, protected path checks, network default-deny behavior, and structured permission requests.

## Runtime Terms

- `State`: current runtime phase.
- `Event`: fact that triggers a transition.
- `Mutation`: synchronous internal state change.
- `Effect`: requested work such as model calls, tool execution, or queue processing.
- `Transition`: pure state-machine decision.
- `Reducer`: applies mutations to `EngineState`.
- `ResultResolver`: turns `ExecutionResult` into raw runtime events.
- `EventClassifier`: turns raw tool events into approval or ready events.
- `Policy`: permission mode, allow/deny rules, command classification, and approval decision.
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
- `runtime/store/store.go`: writes and replays durable session records.
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
  },
  "sandbox": {
    "backend": "opensandbox",
    "opensandbox_id": "sandbox-id",
    "opensandbox_cli": "osb",
    "opensandbox_cwd": "/workspace"
  }
}
```

Supported modes are `ask`, `accept-edits`, `plan`, and `bypass`; `--yolo` maps to `bypass`. If the settings file is missing, the app creates a template on startup.

## Build

`./scripts/build-local.sh` builds the app and installs it as `/usr/local/bin/super-agent`. Set `SUPER_AGENT_INSTALL_DIR` to override the install directory for tests or automation.

## Documentation Maintenance

At the end of each work session, update `AGENTS.md` when project rules, architecture, commands, tests, or security guidance changed. Update this file when detailed architecture or runtime flow changed.
