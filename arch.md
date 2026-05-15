# Arch

目标：把 Agent Runtime 重构成职责明确的状态机。

核心模型：

CurrentState + Event -> NextState / Mutations / Effects

- State 只描述 Runtime 当前阶段
- Event 描述触发状态转移的事实
- Mutation 描述状态转移时对 Runtime 内部数据的同步修改
- Effect 描述状态转移时要执行的外部副作用
- Transition 负责根据 CurrentState 和 Event 决定 NextState，并产出 Mutations 和 Effects

## State

State 表示 Runtime 当前所处阶段，只描述“现在在哪里”。State 不负责执行业务逻辑，也不直接修改上下文。

常用状态：
- Initializing (初始化中)
- Idle (空闲，等待用户输入)
- WaitingLLM (等待模型响应)
- WaitingApproval (等待用户审批)
- RunningTool (正在执行本地工具)
- WaitingTool (等待外部异步工具结果)

RunningTool 表示 Runtime 已经开始执行本地工具，工具进程或函数还没有结束。
WaitingTool 表示工具执行已经交给外部系统，Runtime 只等待后续结果事件。
如果当前只有本地同步工具，可以先不使用 WaitingTool。

## Event

Event 表示触发状态转移的事实。Event 的来源包括：

- UserMessageSubmitted (用户提交消息)
- AssistantMessageReceived (收到模型最终文本)
- ToolCallRequested (模型请求调用工具)
- ToolResultReceived (收到工具返回)
- ApprovalGranted (用户准许执行)
- ApprovalDenied (用户拒绝执行)
- ErrorOccurred (发生错误)
- CancelRequested (用户请求取消)
- ResetRequested (请求重置)

## Mutation

Mutation 表示状态转移时对 Runtime 上下文的同步修改。Mutation 不做 IO，不调用模型，也不执行工具，比如：
- AppendUserMessage (追加用户消息)
- AppendAssistantMessage (追加助手消息)
- AppendToolResult (追加工具结果)
- SetPendingTool (设置待审批工具)
- ClearPendingTool (清除待审批工具)

## Effect

Effect 表示状态转移时要执行的外部副作用，比如：
- CallModel (调用模型)
- RunTool (执行工具)

Effect 执行完成后只能产生新的 Event，并重新进入 Transition；不能直接修改 State。

## Transition

Transition 定义状态机的核心控制规则：CurrentState + Event -> NextState / Mutations / Effects

Transition 应该负责判断：
- 当前 State 是否允许处理某个 Event
- 应该进入哪个 NextState
- 同时产生哪些 Mutations 和 Effects

Runtime Engine 持有上下文、模型、工具和状态机实例。
Engine 不应该把所有状态转移逻辑写成手动 if/else 编排，而应该通过 Transition 驱动。
Engine 的执行顺序是：调用 Transition、应用 Mutations、执行 Effects。Effect 完成后投递新的 Event，继续驱动状态机。