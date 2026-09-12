# Super Agent

Go agent runtime with a state-machine core, LLM providers, local tools, and a Bubble Tea TUI.

![TUI screenshot](./static/ui.png)

## Quick Start

```bash
go run .
```

The TUI is the only interaction surface. On first run the app writes a settings template to
`~/.superagent/settings.json`; add your provider API key there before the first prompt.

Common flags:

- `--no-tools` — disable tool calling
- `--yolo` — auto-approve tool execution
- `--approval-mode <ask|accept-edits|plan|bypass>` — choose the permission mode

To build and install the binary:

```bash
./scripts/build-local.sh
```

## Documentation

The documents under [`docs/`](docs/README.md) are the specification for this codebase, and
[`docs/README.md`](docs/README.md) indexes them: architecture, the state machine, the runtime loop,
sessions and context, the TUI, tools and sandboxing, and configuration.

## Status

The Bubble Tea TUI is the only interaction surface; headless, server, and alternate UI entry points
are out of scope.
