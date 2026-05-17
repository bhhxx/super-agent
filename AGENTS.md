# Repository Guidelines

## Project Architecture

This is a Go agent runtime with a state-machine core, provider adapters, local tools, and a Bubble Tea TUI.

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

Runtime rule:

```text
Effect executed → ExecutionResult → resolveResult (pure function) → raw Event
  → Engine asks Policy.ClassifyToolCall(call, toolPolicyInput) when tool approval is needed
  → classified Event → Transition(state, event)
  → TransitionResult { NextState, Mutations, Effects } → applyMutation → runEffects
```

Transition table (tool events shown after policy classification):

- `State`: current runtime phase.
- `Event`: fact that triggers a transition.
- `Mutation`: synchronous internal state change.
- `Effect`: work requested by a transition, such as model calls, tool execution, or internal tool-queue processing.
- `Transition`: pure state-machine decision. All state changes go through Transition → Mutation — no other code touches engine state directly.
- `Policy`: approval rules. Classifies one tool call with `ToolPolicyInput`, including tool specs. Default policy handles risk-checking, always-allow, and yolo configuration.
- `Engine`: scheduler. Receives events, calls Transition, applies mutations, executes effects. Does not own approval logic — that belongs to Policy.
- `Session`: channel boundary for UI events and approvals. Runs one turn (`RunTurn`) per user input, guarded against concurrent calls.

| State | Event | Next | Mutations | Effects |
|---|---|---|---|---|
| Initializing | EngineReady | Idle | - | - |
| Idle | UserMessageSubmitted | WaitingLLM | AppendUserMessage | CallModel |
| WaitingLLM | AssistantMessageReceived | Idle | AppendAssistantMessage | - |
| WaitingLLM | ToolCallBatchFirstNeedsApproval | WaitingApproval | AppendAssistantMessage, SetQueuedToolCalls, SetPendingTool | - |
| WaitingLLM | ToolCallBatchFirstReadyToRun | RunningTool | AppendAssistantMessage, SetQueuedToolCalls | RunTool |
| WaitingApproval | ApprovalGranted | RunningTool | ClearPendingTool | RunTool |
| WaitingApproval | ApprovalAlwaysGranted | RunningTool | ClearPendingTool, AddAlwaysAllow | RunTool |
| WaitingApproval | ApprovalDenied | AdvancingQueue | ClearPendingTool, AppendToolResult | ProcessNextToolCall |
| RunningTool | ToolResultReceived | AdvancingQueue | AppendToolResult | ProcessNextToolCall |
| AdvancingQueue | NoMoreToolCalls | WaitingLLM | - | CallModel |
| AdvancingQueue | QueuedToolCallNeedsApproval | WaitingApproval | SetPendingTool, PopQueuedToolCall | - |
| AdvancingQueue | QueuedToolCallReadyToRun | RunningTool | PopQueuedToolCall | RunTool |
| any | ErrorOccurred | Idle | ClearPendingTool, ClearQueuedToolCalls, ClearPendingEffects | - |
| any | CancelRequested | Idle | ClearPendingTool, ClearQueuedToolCalls, ClearPendingEffects | - |
| any | ResetRequested | Idle | ResetContext | - |

## Build, Test, and Development Commands

- `go run .`: run the TUI with the default provider, DeepSeek.
- `go run . --no-tools`: run without tool calling.
- `go run . --yolo`: auto-approve tool execution.
- `NO_TOOLS=true go run .`: disable tools through environment config.
- `YOLO=true go run .`: auto-approve tools through environment config.
- `go test ./...`: run all tests.
- `gofmt -w <files>`: format changed Go files before committing.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Keep package names short and lower-case: `runtime`, `llm`, `tools`, `tui`.

Prefer the existing state-machine vocabulary: `State`, `Event`, `Mutation`, `Effect`, and `Transition`. Keep runtime control flow in `runtime/transition.go` and orchestration in `runtime/engine.go`; avoid scattering transition rules through UI or provider code.

## Testing Guidelines

Tests use Go's standard `testing` package. Add tests under `tests/<package>/` with external test packages such as `runtime_test` or `tui_test`.

Name tests by behavior, for example `TestToolCallFeedsResultBackToModel` or `TestEscCancelsPendingApproval`. For runtime changes, cover both state transitions and observable engine behavior when practical.

## Commit & Pull Request Guidelines

Recent history uses short conventional commits, such as `feat: refactor Claude role handling`, plus merge commits. Use concise imperative messages: `fix: preserve reasoning replay`, `test: cover approval cancel`.

Name branches by work scope, not by one commit or PR title: `feat/session-events`, `fix/tool-approval`, `refactor/runtime-session`. Use commits for concrete steps and the PR title for the final merged result. With squash merge, make the PR title the final conventional commit; with rebase merge, keep every commit clean.

Pull requests should include the purpose, main files changed, test command output, and any config needed to run locally. Link issues when relevant. Add screenshots only for visible TUI changes.

## Security & Configuration Tips

`main.go` loads `.env` with `godotenv`. Do not commit secrets. Supported provider variables include `LLM_PROVIDER`, `DEEPSEEK_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, provider base URLs, and model names. DeepSeek defaults to `DEEPSEEK_API_KEY` and falls back to `OPENAI_API_KEY`.
