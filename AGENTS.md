# Repository Guidelines

## Essentials

- Go project: agent runtime, LLM adapters, local tools, Bubble Tea TUI.
- Keep state-machine logic in `runtime/machine/transition.go`.
- Keep orchestration in `runtime/engine/engine.go`.
- Keep effect execution in `runtime/execution/`.
- Keep session/UI boundary in `runtime/session/`.
- Keep durable session storage in `runtime/store/`.
- Load layered instructions with `app/instructions`: user memory, root-to-leaf `AGENTS.md`, fallback `CLAUDE.md`.
- Preserve `system` messages such as project instructions across reset.
- Do not scatter transition rules into `tui/`, `llm/`, or `tools/`.
- Use existing vocabulary: `State`, `Event`, `Mutation`, `Effect`, `Transition`.
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
- TUI session commands: `/memory`, `/permissions`, `/permissions mode <mode>`, `/sessions`, `/resume <id>`, `/rename <id> <title>`, `/delete-session <id>`, `/compact`, `/undo`.
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
- LLM provider config comes from `~/.superagent/settings.json`.
- Permission mode and allow/deny rules come from `~/.superagent/settings.json`.
- Command sandboxing can route through OpenSandbox when `sandbox.backend` is `opensandbox` and `sandbox.opensandbox_id` is set.
