# Repository Guidelines

## Essentials

- Go project: agent runtime, LLM adapters, local tools, Bubble Tea TUI.
- Keep runtime state-machine logic in `runtime/transition.go`.
- Keep orchestration in `runtime/engine.go`.
- Do not scatter transition rules into `tui/`, `llm/`, or `tools/`.
- Use existing vocabulary: `State`, `Event`, `Mutation`, `Effect`, `Transition`.
- More detail: `docs/repository-details.md`.

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
- Config comes from `.env` and environment variables.
- Provider keys include `DEEPSEEK_API_KEY`, `OPENAI_API_KEY`, and `ANTHROPIC_API_KEY`.
