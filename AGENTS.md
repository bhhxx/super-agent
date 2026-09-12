# Repository Guidelines

## Essentials

- Go project: agent runtime, LLM adapters, local tools, Bubble Tea TUI.
- Design pattern: hexagonal architecture with a functional core and imperative shell. `runtime/machine` is the pure domain core; engine, session, TUI, LLM, tools, and store are ports or adapters around it.
- State-machine flow is `Event -> validated MachineSnapshot -> Transition -> RuntimeDataChange + ActionPlan -> transactional RuntimeDataChangeApplier/Executor -> ActionResultResolver -> Event`; dependencies point toward the machine.
- `State` is the current execution state. `RuntimeData` is the complete mutable machine data. A `RuntimeDataChange` constructs the next runtime data; an `ActionPlan` atomically clears obsolete queued work and schedules actions that run only after commit.
- Keep `RunID` stale filtering in the engine. Keep state, call-id, queue guards, and invariants in `runtime/machine`.
- RuntimeDataChangeAppliers must clone, apply, and validate runtime data; the engine commits runtime data and the transition's action plan under one lock only after validation.
- The engine owns the single agent loop and notifies a per-turn state observer after state-changing transitions; the session supplies approval, streaming, notification, and persistence ports without scheduling actions.
- Start turns through `Engine.RunTurn`; route other external machine events through `Engine.DispatchEvent`. Only `UserMessageSubmitted` starts a run, and `AwaitApproval` keeps approval waiting inside the scheduled-action loop.
- Keep state-machine logic in `runtime/machine/transition.go`; transitions use one package-private static registry keyed by state and event kind.
- Keep state definitions in `runtime/machine/state.go`, complete machine data in `runtime/machine/runtime_data.go`, runtime-data changes in `runtime/machine/runtime_data_change.go`, action plans in `runtime/machine/action_plan.go`, and tool-batch data in `runtime/machine/tool_batch.go`.
- Keep orchestration in `runtime/engine/`; constructors belong in `engine.go`, commands in `commands.go`, scheduled-action draining in `action_loop.go`, and queries in `query.go`.
- In `runtime/engine`, reference `machine`, `execution`, and `protocol` owners explicitly; do not re-export them through internal aliases.
- Keep scheduled-action execution in `runtime/execution/`.
- Map scheduled-action results directly to transition events with `runtime/execution.ActionResultResolver`.
- Keep session/UI boundary in `runtime/session/`.
- Use `SessionNotification` for runtime-session output and convert it to `tui.ConversationNotification` at the app boundary; reserve `machine.Event` for state-machine input.
- Keep turn I/O wiring in `runtime/session/turn.go` and history use cases in `runtime/session/history.go`.
- Keep storage and filesystem access behind `runtime/session.Repository` and `runtime/session.Workspace`.
- Keep TUI message routing in `tui/update.go`, commands in `tui/commands.go`, and rendering outside the update loop.
- Map runtime states to presentation-only `tui.AgentStatus` values in `app/tui_adapter.go`; TUI must not define runtime state enums.
- Keep durable session storage in `store/` and filesystem checkpoint access in `workspace/`.
- Load layered instructions with `app/instructions`: user-level spec, root-to-leaf `AGENTS.md`, fallback `CLAUDE.md`.
- Preserve `system` messages such as project instructions across reset.
- Do not scatter transition rules into `tui/`, `llm/`, or `tools/`.
- Use existing vocabulary: `State`, `RuntimeData`, `Event`, `RuntimeDataChange`, `ActionPlan`, `ScheduledAction`, `Transition`.
- Follow the hexagonal architecture in `docs/architecture.md`; `tui` must not import `runtime`.
- Keep the TUI as the only interaction surface; do not add headless, server, or alternate UI entry points.
- LLM and tool adapters may import `runtime/protocol`, not the root `runtime` facade.
- MCP stdio adapters live in `tools/mcp`; discovered tools join `tools.Registry` atomically and remain risky under the common permission policy.
- `app.MCPController` coordinates MCP lifecycle, dynamic registry changes, and atomic settings persistence; TUI only calls its application-facing adapter.
- More detail: `docs/repository-details.md`.
- Transition teaching guide: `teach/agent-transition.md`.
- Agent-loop teaching guide: `teach/agent-loop.md`.
- State, context, and worked-transition guides: `teach/agent-state.md`, `teach/agent-memory.md`, `teach/agent-transition-example.md`.

## Documentation

- At the end of each work session, proactively update `AGENTS.md`.
- Keep `AGENTS.md` aligned with current architecture, commands, tests, and security rules.
- Update `docs/repository-details.md` when architecture or runtime flow changes.

## Commands

