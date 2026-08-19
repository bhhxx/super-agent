# Architecture

Super Agent follows a hexagonal architecture with a state-machine domain core.

```mermaid
flowchart TD
    app["app (composition root)"]
    tui["tui — TUI adapter"]
    adapters["llm / tools / store / workspace"]
    port["Conversation port"]
    session["runtime/session"]
    engine["runtime/engine"]
    machine["runtime/machine"]

    app --> tui
    app --> adapters
    tui --> port
    port --> session
    session --> engine
    engine --> machine
```

## Dependency Rule

- `runtime/machine` is the domain core. It owns states, events, mutations, effects, and transitions.
- `runtime/protocol` owns model and tool adapter contracts without state-machine policy.
- `runtime/permission` owns permission request and command classification value types.
- `runtime/engine` drives the machine. It owns synchronization, effect draining, and run identity.
- `runtime/execution` implements outbound model, tool, and permission ports.
- `runtime/session` exposes application use cases. It must not contain terminal behavior.
- `tui` is an inbound adapter. It depends only on its `Conversation` port and display DTOs.
- `app` is the composition root. It creates dependencies and converts runtime values to TUI values.
- Runtime states become presentation-only `tui.AgentStatus` values at the app boundary; TUI owns no runtime state enum.
- `llm`, `tools`, `store`, and `workspace` are top-level outbound adapters.

Session persistence crosses two outbound ports:

- `session.Repository`: transcript, metadata, approval, and checkpoint lookup operations.
- `session.Workspace`: file capture for checkpoints and restoration for undo.

Their implementations live in top-level `store` and `workspace`; the runtime facade exposes only ports.

Session use cases are separated by intent:

- `session.go`: construction, configuration, reset, and snapshots.
- `turn.go`: one conversational turn and approval flow.
- `history.go`: saved sessions, compaction, and undo.
- `persistence.go`: best-effort persistence notifications.

The TUI follows the same separation:

- `app.go`: Bubble Tea model construction and conversation rendering.
- `update.go`: Bubble Tea message routing and state updates.
- `commands.go`: slash commands and turn submission.
- `actions.go`: cancel and clipboard actions.
- `view.go`: top-level layout and informational views.
- `styles.go`: visual theme.

Dependencies point inward. In particular, `tui` must never import `runtime`, and the runtime must never import `tui`.

The root `runtime` package is a compatibility facade organized by `api_model.go`, `api_machine.go`, `api_execution.go`, `api_engine.go`, and `api_session.go`. Internal packages must depend on the narrow package that owns a type, not on this facade.

The TUI is the only interaction surface. Headless CLI, HTTP server, WebSocket, and alternate UI adapters are out of scope.

## Runtime Rule

```mermaid
flowchart LR
    Event --> Snapshot[Validated MachineSnapshot] --> Transition --> "Mutation + Effect" --> Reducer[Transactional Reducer] --> Commit[Atomic Engine Commit] --> EffectRunner --> OutcomeResolver --> Event
```

The state machine this rule produces:

```mermaid
stateDiagram
    [*] --> Initializing
    Initializing --> Idle: EngineReady
    Idle --> WaitingLLM: UserMessageSubmitted
    WaitingLLM --> Idle: AssistantMessageReceived
    WaitingLLM --> AdvancingQueue: ToolBatchReceived
    AdvancingQueue --> WaitingApproval: ToolCallNeedsApproval
    AdvancingQueue --> RunningTool: ToolCallReadyToRun
    WaitingApproval --> RunningTool: ApprovalGranted / ApprovalAlwaysGranted
    WaitingApproval --> AdvancingQueue: ApprovalDenied
    RunningTool --> AdvancingQueue: ToolResultReceived
    AdvancingQueue --> WaitingLLM: ToolBatchFinished
    WaitingLLM --> Idle: ErrorOccurred / CancelRequested
    AdvancingQueue --> Idle: ErrorOccurred / CancelRequested
    WaitingApproval --> Idle: ErrorOccurred / CancelRequested
    RunningTool --> Idle: ErrorOccurred / CancelRequested
    Idle --> Idle: ResetRequested
```

`ErrorOccurred`, `CancelRequested`, and `ResetRequested` return to `Idle` from any state; `ResetContext` preserves `system` messages.

The engine drops stale `RunID` outcomes before event resolution. `SnapshotFrom` validates `EngineState` and exposes only state, pending/current tool, and derived queue semantics. `Transition` owns state/event compatibility plus call and queue guards. The reducer clones state, applies mutations without scheduler callbacks, validates the result, and returns descriptive scheduler operations. The engine commits state and scheduler changes under one lock only after validation.

Errors distinguish incompatible events (`UnexpectedEventError`), current-run protocol mismatches (`ProtocolViolationError`), and impossible machine state (`InvariantViolationError`).

## Refactoring Rules

- Prefer concrete domain names over generic plumbing names.
- Keep interfaces at adapter boundaries, not between every internal function.
- Keep files focused on one responsibility.
- Convert transport and display DTOs only at the composition boundary.
- Preserve behavior with transition and application-use-case tests.

Engine files follow these responsibilities:

- `engine.go`: dependencies and construction.
- `query.go`: immutable snapshots and state queries.
- `commands.go`: public runtime commands and approval handling.
- `effect_loop.go`: transition dispatch and effect draining.

Permission decisions live in `execution/policy.go`; shell inspection lives in `execution/command_analyzer.go`.
