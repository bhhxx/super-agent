# Repository Guidelines

## Project Structure & Module Organization

This is a Go agent runtime built around a state-machine model. `main.go` wires flags, `.env` loading, the LLM provider, tools, runtime engine, and Bubble Tea TUI.

- `runtime/`: state, events, mutations, effects, transitions, and engine orchestration.
- `llm/`: provider adapters for DeepSeek, OpenAI, and Claude.
- `tools/`: local tool runners, including bash and no-tool mode.
- `tui/`: Bubble Tea application and prompt/approval UI.
- `tests/`: package-oriented tests under `tests/runtime`, `tests/llm`, `tests/tools`, and `tests/tui`.
- `arch.md`: source note for the runtime state-machine design.

## Build, Test, and Development Commands

- `go run .`: run the TUI with the default provider, DeepSeek.
- `go run . --no-tools`: run without tool calling.
- `go run . --yolo`: allow autonomous tool execution.
- `NO_TOOLS=true go run .`: disable tools through environment config.
- `YOLO=true go run .`: enable YOLO mode through environment config.
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

Pull requests should include the purpose, main files changed, test command output, and any config needed to run locally. Link issues when relevant. Add screenshots only for visible TUI changes.

## Security & Configuration Tips

`main.go` loads `.env` with `godotenv`. Do not commit secrets. Supported provider variables include `LLM_PROVIDER`, `DEEPSEEK_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, provider base URLs, and model names. DeepSeek defaults to `DEEPSEEK_API_KEY` and falls back to `OPENAI_API_KEY`.