- `go run .`: run TUI.
- `go run . --no-tools`: run without tools.
- `go run . --yolo`: auto-approve tools.
- `go run . --approval-mode <ask|accept-edits|plan|bypass>`: set permission mode.
- TUI session commands: `/instructions`, `/permissions`, `/permissions mode <mode>`, `/mcp list`, `/mcp add <name> <command> [args...]`, `/mcp remove <name>`, `/mcp restart <name>`, `/sessions`, `/resume <id>`, `/rename <id> <title>`, `/delete-session <id>`, `/fork [title]`, `/memory`, `/remember <text>`, `/forget`, `/attach <path>`, `/attachments`, `/compact`, `/undo`.
- Agent commands: `/agent`, `/agent <name>`, `/plan`, `/build`, and `/mode <plan|build>`; custom profiles can restrict tools and live under `agents` in settings.
- The `delegate` tool creates persistent child sessions; cancellation follows the parent context, and optional worktrees live under `.super-agent/worktrees/`.
- `/fork [title]` branches the transcript; `/memory`, `/remember <text>`, and `/forget` manage cross-session memory.
- Workflow commands: `/review`, `/diff`, `/fix-ci`, `/branch`, `/commit-message`, and `/diagnostics <path>`.
- Extension commands: `/commands`, `/skills`, and `/plugins`; discovery uses user/project `.superagent` directories.
- `/export <markdown|json>` and `/share` write local files under `.super-agent/exports/`.
- `/attach <path>` queues a bounded workspace attachment for the next turn; `/attachments` lists the queue.
- While a turn runs, `Enter` cancels and steers with the new prompt; `Tab` queues a follow-up. Queued prompts run in order.
- The footer previews the first three queued prompts and the remaining count.
- The footer shows a `states:` history of the current turn's state transitions (for example `WaitingLLM → AdvancingQueue → RunningTool`); consecutive repeats collapse and the history resets when a new turn starts.
- Manual run cancellation clears queued prompts; steering cancellation preserves them.
- Below 18 terminal rows, use compact footer rendering and keep viewport/input dimensions positive.
- Keep long command output in the scrollable viewport and bound footer status height.
- Approval UI supports arrows/Enter plus `1/y`, `2/a`, and `3/n`; ignore duplicate input after submission.
- The composer is multiline: `Ctrl+J`, `Shift+Enter`, or `Alt+Enter` inserts a newline; `Enter` submits.
- Typing `/` opens the command palette; arrows select and `Tab` or `Enter` completes commands.
- The full command palette shows descriptions and argument hints; compact mode shows names only.
- Prompt-history navigation preserves and restores the current unsubmitted draft.
- `go test ./...`: run all tests.
- `gofmt -w <files>`: format changed Go files.
- `./scripts/coverage.sh`: run external tests with whole-project coverage.
- `./scripts/verify.sh`: run vet, tests, race detection, and coverage.
- `./scripts/build-local.sh`: install `/usr/local/bin/super-agent`.

## Tests

- All test code must live under `tests/`.
- Do not add new `_test.go` files inside production package directories.
- Use external test packages such as `runtime_test` or `tui_test`.
- Name tests by behavior, for example `TestToolCallFeedsResultBackToModel`.
- Runtime changes should cover transitions and observable engine behavior when practical.
- Transition tests should assert complete runtime-data-change/action-queue-change/scheduled-action order.
- Reset tests should prove system messages are preserved.
- GitHub Actions runs `./scripts/verify.sh` for pushes and pull requests.

## Security

- Do not commit secrets.
- Runtime switches come from `.env` and environment variables.
- `YOLO=true` in `.env` enables bypass only when no explicit `--approval-mode` flag was passed; the flag always wins over the environment.
- LLM provider config comes from `~/.superagent/settings.json`.
- Permission mode and allow/deny rules come from `~/.superagent/settings.json`.
- MCP stdio server definitions come from the top-level `mcp_servers` settings map.
- LSP stdio server definitions come from `lsp_servers`; configured servers expose diagnostics, symbols, definitions, references, and outlines as tools.
- Extensions configure custom commands, lifecycle hooks, skills, and local plugin manifests; tool hooks must use recursion-safe direct execution.
- Structured JSONL telemetry correlates run/action IDs, transitions, tools, durations, errors, and token estimates.
- `web_search` and `browser_fetch` are risky network tools; browser fetch blocks local/private targets and enforces redirect, timeout, and response limits.
- Command classification routes approvals but is not a security boundary.
- Linux command tools use strict bubblewrap isolation by default with a read-only host root, writable workspace, policy-controlled networking, ephemeral home/tmp, and `prlimit` resource bounds.
- Strict sandbox mode fails closed when `bwrap` or `prlimit` is unavailable; unsupported platforms require explicit `sandbox.mode: off`.
