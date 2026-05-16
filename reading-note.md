# Super Agent — 项目通读笔记

## 项目概览

一个用 Go 编写的 AI Agent 运行时，核心理念是基于状态机来管理 Agent 与 LLM 之间的交互流程。项目还带有一个基于 Bubble Tea 的终端 UI（TUI）。

## 核心设计模型（来自 arch.md）

```
CurrentState + Event → NextState / Mutations / Effects
```

整个运行时被建模成一个有限状态机，拥有 6 种状态：

| 状态 | 含义 |
|---|---|
| `Idle` | 空闲，等待用户输入 |
| `WaitingLLM` | 等待大模型返回响应 |
| `WaitingApproval` | 等待用户审批工具调用 |
| `RunningTool` | 正在执行工具 |
| `AdvancingQueue` | 工具队列推进中的短暂状态 |
| `Initializing` | 启动阶段 |

- **State**: 描述 Runtime 当前阶段。
- **Event**: 触发状态转移的事实。
- **Mutation**: 同步修改内部上下文的动作，不涉及 IO。
- **Effect**: 外部副作用，如调用模型或执行工具。
- **Transition**: 状态转移规则，纯逻辑决策：检查当前状态和事件，产出下一个状态、Mutations 和 Effects。
- **Engine**: 负责持有上下文、应用 Mutations、并通过 EffectExecutor 执行 Effects，将结果转回 Event。

这个模型实现了「决策」与「执行」的解耦，使得状态机核心高度可测试。

## 目录结构

| 目录 | 职责 |
|---|---|
| `runtime/` | 状态机核心：状态、事件、Mutation、Effect、Transition 规则、Engine 控制器、Session 边界层 |
| `llm/` | 模型适配层：OpenAI、DeepSeek、Claude 三个 Provider，统一对接 `runtime.Model` 接口 |
| `tools/` | 工具系统：Bash 工具、工具注册表、NoTools 模式 |
| `tui/` | 终端 UI：用户输入、审批对话、流式输出展示 |
| `app/` | 组合根：加载配置（ENV + Flags）、依赖注入、组装 Session |
| `tests/` | 按包组织的测试（`tests/runtime`、`tests/llm`、`tests/tools`、`tests/tui`、`tests/app`） |

## 按层阅读

### 第 1 层：类型基石 — `runtime/types.go`（64 行）

整个运行时的「词汇表」，5 个核心类型：

- **`State`**（`string`）— 6 个状态常量
- **`Message`** — 一条对话记录，可含 `ToolCalls` 和 `ReasoningContent`
- **`ToolCall`** — 模型发起的一次工具调用：ID + Name + 参数 JSON + `Risky` 标记
- **`ModelResponse`** — 模型返回的结构化结果：可能有最终回答，也可能有工具调用列表
- **`Model` / `ToolRunner` 接口** — 分别是 LLM 调用和工具执行的抽象。`Model.Next()` 同时支持流式（回调）和结构化响应

### 第 2 层：Event / Effect / Mutation — 三大基础抽象

**`runtime/event.go`** — 11 种事件：
- 用户输入：`UserMessageSubmitted`
- 模型响应：`AssistantMessageReceived`、`ToolCallsRequested`
- 工具结果：`ToolResultReceived`
- 审批操作：`ApprovalGranted`、`ApprovalDenied`
- 队列推进：`QueueItemReady`、`QueueExhausted`
- 控制命令：`ErrorOccurred`、`CancelRequested`、`ResetRequested`

注意 `ToolResultReceived` 和 `ApprovalDenied` 不再携带 `NextCall`。队列推进由 `Decision.PopQueue` 标记触发，`dispatchLocked` 收到后自动出队并派发 `QueueItemReady` 或 `QueueExhausted`。

**`runtime/effect.go`** — 只有 2 种副作用：
- `CallModel` — 调用 LLM
- `RunTool` — 执行一个工具

**`runtime/mutation.go`** — 9 种同步上下文修改：
- 三类 `Append*` — 往消息历史追加消息
- `SetPendingTool` / `SetPendingToolQueue` — 管理待执行工具队列
- 三个 `Clear*` — 清理待定状态
- `ResetContext` — 清空全部消息历史

队列弹出不再伪装成 Mutation，而是 `Decision.PopQueue bool` 字段，由 `dispatchLocked` 直接读取。

