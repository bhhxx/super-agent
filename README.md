# Super Agent

Go agent runtime with a state-machine core, LLM providers, local tools, and a Bubble Tea TUI.

![TUI screenshot](./static/ui.png)

## Run

- `go run .`: start the TUI. Default provider: DeepSeek.
- `go run . --no-tools`: disable tool calling.
- `go run . --yolo`: allow autonomous tool execution.
- `go run . --approval-mode <ask|accept-edits|plan|bypass>`: choose the permission mode.
- `NO_TOOLS=true go run .`: disable tools by env.
- `YOLO=true go run .`: enable bypass when no explicit approval mode is supplied.

## Test

- `go test ./...`: run all tests.
- `gofmt -w <files>`: format changed Go files.
- `./scripts/coverage.sh`: run external tests with whole-project coverage.
- `./scripts/verify.sh`: run vet, tests, race detection, and coverage.

## Build

- `./scripts/build-local.sh`: install `/usr/local/bin/super-agent`.

The binary is self-contained and can be run from any working directory. Set
`SUPER_AGENT_INSTALL_DIR` to choose another install directory. If your user
cannot write `/usr/local/bin`, run the script with `sudo`.

## Configuration

`main.go` loads `.env` with `godotenv`.

`.env` supports runtime switches:

- `NO_TOOLS=true`: disable tools.
- `YOLO=true`: auto-approve tools.

LLM provider config lives in `~/.superagent/settings.json`. On first run, the
app creates this template if the file does not exist:

```json
{
  "provider": "deepseek",
  "providers": {
    "deepseek": {
      "base_url": "https://api.deepseek.com",
      "api_key": "sk-...",
      "model": "deepseek-reasoner"
    },
    "openai": {
      "api_key": "sk-...",
      "model": "gpt-4o"
    },
    "claude": {
      "api_key": "sk-ant-...",
      "model": "claude-3-7-sonnet-20250219"
    }
  },
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
    "mode": "strict",
    "cpu_seconds": 120,
    "memory_mb": 1024,
    "max_processes": 128,
    "max_open_files": 256
  },
  "mcp_servers": {},
  "lsp_servers": {
    "go": {
      "command": "gopls",
      "extensions": ["go"],
      "language_id": "go"
    }
  },
  "agent": "build",
  "agents": {
    "reviewer": {
      "prompt": "Review changes and report defects.",
      "permission_mode": "plan",
      "tools": ["read_file", "search", "git_diff", "lsp_diagnostics"]
    }
  },
  "extensions": {
    "commands": {"explain": "Explain $ARGUMENTS"},
    "hooks": {"after_turn": ["go test ./..."]},
    "skills": [".superagent/skills/reviewer"],
    "plugins": [".superagent/plugins/team"]
  },
  "telemetry": {}
}
```

The built-in system prompt lives in `app/system_prompt.go` and is compiled into
the binary.

`build` and `plan` are built-in Agent profiles. Use `/agent` to list profiles,
`/agent <name>` to switch, or `/plan` and `/build` as shortcuts. Custom entries
may override `provider`, `model`, `prompt`, `tools`, and `permission_mode`; switching
profiles clears the current transcript.

Instructions are loaded from optional `~/.superagent/AGENTS.md`, then from
project `AGENTS.md` files from root to the working directory. `CLAUDE.md` is the
fallback when a directory has no non-empty `AGENTS.md`.

## Sessions

The TUI persists sessions under `~/.superagent/sessions/`. Use `/sessions`,
`/resume`, `/rename`, and `/delete-session` to manage them. `/compact` reduces
model context, while `/undo` restores the latest workspace checkpoint and
truncates the corresponding transcript.
Use `/fork [title]` to branch the current transcript. `/remember <text>` stores
cross-session memory, `/memory` lists it, and `/forget` clears it.
Use `/review` for a defect-focused review, `/diff` for patch preview, `/fix-ci`
for CI repair, `/branch` for repository status, and `/commit-message` for a
conventional commit subject.
Use `/export markdown`, `/export json`, or `/share` to write portable session
files under `.super-agent/exports/`.

## Tools

Permission modes route approvals but are not the security boundary. On Linux, `sandbox.mode: strict` is the default and requires `bwrap` plus `prlimit`. It exposes the host root read-only, makes only the workspace writable, uses ephemeral temporary/home directories, denies network access unless `permissions.network` is `allow`, and applies CPU, address-space, process, and open-file limits. Strict mode fails closed when unavailable. Other platforms require explicit `sandbox.mode: off`, which runs commands with the current user's authority.

Default tools:

- `read_file`: read workspace files with optional line ranges.
- `list_files`: list workspace files with optional glob filtering.
- `search`: search workspace files by regular expression.
- `apply_patch`: replace expected text in a workspace file.
- `write_file`: write workspace files and create parent directories.
- `run_command`: run workspace commands with cwd, timeout, and output limits.
- `go_test`: run `go test` for workspace packages.
- `format`: run `gofmt -w` on workspace files.
- `git_status`: show `git status --short`.
- `git_diff`: show `git diff` for optional paths.
- `bash`: run shell commands after approval.
- `delegate`: run a child Agent and return its final response. Set `worktree` to
  create an isolated detached Git worktree under `.super-agent/worktrees/`.
- `web_search`: search the public web after network approval.
- `browser_fetch`: fetch public HTTP(S) pages with redirect, size, timeout, and
  private-address protections.

MCP stdio servers are configured in `mcp_servers` by name. Each entry accepts
`command`, `args`, `env`, `cwd`, `connect_timeout_seconds`, and
`call_timeout_seconds`. Discovered schemas join the built-in registry; MCP tools
are treated as risky and use the same approval flow. Server environment
variables are explicit except for basic process variables such as `PATH` and
`HOME`. Use `/mcp list`, `/mcp add <name> <command> [args...]`,
`/mcp remove <name>`, and `/mcp restart <name>` to manage servers. Add/remove
updates `settings.json` atomically.

Language servers use stdio and are configured in `lsp_servers` with `command`,
`args`, `extensions`, and `language_id`. Configured servers expose
`lsp_diagnostics`, `lsp_symbols`, `lsp_definition`, `lsp_references`, and
`lsp_outline` tools. `/diagnostics <path>` runs diagnostics without a model turn.

Use `/attach <path>` to queue a workspace image or file for the next prompt and
`/attachments` to inspect the queue. Attachments are limited to 10 MiB. Images
are sent as native multimodal blocks; supported documents are sent as file or
document blocks.

Long command results such as `/diff` render in the scrollable conversation
viewport; footer status is bounded so small terminals retain usable input.

The `extensions` section supports prompt-backed slash commands, sandboxed
lifecycle hooks, `SKILL.md` paths, and local plugin directories. Commands,
skills, and plugins are also discovered under user/project `.superagent/`
directories. Use `/commands`, `/skills`, and `/plugins` to inspect them.

Telemetry is written as JSON Lines to `~/.superagent/telemetry.jsonl` by
default. Records correlate run/action IDs, transitions, tools, errors,
durations, and estimated model input/output tokens. Set `telemetry.log_path`
to an absolute path or a path relative to the workspace.

## Status

The Bubble Tea TUI remains the only interaction surface; headless, server, and
alternate UI entry points are out of scope.
