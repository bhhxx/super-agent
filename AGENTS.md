# Repository Guidelines

## Essentials

- Go project: agent runtime, LLM adapters, local tools, Bubble Tea TUI.
- Keep state-machine logic in `runtime/machine/transition.go`.
- Keep orchestration in `runtime/engine/engine.go`.
- Keep effect execution in `runtime/execution/`.
- Keep session/UI boundary in `runtime/session/`.
- Load project instructions from `AGENTS.md` in the current working directory.
- Preserve `system` messages such as project instructions across reset.
- Do not scatter transition rules into `tui/`, `llm/`, or `tools/`.
- Use existing vocabulary: `State`, `Event`, `Mutation`, `Effect`, `Transition`.
- More detail: `docs/repository-details.md`.

## Documentation

- At the end of each work session, proactively update `AGENTS.md`.
- Keep `AGENTS.md` aligned with current architecture, commands, tests, and security rules.
- Update `docs/repository-details.md` when architecture or runtime flow changes.

## Commands

- `go run .`: run TUI.
- `go run . --no-tools`: run without tools.
- `go run . --yolo`: auto-approve tools.
- `go test ./...`: run all tests.
- `gofmt -w <files>`: format changed Go files.

## Tests

- All test code must live under `tests/`.
- Do not add new `_test.go` files inside production package directories.
- Use external test packages such as `runtime_test` or `tui_test`.
- Name tests by behavior, for example `TestToolCallFeedsResultBackToModel`.
- Runtime changes should cover transitions and observable engine behavior when practical.

## Security

- Do not commit secrets.
- Runtime switches come from `.env` and environment variables.
- LLM provider config comes from `~/.superagent/settings.json`.
