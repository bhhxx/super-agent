# Contributing

## Doc-First Workflow

This project treats documentation as the specification for the code. The order matters:

1. **Change the document first.** Decide the behaviour in the owning document under `docs/`.
2. **Implement it.** The code now has a written target to match.
3. **Ship both together.** A behaviour change that `docs/` does not reflect is incomplete.

If the code and a document disagree, treat the code as wrong until the document is deliberately
amended. Restating a normative fact in a second place is not documentation — it is a future
inconsistency. Link instead; see the index in [README.md](README.md).

Two tests enforce the parts that machines can check:

- `tests/architecture/spec_test.go` pins `machine.md` to the real transition graph, both directions.
- `tests/architecture/dependencies_test.go` pins the dependency rule by parsing imports.

## Tests

- All test code lives under `tests/`. Do not add `_test.go` files inside production package
  directories.
- Use external test packages such as `runtime_test`, `tui_test`, or `architecture_test`.
- Name tests by behaviour, for example `TestToolCallFeedsResultBackToModel`.
- Runtime changes should cover transitions and observable engine behaviour.
- Transition tests assert the complete order of runtime-data changes, action-queue changes, and
  scheduled actions.
- Reset tests prove `system` messages are preserved.
- Tests that parse documents or source must fail loudly when the format changes, rather than silently
  matching nothing.

Run everything with `./scripts/verify.sh`, which runs vet, the tests, race detection, and coverage.
GitHub Actions runs it on every push and pull request.

## Build

`./scripts/build-local.sh` builds the app and installs it as `/usr/local/bin/super-agent`. Set
`SUPER_AGENT_INSTALL_DIR` to override the install directory for tests or automation.

## Git and Pull Requests

- Use concise conventional commit messages, for example `fix: preserve reasoning replay`.
- Name branches by scope: `feat/session-notifications`, `fix/tool-approval`.
- A pull request should state its purpose, the main files changed, test output, and any local config
  notes.
- Add screenshots only for visible TUI changes.

## Repository Notes

`CLAUDE.md` is a symlink to `AGENTS.md`, so the two names refer to one file. Edit either; there is
nothing to keep in sync.

## Keeping Documentation Current

- Update `AGENTS.md` when project rules, architecture, commands, tests, or security guidance change.
- Update the owning document under `docs/` when behaviour changes — before the code, per the workflow
  above.
- When a fact moves, delete the old copy. Two copies of a fact is the failure mode this documentation
  set was consolidated to eliminate.

## Project Layout at a Glance

```text
main.go                 entry point
app/                    composition root and configuration
app/instructions/       layered instruction loading
runtime/machine/        pure domain core — states, transitions, invariants
runtime/engine/         orchestration, the single agent loop
runtime/execution/      model, tool, and permission ports
runtime/session/        application use cases and ports
runtime/protocol/       adapter contracts
runtime/permission/     permission vocabulary
runtime/telemetry/      JSONL telemetry
tui/                    Bubble Tea inbound adapter
llm/                    provider adapters
tools/                  file, command, git, web, MCP, and LSP tools
store/                  durable session storage
project/                project root resolution
workspace/              workspace access policy and filesystem session adapter
tests/                  external package tests by module
docs/                   this specification
```

Each package's files and responsibilities are listed in [architecture.md](architecture.md).