### 第 3 层：Transition — 状态机的「大脑」（`runtime/transition.go`，124 行）

`Transition(state, event) → (Decision, error)` 纯函数：
1. **校验**当前状态是否允许该事件
2. **决策**产出下个状态 + Mutations + Effects

转移规则：

| 当前状态 | 事件 | 下个状态 | 说明 |
|---|---|---|---|
| `Idle` | `UserMessageSubmitted` | `WaitingLLM` | 追加用户消息 → 发起 CallModel |
| `WaitingLLM` | `AssistantMessageReceived` | `Idle` | 纯文本回复结束 |
| `WaitingLLM` | `ToolCallsRequested` | `WaitingApproval` 或 `RunningTool` | 首个 risky → 等审批；否则直接执行；剩余入队列 |
| `WaitingApproval` | `ApprovalGranted` | `RunningTool` | 清除 pending，执行工具 |
| `WaitingApproval` | `ApprovalDenied` | `AdvancingQueue` | 记录拒绝结果，弹出队列 |
| `RunningTool` | `ToolResultReceived` | `AdvancingQueue` | 记录工具结果，弹出队列 |
| `AdvancingQueue` | `QueueItemReady` | `WaitingApproval` 或 `RunningTool` | 下一个工具等待审批或直接执行 |
| `AdvancingQueue` | `QueueExhausted` | `WaitingLLM` | 工具结果回传给 LLM |
| 任意 | `ErrorOccurred` / `CancelRequested` | `Idle` | 清所有 pending |
| 任意 | `ResetRequested` | `Idle` | 清 pending + 清空消息历史 |

### 第 4 层：Engine — 状态持有者 & Effect 循环驱动者（`runtime/engine.go`，283 行）

核心控制器，用 `sync.Mutex` 保护所有可变状态：

**持有状态**：当前 State、消息历史、pendingTool、pendingToolQueue、pendingEffects、alwaysAllow map、YOLOMode、poppedTool

**核心枢纽 `dispatch(event)`**：
1. 调用 `Transition(state, event)` 拿到 Decision
2. 更新状态 → 应用所有 Mutation → 追加 Effects 到 pending 队列
3. 如果 `Decision.PopQueue` 为 true，Engine 弹出下一个工具，并内部派发 `QueueItemReady` 或 `QueueExhausted`
4. 不在此执行 Effect

**Effect 消费循环 `runPendingEffects()`**：
- 逐个弹出 pendingEffects，调用 `executor.Execute()`
- Execute 返回 Event，再 dispatch 回去
- 形成「Effect → Event → Decision → Effect」循环，直到队列为空

**审批逻辑 `needsApprovalLocked()`**：
`Risky && !alwaysAllow[key] && !YOLO`

### 第 5 层：EffectExecutor — 副作用执行器（`runtime/effect_executor.go`，72 行）

实现 `Effect → Event` 转换：
- **CallModel** → 调用 `model.Next()`，根据是否有工具调用返回 `ToolCallsRequested` 或 `AssistantMessageReceived`
- **RunTool** → 调用 `tools.Run()`，返回 `ToolResultReceived`

`EffectContext` 只把 `Messages`、`ToolSpecs`、`NeedsApproval` 传给 Executor。工具队列推进由 Engine 自己处理。

### 第 6 层：Session — UI 边界（`runtime/session.go`，166 行）

Engine 和 TUI 之间的桥梁，通过双向通道通信：

```
TUI  →  approvals channel  →  Session  →  Engine
Session  →  events channel  →  TUI
```

**SessionEvent 类型**：`StateChanged`、`ToolApprovalRequested`、`StreamChunkReceived`、`MessageAppended`、`SessionError`

**`drainRun` 流程**：
1. 发送 `UserMessageSubmitted` 给 Engine
2. 遇 `WaitingApproval` → 阻塞等待 TUI 通过 approvals channel 送来的确认
3. 遇 `Idle` → 本轮结束

**增量事件推送**：`emitSnapshot()` 用 `emittedMessages` 索引追踪已推送消息，只推增量。

### 第 7 层：LLM 适配器（`llm/`）

**`llm/factory.go`** — 注册表模式。默认注册 deepseek / openai / claude 三个 Provider。

