# Competitive Gap Task Document

Date: 2026-06-02

## Scope

This document compares Super Agent with opencode, OpenAI Codex CLI, and Claude Code. It records missing features, implementation tasks, architecture impact, and the current Super Agent architecture.

Sources:

- opencode docs: https://opencode.ai/docs/
- opencode agents: https://opencode.ai/docs/agents/
- opencode tools: https://opencode.ai/docs/tools/
- opencode MCP: https://opencode.ai/docs/mcp-servers/
- opencode server: https://opencode.ai/docs/server/
- OpenAI Codex CLI docs: https://developers.openai.com/codex/cli/
- OpenAI Codex CLI features: https://developers.openai.com/codex/cli/features
- OpenAI Codex permissions: https://developers.openai.com/codex/permissions
- Claude Code overview: https://docs.anthropic.com/en/docs/claude-code/overview
- Claude Code MCP: https://code.claude.com/docs/en/mcp
- Claude Code subagents: https://docs.anthropic.com/en/docs/claude-code/sub-agents
- Claude Code permission modes: https://code.claude.com/docs/en/permission-modes

## Current Capability Baseline

Implemented:

- Bubble Tea TUI with compact small-terminal layout, a growing multiline composer, slash-command palette, streaming assistant output, markdown rendering, draft-preserving command history, steering, visible follow-up queueing during active turns, `/clear`, `/help`, `/quit`, `/instructions`, `/permissions`, `/sessions`, `/resume`, `/rename`, `/delete-session`, `/compact`, `/undo`, cancel, and copy-last-code-block.
- LLM adapters for DeepSeek, OpenAI-compatible chat completion, and Claude.
- Tool calls with file read/list/search, patch/write, shell command, `bash`, Go test, gofmt, git status, and git diff.
- Tool batch execution, selectable keyboard approval menu, one-call approval, always-allow approval, deny, yolo auto-approval, and no-tools mode.
- Permission modes (`ask`, `accept-edits`, `plan`, `bypass`), config file rules for tools/commands/paths/env/network, protected paths, deny-by-default network classification, and command classification (read-only/write/network/destructive).
- State-machine runtime with `State`, `Event`, `Mutation`, `Effect`, and `Transition`.
- JSONL session persistence with serialized access, session listing/resume/rename/delete, file checkpoints with transcript-truncating undo, and context compaction that does not duplicate transcripts on replay.
- Layered project instructions: user-level spec plus root-to-leaf `AGENTS.md` with `CLAUDE.md` fallback.
- Reset preserving `system` messages.
- Tests under `tests/` for config, instructions, LLM adapters, tools, TUI helpers, transitions, engine behavior, scheduler, resolver, permission policy, command classification, and build script.

Not implemented:

- Branching, transcript fork, or persistent conversation memory.
- MCP, plugins, custom tools, custom commands, hooks, skills, or subagents.
- CLI automation, headless mode, local server/API, IDE integration, GitHub/GitLab integration, or CI agent mode.
- LSP/code intelligence, diagnostics, code actions, or symbol navigation.
- Review mode, PR review, issue workflow, web search, browser/computer use, or image input/output.

## Competitor Feature Map

### opencode

Key features to match or consider:

- Terminal-first agent with TUI, desktop/IDE integrations, and client-server design.
- Plan/build agent modes, subagents, custom agents, custom commands, custom tools, and skills.
- MCP local/remote servers with OAuth-style auth support.
- Permission rules for tool execution.
- LSP diagnostics and code intelligence.
- Session sharing and local server API.
- GitHub/GitLab agent flows.

### OpenAI Codex CLI

Key features to match or consider:

- Full-screen terminal CLI plus non-interactive automation.
- AGENTS.md guidance, session resume, model selection, image input, web search, and output schemas.
- Permission and sandbox modes with filesystem and network policy.
- MCP servers, hooks, plugins, and subagents.
- Local app server and Codex IDE/Web/GitHub integration.
- Code review and CI automation workflows.

### Claude Code

Key features to match or consider:

- Terminal, IDE, desktop, and web workflows.
- CLAUDE.md memory, slash commands, hooks, MCP, skills, and subagents.
- Permission modes including ask, accept-edits, plan, and bypass variants.
- Resume, branch, fork, checkpoint, and rewind workflows.
- Specialized agents, background agents, Git worktree workflows, and code review/security review.
- Remote control and integration surfaces.

## Task Roadmap

### P0. Configurable Permissions

Status: implemented. Modes, config rules, command classification, protected paths, deny-by-default network classification, approval metadata, `/permissions` inspection/editing, and config validation are in place.

Why:

- Current policy is binary: safe tools run, risky tools need approval, yolo approves all.
- Competitors expose mode-based policies and stronger command safety.

Tasks:

