# super-agent 的上下文机制

## 上下文是什么

本项目没有独立的向量数据库或跨会话“长期记忆”。Agent 当前能记住的内容，本质上是交给模型的 `[]Message`。它们保存在 `runtime/machine.EngineState.Messages` 中。

`Message` 定义在 `runtime/protocol/types.go`，有四种角色：

- `system`：内置提示词和项目指令。
- `user`：用户提问。
- `assistant`：模型回答，也可包含推理内容和工具调用。
- `tool`：工具执行结果，通过 `ToolCallID` 对应某次工具调用。

每次调用模型时，`runtime/execution.DefaultEffectExecutor` 会将完整的消息列表和当前工具定义交给 `Model.Next`：

```text
Messages + ToolSpecs -> Model.Next -> ModelResponse
```

因此，模型能否“记得”某件事，取决于该信息是否仍在当前 `Messages` 中。

## 初始上下文

`app.NewSession` 创建会话时，`initialMessages` 会生成第一条 `system` 消息：

```text
app.SystemPrompt
  + 用户级 ~/.superagent/AGENTS.md
  + 从项目根目录到当前目录的项目指令
```

指令由 `app/instructions.Load` 加载，规则如下：

1. 先读取可选的 `~/.superagent/AGENTS.md`。
2. 再按“根目录到当前目录”的顺序读取项目指令。
3. 同一目录中优先使用非空的 `AGENTS.md`；没有时才使用 `CLAUDE.md`。
4. 单个指令文件最大 128 KiB，超限会终止加载并返回错误。

所有指令被合并成一条 `system` 消息，而不是每轮重新扫描。会话元数据还会记录指令来源路径和 system 消息指纹，便于识别会话的初始环境。

## 一轮对话如何增长上下文

普通回答的流程是：

```text
system + 历史消息
  -> 追加 user 消息
  -> CallModel
  -> 追加 assistant 消息
```

如果模型返回工具调用，则会形成一个闭环：

```text
assistant(tool_calls)
  -> 审批并执行工具
  -> tool(result)
  -> 将更新后的全部 Messages 再次交给模型
  -> assistant 最终回答，或继续调用工具
```

工具结果不是额外的隐式内存，而是普通的 `tool` 消息。工具失败也会被转换成以 `Error:` 开头的工具结果，使模型能根据错误继续处理。审批决定会持久化，但不会作为消息发给模型；被拒绝的调用会生成 `denied: <tool-name>` 工具结果。

流式输出期间，文本暂存在 `StreamingContent` 和 `StreamingReasoning` 中，用于实时展示。只有最终响应经过状态转移后，才成为正式的历史消息。

## 运行时上下文与持久化

上下文有两个层次：

- `runtime/machine`中的 `EngineState.Messages` 是当前运行时真值，模型直接使用它。
- `~/.superagent/sessions/<session-id>/events.jsonl` 是持久化的事件日志，用于重建历史。

`runtime/session.snapshotEmitter` 观察 Engine 快照，将新增消息发给 TUI，同时通过 `Repository` 持久化。它用 `emittedMessages` 记录已发送的位置，避免重复写入。

会话目录包含：

```text
~/.superagent/sessions/<session-id>/
  meta.json      # id、标题、模型、cwd、指令来源、当前 turn id
  events.jsonl   # 消息、工具结果、审批、取消、重置、压缩、检查点等事件
```

事件日志保留的信息多于模型上下文。`store.messagesFromRecords` 重放日志时，只把消息、工具结果、重置和压缩记录转换为 `Messages`。审批、错误和取消记录用于审计，不会自动进入模型上下文。

## 恢复会话：`/resume <id>`

`Session.Resume` 通过 `Repository.Load` 读取事件日志并重建 `Messages`，然后调用 `Engine.ReplaceMessages`。替换时会：

- 取消当前 run，使迟到结果失效。
- 清理待审批工具、当前工具、工具批次和流式缓冲。
- 将状态恢复为 `Idle`。
- 使后续轮次从恢复出的历史继续。

`/resume` 恢复的是已持久化的消息历史，不会恢复一个执行到一半的工具调用。

## 压缩上下文：`/compact [summary]`

对话越长，每次请求携带的消息越多。`Session.Compact` 通过“摘要 + 最新消息”减小模型上下文。

默认流程：

1. 如果没有手动提供 summary，用当前完整历史额外调用一次模型生成摘要。
2. 保留所有 `system` 消息。
3. 将较旧的非 system 消息替换为一条 `system` 摘要：`Conversation summary:\n...`。
4. 默认保留最新 4 条非 system 消息。
5. 先保存 compact 记录，成功后才替换 Engine 中的消息，避免内存与磁盘不一致。

压缩记录同时保存 `OriginalMessages` 和 `KeptMessages`，但之后恢复会话时使用的是 `KeptMessages`。压缩是有损的：摘要没覆盖到的细节不再会发给模型。

## 重置上下文：Reset

`Session.Reset` 先写入 `reset` 事件，再调用 `Engine.Reset`。状态机的 `ResetContext` Mutation 会清除非 system 消息，但保留所有 `system` 消息。

```text
reset 前：system + user + assistant + tool + ...
reset 后：system
```

事件重放遇到 `reset` 时也使用同样的规则，因此程序重启后不会“复活”已被重置的普通对话，项目指令则仍然保留。

## 撤销：`/undo`

`/undo` 不是单纯删除最后一条消息。在 `write_file`、`apply_patch` 或 `format` 等可跟踪写操作执行前，Session 通过 Workspace 捕获相关文件快照并保存 checkpoint。

撤销时会：

1. 找到最新的非空 checkpoint。
2. 先恢复文件系统。
3. 再将 `events.jsonl` 原子截断到该 checkpoint。
4. 用 checkpoint 之前的消息替换 Engine 上下文。

先恢复文件、后截断对话，可避免文件恢复失败时丢失历史。它使工作区与模型上下文一起回到操作前。

## 取消与迟到结果

Cancel 会停止当前 run，清理工具队列并回到 `Idle`，但保留已生成的消息。Engine 使用 `RunID` 过滤取消、重置或替换上下文后才返回的旧模型/工具结果，防止它们污染新上下文。

## 机制边界

当前实现有以下边界：

- 每次模型调用会传入当前全部 `Messages`，项目本身不计算 token，也不会在超限前自动压缩。
- `/compact` 需要用户主动触发，摘要质量取决于模型或用户提供的内容。
- 持久化是按会话隔离的；新会话不会自动检索其他会话。
- 项目指令在新建会话时加载。恢复旧会话使用它当时持久化的 system 消息，不会自动替换为磁盘上最新的指令。
- checkpoint 只覆盖能解析出路径的已知写工具，不是通用文件系统快照。

## 代码导航

| 主题 | 位置 |
|---|---|
| 消息与模型接口 | `runtime/protocol/types.go` |
| 初始 system 消息 | `app/system_prompt.go`、`app/session.go` |
| 分层指令加载 | `app/instructions/instructions.go` |
| 消息变更与重置 | `runtime/machine/reducer.go` |
| 对话和工具状态转移 | `runtime/machine/transition.go` |
| 模型/工具执行 | `runtime/execution/effect_executor.go` |
| 会话轮次与消息发送 | `runtime/session/turn.go`、`runtime/session/snapshot_emitter.go` |
| 恢复、压缩与撤销 | `runtime/session/history.go` |
| 持久化适配器 | `store/repository.go`、`store/store.go` |
| 文件 checkpoint | `runtime/session/checkpoint.go`、`workspace/workspace.go` |