**`llm/deepseek.go`**（15 行）— 复用 `OpenAIModel`，只换配置指向 DeepSeek API。

**`llm/openai.go`**（208 行）— 核心适配实现：
1. 将 runtime 消息/工具 → OpenAI API 格式
2. 流式调用，用 Accumulator 累积 + 解析多种推理字段（`reasoning_content` / `reasoning` / `thinking`）
3. `isRiskyTool()` 从 `ToolSpec.Risky` 标记工具风险属性

**`llm/claude.go`**（218 行）— 适配 Anthropic SDK 事件流：
- `content_block_start` → 检测 tool_use 块
- `content_block_delta` → text_delta / thinking_delta / input_json_delta
- `content_block_stop` → 构造 ToolCall
- `mergeAdjacentMessages()` 满足 Claude API 同角色合并约束

### 第 8 层：工具系统（`tools/`）

**`tools/registry.go`** — 注册表，持有 `[]Tool`（按序）和 `map[string]Tool`（按名）。

**`tools/bash.go`** — 唯一实际工具，`Risky: true`，用 `bash -lc` 执行命令。区分正常退出、CTX 取消、非零退出码。

**`tools/no_tools.go`** — 空实现，`Specs()` 返回 nil，`Run()` 直接报错。

### 第 9 层：TUI — 终端界面（`tui/app.go`，649 行）

基于 Bubble Tea 的完整 TUI 实现。

**核心流程**：
- `submit()` → 创建 channels + context，并行启动 `session.Run()` 和 `waitForEvent()`
- `Update()` → 处理 sessionEventMsg（增量更新流式内容）、submitDoneMsg（清理状态）、KeyMsg（快捷键 / 审批键）
- `View()` → header + viewport + footer 三段布局

**审批 UI**：`footerView()` 在 `PendingTool != nil` 时显示黄色 `ACTION REQUIRED`。`handleApprovalKey()` 把 `y/a/n` 映射为 `ConfirmOnce/ConfirmAlways/ConfirmDeny`。

**快捷键**：`ctrl+c/esc` 退出/取消、`ctrl+y` 复制代码块、`ctrl+l` 清屏、`up/down` 历史、`?` 帮助。

### 第 10 层：`app/session.go` — 依赖注入（24 行）

整个项目的组合根：
```
llm.NewModel(provider)  →  runtime.Model
tools.NewBashTools()   →  runtime.ToolRunner
runtime.NewEngine(model, tools, nil)  →  Engine
runtime.NewSession(engine)           →  Session
```

## 依赖关系总览

```
main.go
  └─ app.LoadConfig()      ← flags + env
  └─ app.NewSession()      ← 依赖注入
       └─ llm.NewModel()   ← 根据 provider 选工厂
       └─ tools.NewBashTools()
       └─ runtime.NewEngine(model, tools, nil)
       └─ runtime.NewSession(engine)
  └─ tui.New(session)      ← Bubble Tea App
       └─ Session.Run()     ← 通过 channels 双向通信
            └─ Engine.dispatch()  ← Transition() + applyMutation() + PopQueue 自动推进
            └─ Engine.runPendingEffects()  ← EffectExecutor.Execute()
                 └─ CallModel  → model.Next()
                 └─ RunTool    → tool.Run()
```

## 运行时流程（以用户输入为例）

1. `UserMessageSubmitted` → `Idle` → `WaitingLLM`，追加用户消息，Effect: `CallModel`
2. 模型返回无工具调用 → `AssistantMessageReceived` → `Idle`
3. 模型返回多工具调用 → `ToolCallsRequested`：
   - 有风险的 → `WaitingApproval`，等待用户在 TUI 中审批
   - 安全的或 YOLO 模式 → `RunningTool`，直接执行
4. 工具结果返回 → `ToolResultReceived` → 继续下一个或回到 `WaitingLLM`

## 测试

```
tests/app/config_test.go
tests/llm/factory_test.go
tests/llm/openai_test.go
tests/runtime/engine_test.go
tests/tools/bash_test.go
tests/tui/app_test.go
tests/tui/extract_test.go
```

## TODO

- [ ] MCP 兼容
- [ ] skill 兼容
- [ ] 系统提示词配置
- [ ] AGENTS.md 读取
- [ ] 记忆机制
- [ ] 持久化的 session
- [ ] UI 优化
