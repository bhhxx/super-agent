# Extensibility Arch Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Review this Go agent runtime from future extensibility and write the result plus updated architecture diagrams into `arch.md`.

**Architecture:** This is a docs-only pass. Keep the document aligned with current code: `tui -> runtime -> llm/tools`, `Session -> Engine -> Transition`, and the current single-tool-call runtime model. Mark extension pressure explicitly instead of pretending planned abstractions already exist.

**Tech Stack:** Go, Bubble Tea, Mermaid, Markdown.

---

### Task 1: Capture Current Structure

**Files:**
- Read: `runtime/types.go`
- Read: `runtime/transition.go`
- Read: `runtime/engine.go`
- Read: `runtime/session.go`
- Read: `llm/openai.go`
- Read: `llm/claude.go`
- Read: `tools/bash.go`
- Modify: `arch.md`

- [ ] **Step 1: Verify code facts**

Run:

```bash
rg -n "type ModelResponse|ToolCall|ToolRunner|func Transition|func \\(e \\*Engine\\) runEffect|type SessionEvent|func NewModel|type BashTools" runtime llm tools
```

Expected: output shows current model response, tool runner, transition, effect executor, session event, provider factory, and bash tool code paths.

- [ ] **Step 2: Record review points**

Use these points in `arch.md`:

```text
- Provider extension is easy because runtime.Model hides OpenAI/Claude/DeepSeek adapters.
- Tool extension is limited because ToolRunner is a single runner and BashTools switches on name.
- Agent behavior extension is limited because ModelResponse carries only one ToolCall.
- Effect extension is limited because engine.runEffect switches on concrete effects.
- UI event extension is limited because SessionEvent is one struct with optional fields.
- main.go is too concrete for future config, tool registry, and provider registry growth.
```

### Task 2: Rewrite `arch.md`

**Files:**
- Modify: `arch.md`

- [ ] **Step 1: Replace `arch.md` content**

Write a document with these sections in this order:

```text
# Arch
## Design Model
## Current Architecture
## Runtime Flow
## Extensibility Review
## Target Extension Shape
## Module Notes
```

- [ ] **Step 2: Include updated Mermaid diagrams**

Include:

```mermaid
flowchart TD
```

for module boundaries, and:

```mermaid
sequenceDiagram
```

for one runtime turn.

### Task 3: Verify

**Files:**
- Read: `arch.md`

- [ ] **Step 1: Check doc mentions current code names**

Run:

```bash
rg -n "ToolRegistry|EffectExecutor|ModelResponse|ToolRunner|SessionEvent|CurrentState|sequenceDiagram|flowchart" arch.md
```

Expected: output includes current names and labels target abstractions as target shape.

- [ ] **Step 2: Run tests**

Run:

```bash
go test ./...
```

Expected: all packages pass.
