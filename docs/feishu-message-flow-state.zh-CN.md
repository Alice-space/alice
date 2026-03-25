# 飞书消息处理详解 Part 3：状态落盘、完整示例与读码顺序

返回总览：[feishu-message-flow.zh-CN.md](./feishu-message-flow.zh-CN.md)


这是 runtime HTTP API 的作用。

### 15.1 runtime HTTP API 提供什么

当前暴露的主要接口有：

- `/api/v1/messages/image`
- `/api/v1/messages/file`
- `/api/v1/memory/*`
- `/api/v1/automation/*`
- `/api/v1/campaigns/*`

### 15.2 为什么 tool 不需要传 receive_id

因为 `buildLLMRunEnv(job)` 早就把当前 session 信息放进环境变量了。

skill 或工具脚本只需要：

1. 读取环境变量
2. 调本地 runtime API
3. runtime API 从 header 里恢复当前 session context

然后发送时会自动路由：

- 有 `SourceMessageID` 时，优先 `ReplyText/ReplyImage/ReplyFile`
- 没有时，回退成 `SendText/SendImage/SendFile`

所以一个工具只要知道“我要发文字/图/文件”，而不需要知道“这条飞书消息应该回复到哪个 chat/thread”。

### 15.3 这对多轮会话意味着什么

意味着 LLM 在一次执行里：

- 自己说出来的流式进度
- skill 通过 runtime API 发出来的图片/文件/文字
- 最终 answer

都能保持在同一会话上下文里，不容易串台。

## 16. 第十三层：哪些状态会落盘，哪些不会

### 16.1 Session state

`Processor` 会维护并落盘 `session_state.json`，主要包含：

- `MemoryScopeKey`
- `ThreadID`
- `LastMessageAt`
- `LastIdleSummaryAnchor`

作用：

- 保持同 session 的 thread 连续性
- 支持 idle summary 扫描

### 16.2 Runtime state

`App` 会维护并落盘 `runtime_state.json`，主要包含：

- `latest`：每个 session 的最新 version
- `pending`：尚未完成的 Job

作用：

- 进程重启后可恢复未处理完的 pending job

### 16.3 Memory 文件

memory layout 会维护：

- 全局长期记忆
- scope 长期记忆
- scope daily 短期目录

这些主要用于：

- 组 prompt
- 被 runtime API / skill 显式读写

### 16.4 下载资源文件

附件下载后会落在 scoped resource root 下，供：

- prompt 引用本地路径
- skill 再次处理
- runtime API 上传回飞书

### 16.5 一个容易误解的点：当前对话并不会在这里自动写进 memory 文件

`Processor.recordInteraction(...)` 的确会调用：

- `memory.SaveInteraction(memoryScopeKey, userText, reply, failed)`

但当前 `memory.Manager.SaveInteraction` 的实现只是记一条 debug 日志，然后返回：

- `changed = false`
- `err = nil`

也就是说，在当前版本里：

- memory 确实参与了 prompt 组装
- memory 也可以被 runtime API / skill 主动修改
- 但“每轮普通对话在这里自动写回 memory”这件事，目前并没有在 `SaveInteraction` 里真正落盘

如果以后要把“对话自动沉淀到 memory”做实，这里就是一个关键扩展点。

## 17. 一个完整示例：群里先发图片，再 @bot 问问题

下面用一个具体场景把整条链路串起来。

### 17.1 场景

群配置为：

- `trigger_mode = at`
- bot id 已配置

用户 A 在群里连续做两件事：

1. 先发一张图片，不 @bot
2. 再发一条文本：`@Alice 帮我看下这张图的问题`

### 17.2 第一条图片进入系统时

系统会：

1. 识别为群消息
2. 若启用了 `group_scenes.chat`，直接归类到 `chat` 场景
3. `BuildJob` 把图片解析成当前 Job 的 attachment
4. Job 立刻入队，不再等待后续 `@bot` 触发

这一步结束时：

- 队列里已经有一条 `chat` Job
- attachment 的 `SourceMessageID` 会被保留下来

### 17.3 第二条 @bot 文本进入系统时

系统会：

1. 识别为群消息
2. 若内容满足 `#work + @bot`，路由到 `work` 场景；否则仍然落到 `chat` 场景
3. `BuildJob` 只解析“当前这条消息”的文本和附件
4. `routeIncomingJob` 根据场景规则生成 session key / memory scope key
5. 不再去合并“几分钟前缓存的群附件”

### 17.4 入队与执行

接着系统会：

1. 给这条 Job 分配 `SessionVersion`
2. 放进队列
3. worker 取出后锁住该 session
4. 进入 `processReplyMessage`

### 17.5 用户看到的第一反馈

系统先回：

- 一个 reaction
  或
- `收到！`

具体看配置。

### 17.6 LLM 实际看到的输入

真正传给 LLM 的内容会接近这样：

- 用户 A 的 id 映射
- 当前文本：`帮我看下这张图的问题`
- 当前消息自带的附件资源信息：
  - 类型：image
  - image_key / file_key
  - 下载后的本地路径

如果这条消息本身是 reply 了某条历史消息，还会把被回复消息内容一起拼进去。

### 17.7 如果 agent 中途调用了工具

比如 agent 想：

- 生成一张标注图
- 再把标注图发回飞书

那么工具只需要调用 runtime API 的 `/messages/image`：

- 它不需要自己知道 chat id
- 也不需要自己知道 source message id

runtime API 会自动把图回复到当前这条飞书消息所在的上下文。

### 17.8 最终结果

当 backend 结束后：

- 如果之前已经流式发过“完整答案”，就不重复发
- 否则补发最终 answer
- thread id 被记进 session state
- 本轮 pending job 被从 runtime state 移除

## 18. 读代码时，建议按这个顺序看

如果你想从代码继续往下读，建议顺序是：

1. `cmd/connector/main.go`
2. `cmd/connector/root.go`
3. `cmd/connector/runtime_root.go`
4. `internal/bootstrap/connector_runtime_builder.go`
5. `internal/connector/app.go`
6. `internal/connector/message.go`
7. `internal/connector/group_scenes.go`
8. `internal/connector/app_queue.go`
9. `internal/connector/processor.go`
10. `internal/connector/processor_context.go`
11. `internal/connector/reply_dispatcher.go`
12. `internal/connector/sender.go`
13. `internal/prompting/loader.go`
14. `internal/llm/codex/codex.go`
15. `internal/runtimeapi/server.go`
16. `internal/memory/memory.go`
17. `internal/connector/session_state.go`
18. `internal/connector/app_state.go`

## 19. 一句话总结

这套实现的核心思想可以概括成一句话：

**Feishu 只负责把事件送进来；`App` 负责按 session 串行与抢占；`Processor` 负责把一条消息扩展成完整执行上下文；LLM/backend 负责生成与流式进度；`replyDispatcher + LarkSender` 负责稳定回飞书；runtime HTTP API 则保证 skill/工具调用和主链路共享同一份会话语义。**