- Done: add permission modes: `ask`, `accept-edits`, `plan`, and `bypass`.
- Done: add config file rules under `~/.superagent/settings.json` for allow/deny tool names, command prefixes, paths, env vars, and network mode.
- Done: add protected paths: `.git`, `.env`, SSH keys, cloud credentials, and files outside cwd.
- Done: add command classification for read-only, write, network, and destructive commands.
- Done: add deny-by-default network mode for shell commands unless configured.
- Done: add approval UI that shows command class, cwd, touched paths, and reason.
- Done: add `/permissions` to inspect and edit current policy mode.
- Done: validate permission modes so invalid settings fail config load.

Architecture impact:

- Done: replace `ToolSpec.Risky`-only policy with structured `PermissionRequest`.
- Keep classification in `runtime/execution`, not `tui`.
- Done: command analyzer logic for shell and command tools lives in `runtime/execution`.
- Done: `ApprovalStore` stores session-only decisions; mode and allow rules are loaded from settings into policy and can be replaced for the active session.

Acceptance:

- Destructive command examples are denied or require approval in tests.
- Read-only git commands run without approval in `accept-edits`.
- `plan` mode prevents write tools and shell writes.
- Existing yolo behavior remains available as explicit `bypass`.

### P0. MCP And Extension Runtime

Why:

- MCP is table stakes for Claude Code, Codex CLI, and opencode.
- Current tools are compiled into `tools.DefaultRegistry`.

Tasks:

- Add dynamic tool registry that can merge built-in tools, MCP tools, and user tools.
- Support MCP stdio servers first.
- Add server config in `~/.superagent/settings.json`.
- Add `/mcp list`, `/mcp add`, `/mcp remove`, and `/mcp restart`.
- Map MCP tool schemas into `runtime.ToolSpec`.
- Route MCP calls through the same approval policy.
- Add timeout, cancellation, output size limit, and error normalization.
- Add tests with a fake MCP server.

Architecture impact:

- `tools.Registry` becomes an implementation behind a `ToolProvider` interface.
- Add `tools/mcp` package for client lifecycle.
- `runtime/execution` stays provider-neutral and receives merged tool specs.
- `app.NewSession` assembles tool providers from settings.

Acceptance:

- A fake MCP stdio server exposes one tool and receives a call.
- MCP tool approval uses the same UI as built-in risky tools.
- MCP server crash produces a tool result or session error without corrupting state.

### Out Of Scope: Headless CLI And Local API

Why:

- Super Agent intentionally keeps one interactive surface: the TUI.
- Runtime ports exist to decouple architecture, not to add parallel UI or server products.

Do not add `cli/`, `server/`, HTTP, WebSocket, SSE, or JSON automation surfaces unless product scope changes explicitly.

### P1. Agent Modes, Subagents, And Worktrees

Why:

- Competitors expose plan/build modes and specialized agents.
- Current runtime has one assistant loop.

Tasks:

- Add agent profiles with name, model, system prompt additions, tools, and permission mode.
- Add `/mode plan`, `/mode build`, and `/agent <name>`.
- Add subagent tasks as child sessions with parent-child links.
- Add optional Git worktree creation for isolated subagent execution.
- Add child result summarization into the parent conversation.
- Add cancellation that cascades to child sessions.

Architecture impact:

- Add `runtime/agents` for profile config and child session orchestration.
- Session store records parent session id.
- `RunController` tracks child run ids.
- Tool execution must be workspace-aware for worktrees.

Acceptance:

- Plan mode cannot mutate files.
- Build mode can request approvals normally.
- Parent session can spawn a read-only child task and receive a summarized result.
- Cancelling parent cancels active children.

### P1. Code Intelligence

Why:

- opencode advertises LSP-based diagnostics and code intelligence.
- Current code tools are file and grep oriented.

Tasks:

- Add optional LSP client manager per workspace.
- Add diagnostics tool.
- Add symbol search, definition lookup, references, and document outline tools.
- Add `/diagnostics` command.
- Include diagnostics in review prompts when available.

Architecture impact:

- Add `tools/lsp` package with language-server lifecycle.
- Tool registry gains workspace-scoped providers.
- Runtime remains unaware that tools are LSP-backed.

Acceptance:

- Fake LSP test returns diagnostics through a tool call.
- Go workspace diagnostics can be displayed without running the model.

### P1. Review, Git, And CI Workflows

Why:

- Competitors support code review, PR workflows, issue workflows, and CI agents.
- Current Git tools are read-only status/diff.

Tasks:

- Add `/review` to review current diff.
- Add `/fix-ci` workflow that runs configured commands and summarizes failures.
- Add GitHub integration for issue/PR context through MCP or a built-in provider.
- Add patch preview before writes.
- Add branch/status header details.
- Add commit helper that drafts a conventional commit message but asks before commit.

Architecture impact:

- Add workflow layer above runtime, probably `runtime/workflow` or `app/workflows`.
- Workflows submit prompts and tool policies to existing session APIs.
- GitHub should enter through MCP first unless built-in auth is required.

Acceptance:

- `/review` produces findings grounded in file paths and line numbers.
- CI workflow can run `go test ./...` and feed failures back to the model.
- Commit helper never commits without approval.

### P2. Hooks, Commands, Skills, And Plugins

Why:

