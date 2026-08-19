# Repository Guidelines

## Essentials

- Go project: agent runtime, LLM adapters, local tools, Bubble Tea TUI.
- Design pattern: hexagonal architecture with a functional core and imperative shell. `runtime/machine` is the pure domain core; engine, session, TUI, LLM, tools, and store are ports or adapters around it.
- State-machine flow is `Event -> validated MachineSnapshot -> Transition -> Mutation + Effect -> transactional Reducer/Executor -> OutcomeResolver -> Event`; dependencies point toward the machine.
- Keep `RunID` stale filtering in the engine. Keep state, call-id, queue guards, and invariants in `runtime/machine`.
- Reducers must clone, apply, validate, and return descriptive scheduler operations; the engine commits state and scheduler changes under one lock only after validation.
- The engine notifies a per-turn state observer after state-changing transitions; the session uses it to emit live snapshots so the TUI header tracks states such as `RunningTool`.
- Keep state-machine logic in `runtime/machine/transition.go`.
- Keep orchestration in `runtime/engine/`; constructors belong in `engine.go`, commands in `commands.go`, effect draining in `effect_loop.go`, and queries in `query.go`.
- In `runtime/engine`, reference `machine`, `execution`, and `protocol` owners explicitly; do not re-export them through internal aliases.
- In `runtime/engine`, reference `machine`, `execution`, and `protocol` owners explicitly; do not re-export them through internal aliases.
- Keep effect execution in `runtime/execution/`.
- Map effect results directly to transition events with `runtime/execution.OutcomeResolver`.
- Keep session/UI boundary in `runtime/session/`.
- Keep turn flow in `runtime/session/turn.go` and history use cases in `runtime/session/history.go`.
- Keep storage and filesystem access behind `runtime/session.Repository` and `runtime/session.Workspace`.
- Keep TUI message routing in `tui/update.go`, commands in `tui/commands.go`, and rendering outside the update loop.
- Map runtime states to presentation-only `tui.AgentStatus` values in `app/tui_adapter.go`; TUI must not define runtime state enums.
- Map runtime states to presentation-only `tui.AgentStatus` values in `app/tui_adapter.go`; TUI must not define runtime state enums.
- Keep durable session storage in `store/` and filesystem checkpoint access in `workspace/`.
- Load layered instructions with `app/instructions`: user-level spec, root-to-leaf `AGENTS.md`, fallback `CLAUDE.md`.
- Preserve `system` messages such as project instructions across reset.
- Do not scatter transition rules into `tui/`, `llm/`, or `tools/`.
- Use existing vocabulary: `State`, `Event`, `Mutation`, `Effect`, `Transition`.
- Follow the hexagonal architecture in `docs/architecture.md`; `tui` must not import `runtime`.
- Keep the TUI as the only interaction surface; do not add headless, server, or alternate UI entry points.
- LLM and tool adapters may import `runtime/protocol`, not the root `runtime` facade.
- More detail: `docs/repository-details.md`.
- Competitive gap roadmap: `docs/competitive-gap-tasks.md`.

## Documentation

- At the end of each work session, proactively update `AGENTS.md`.
- Keep `AGENTS.md` aligned with current architecture, commands, tests, and security rules.
- Update `docs/repository-details.md` when architecture or runtime flow changes.
- Update `docs/competitive-gap-tasks.md` when the roadmap against opencode, Codex CLI, or Claude Code changes.

## Commands

- `go run .`: run TUI.
- `go run . --no-tools`: run without tools.
- `go run . --yolo`: auto-approve tools.
- `go run . --approval-mode <ask|accept-edits|plan|bypass>`: set permission mode.
- TUI session commands: `/instructions`, `/permissions`, `/permissions mode <mode>`, `/sessions`, `/resume <id>`, `/rename <id> <title>`, `/delete-session <id>`, `/compact`, `/undo`.
- While a turn runs, `Enter` cancels and steers with the new prompt; `Tab` queues a follow-up. Queued prompts run in order.
- The footer previews the first three queued prompts and the remaining count.
- The footer shows a `states:` history of the current turn's state transitions (for example `WaitingLLM → AdvancingQueue → RunningTool`); consecutive repeats collapse and the history resets when a new turn starts.
- Manual run cancellation clears queued prompts; steering cancellation preserves them.
- Below 18 terminal rows, use compact footer rendering and keep viewport/input dimensions positive.
- Approval UI supports arrows/Enter plus `1/y`, `2/a`, and `3/n`; ignore duplicate input after submission.
- The composer is multiline: `Ctrl+J`, `Shift+Enter`, or `Alt+Enter` inserts a newline; `Enter` submits.
- Typing `/` opens the command palette; arrows select and `Tab` or `Enter` completes commands.
- The full command palette shows descriptions and argument hints; compact mode shows names only.
- Prompt-history navigation preserves and restores the current unsubmitted draft.
- `go test ./...`: run all tests.
- `gofmt -w <files>`: format changed Go files.
- `./scripts/build-local.sh`: install `/usr/local/bin/super-agent`.

## Tests

- All test code must live under `tests/`.
- Do not add new `_test.go` files inside production package directories.
- Use external test packages such as `runtime_test` or `tui_test`.
- Name tests by behavior, for example `TestToolCallFeedsResultBackToModel`.
- Runtime changes should cover transitions and observable engine behavior when practical.
- Transition tests should assert complete mutation/effect order.
- Reset tests should prove system messages are preserved.

## Security

- Do not commit secrets.
- Runtime switches come from `.env` and environment variables.
- `YOLO=true` in `.env` enables bypass only when no explicit `--approval-mode` flag was passed; the flag always wins over the environment.
- LLM provider config comes from `~/.superagent/settings.json`.
- Permission mode and allow/deny rules come from `~/.superagent/settings.json`.