- Claude Code and Codex CLI use hooks and slash commands for automation.
- opencode supports custom commands/tools/skills.

Tasks:

- Add hook events: session start, pre-tool, post-tool, approval requested, turn complete, and error.
- Add command discovery from `.superagent/commands/` and `~/.superagent/commands/`.
- Add skill discovery from `.superagent/skills/` and `~/.superagent/skills/`.
- Add plugin manifests for commands, tools, skills, and MCP servers.
- Add `/commands`, `/skills`, and `/plugins`.

Architecture impact:

- Add `app/extensions` for manifest loading.
- Hooks run outside pure transition logic.
- Dynamic commands must route through a command registry, not hardcoded `tui.submit`.

Acceptance:

- Hook test records pre-tool and post-tool events in order.
- Custom slash command can submit a prompt template.
- Broken plugin manifest returns a clear error.

### P2. Multimodal, Browser, And Web

Why:

- Competitors support image input, web search, browser/computer-like tools, or remote control surfaces.
- Current messages are text-only.

Tasks:

- Extend `runtime.Message` with content parts for text, image, and file attachment.
- Add provider-specific multimodal conversion in LLM adapters.
- Add web search tool behind permission policy.
- Add screenshot/image input in TUI and CLI.
- Add optional browser automation tool with strict permission prompts.

Architecture impact:

- `runtime/protocol` needs typed content parts.
- TUI render path needs attachment previews.
- Tool policy needs network/browser categories.

Acceptance:

- OpenAI and Claude adapters can send one image attachment in tests.
- Network tools are denied unless policy allows them.

### P2. Observability And Sharing

Why:

- Competitors expose shareable sessions, logs, and integration APIs.
- Current debugging relies on tests and TUI state.

Tasks:

- Add structured runtime logs with run id, effect id, state, event, and tool name.
- Add transcript export in markdown and JSON.
- Add `/share` as local export first, not public upload.
- Add timing metrics for model latency, tool latency, approvals, and token estimates.

Architecture impact:

- Add observer interface in `runtime/session` or `runtime/engine`.
- Persist metrics in session store.
- Keep logging separate from transition decisions.

Acceptance:

- Exported transcript includes messages, tool calls, approvals, and errors.
- Logs correlate every effect result with a run id.

## Suggested Milestones

1. Durable core: session store, resume, compaction, layered instructions.
2. Safe autonomy: permission modes, command analysis, checkpoint/undo.
3. Extension parity: MCP stdio, dynamic registry, custom commands.
4. Agent workflows: modes, subagents, worktrees, review workflows within the TUI.
5. Ecosystem polish: LSP, GitHub/CI, hooks, skills, sharing, multimodal.

## Current Architecture Summary

Startup flow:

```text
main.go
  -> app.LoadConfig
  -> app.NewSession
       -> app.LoadProjectInstructions
       -> llm.NewModel
       -> tools.DefaultRegistry / tools.NoTools
       -> runtime.NewEngine
       -> runtime.NewSession
  -> tui.New
```

Core runtime flow:

```text
UserMessageSubmitted
  -> validated MachineSnapshot
  -> context-aware Transition
  -> transactional Reducer
  -> atomic state + EffectScheduler commit
  -> EffectRunner
  -> EffectExecutor
  -> OutcomeResolver
  -> Transition
```

Package roles:

- `app/`: loads `.env`, settings, provider config, project instructions, and assembles a runtime session.
- `llm/`: provider adapters. OpenAI-compatible and DeepSeek use chat completions. Claude uses Anthropic messages.
- `runtime/protocol/`: model/tool adapter contracts for messages, calls, specs, and streams.
- `runtime/permission/`: permission request and command classification values.
- `runtime/machine/`: state-machine states and tool-batch state.
- `runtime/machine/`: pure state machine, validated snapshots, invariants, events, mutations, effects, transactional reducer, and transition table.
- `runtime/execution/`: effect execution, model/tool calls, approval classification, result resolving, scheduling, and run cancellation.
- `runtime/engine/`: orchestration around atomic state/scheduler commits, effect draining, stale `RunID` dropping, and UI snapshots.
- `runtime/session/`: UI-facing boundary, turn serialization, approval waiting, cancellation, reset, and session events.
- `tools/`: compiled-in tool registry and built-in workspace tools.
- `tui/`: Bubble Tea UI, slash commands, approvals, streaming render, and keyboard handling.
- `tests/`: external package tests.

Design invariant:

- Transition rules stay in `runtime/machine/transition.go`.
- State and call-id invariants stay in `runtime/machine`; stale `RunID` filtering stays in the engine.
- Orchestration stays in `runtime/engine/`.
- Effect execution stays in `runtime/execution/`.
- UI/session boundary stays in `runtime/session/`.
- TUI displays state and collects input; it should not own runtime rules.

Main architectural gap:

- The current design is a solid single-session state-machine agent. To match opencode, Codex CLI, and Claude Code, it needs durable session storage, policy-driven permissions, dynamic extension loading, and additional clients around the same runtime boundary.
