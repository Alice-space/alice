# 飞书消息处理详解

本文基于仓库在 2026 年 3 月 13 日的实现，详细说明一条 Feishu `im.message.receive_v1` 消息如何进入 Alice，如何被筛选、归一化、排队、执行、回包，以及在执行过程中如何和本地 runtime HTTP API、memory、automation、附件下载、thread/session 状态协同工作。

本文不是产品视角的“功能说明”，而是代码视角的“真实执行路径说明”。核心代码主要分布在：

- `cmd/connector/main.go`
- `cmd/connector/root.go`
- `cmd/connector/runtime_root.go`
- `internal/bootstrap/connector_runtime_builder.go`
- `internal/connector/*.go`
- `internal/llm/*.go`
- `internal/prompting/*.go`
- `internal/runtimeapi/*.go`
- `internal/memory/*.go`
- `prompts/**/*.md.tmpl`

## 先看一眼总链路

```text
Feishu websocket event
  -> App.onMessageReceive
  -> 触发规则过滤 / 群上下文缓存
  -> BuildJob 解析消息
  -> 归一化 session key / 合并群最近上下文
  -> enqueueJob 分配 session version
  -> workerLoop 按 session 串行执行
  -> Processor.ProcessJobState
  -> 即时反馈 / 下载附件 / 组 prompt / 注入 env
  -> LLM backend 运行 codex / claude / gemini / kimi
  -> 流式 agent_message / file_change 回飞书
  -> 最终答案回飞书
  -> session/runtime state 周期性落盘

如果执行过程中 agent 调了 skill 或 runtime 工具：
  -> 本地 runtime HTTP API
  -> 复用当前会话上下文发送 text/image/file 或读写 memory/automation
  -> 结果继续回到当前飞书会话
```

## 1. 进程启动后，哪些组件会被装起来

`cmd/connector/main.go` 只是最外层入口，真正的 CLI 组装在 `cmd/connector/root.go`。启动 connector 主进程时大致会做这些事：

1. 读取配置文件。
2. 初始化 LLM provider。
3. 同步 bundled skills。
4. 调用 `bootstrap.BuildConnectorRuntime` 组装运行时。
5. 同时启动：
   - Feishu websocket connector
   - automation engine
   - 本地 runtime HTTP API

`BuildConnectorRuntime` 会组装出下面这些对象：

- `LarkSender`
  负责真正和飞书 API 打交道，发送文本、卡片、富文本、图片、文件，下载附件，查询被回复消息内容。
- `Processor`
  负责消息级业务处理：准备 prompt、运行 LLM、处理流式进度、发最终结果、维护 session state。
- `App`
  负责 Feishu 事件接入、Job 入队、worker 调度、session 串行、抢占中断、runtime state 恢复。
- `memory.Manager`
  负责 memory prompt 拼装、memory layout 管理、长期记忆读写、daily summary 追加。
- `automation.Store` / `automation.Engine`
  负责定时任务持久化与调度。
- `runtimeapi.Server`
  暴露本地 HTTP API，给 skills 调用。

运行时还会准备一组路径：

- `memoryDir`
- `resourceDir = <memoryDir>/resources`
- `automationStatePath`
- `sessionStatePath`
- `runtimeStatePath`

其中：

- `sessionStatePath` 保存 `Processor` 的 session 级状态，比如 `ThreadID`、`LastMessageAt`。
- `runtimeStatePath` 保存 `App` 的运行时状态，比如 `latest session version`、`pending jobs`。

## 2. Feishu 消息是怎么进来的

`internal/connector/app.go` 中的 `App.Run` 会创建 Feishu websocket client，并注册：

- `OnP2MessageReceiveV1(a.onMessageReceive)`

这意味着当前 connector 关注的是 Feishu 的 `im.message.receive_v1` 事件。

同时它还会：

- 按 `cfg.WorkerConcurrency` 启动多个 worker。
- 启动后台 automation。
- 在退出时 flush runtime state 和 session state。

注意一点：虽然 worker 可以并行跑多个 session，但同一个 session 永远是串行执行的，后面会展开说明。

## 3. 第一层：消息是否应该被处理

所有消息先进入 `App.onMessageReceive`。这个函数一开始会做三件事：

1. 记 debug 日志。
2. 调 `BuildJob(...)` 解析文本、附件和消息上下文。
3. 调 `routeIncomingJob(...)` 决定这条消息该落到哪个 session，以及是否正式入队。

### 3.1 私聊和群聊的处理差异

`shouldProcessIncomingMessage` 的规则是：

- 如果不是群聊/话题群，直接返回 `true`。
- 如果是内建命令，也直接返回 `true`。
- 否则按 trigger mode 判断。

当前内建命令有：

- `/help`
- `/status`
- `/clear`

当前 trigger mode 有三种：

- `at`
  只有群消息明确 @ 到 bot 才接。
- `active`
  默认接收群消息，但消息如果以 `trigger_prefix` 开头则忽略。
- `prefix`
  只有消息以 `trigger_prefix` 开头才接；如果消息明确 @ 到 bot，也可放行。

### 3.2 @bot 是怎么判断的

群里是否“@到了 bot”，不是只看一种格式，而是两条路都看：

- 看 Feishu 事件结构化的 `message.Mentions`
- 看原始 content 里的 `<at ...>` 标签

匹配目标使用配置里的：

- `feishu_bot_open_id`
- `feishu_bot_user_id`

如果 trigger mode 是 `at`，但这两个 bot id 都没配置，那么群消息不会被正式接收。

### 3.3 prefix 模式还会额外做一件事

如果群消息最终被接收，且当前模式是 `prefix`，那么在 `BuildJob` 后会把触发前缀从 `job.Text` 里去掉，再把剩余文本交给后续 LLM。

也就是说，用户发：

```text
!alice 帮我总结一下今天进展
```

真正喂给后续处理链路的，不是整句原文，而是：

```text
帮我总结一下今天进展
```

## 4. 群场景路由是怎么工作的

当前群聊不再使用“最近 5 分钟消息窗口”。取而代之的是一个显式的场景路由层：`routeIncomingJob(...)`。

### 4.1 `chat` 场景

如果启用了 `group_scenes.chat`：

- 群聊普通消息不需要 `@bot`
- 整个群共享一个 session key，例如 `chat_id:oc_xxx|scene:chat`
- 新消息都会继续 resume 这个 session
- 发送 `/clear` 会切换到一个新的 chat session；后续普通消息会从新的 Codex 上下文继续
- 若模型输出 `no_reply_token`（默认 `[[NO_REPLY]]`），connector 会静默不发言

### 4.2 `work` 场景

如果启用了 `group_scenes.work`：

- 只有满足 `trigger_tag + @bot` 的群根消息才会触发，例如 `#work @bot ...`
- 这条根消息会新建一个独立 work session，例如 `chat_id:oc_xxx|scene:work|seed:om_xxx`
- 随后的同一 Feishu thread 消息只有在再次命中当前 trigger（默认仍需 `@bot`）时，才会继续复用这个 work session
- work 场景默认走 reply-in-thread 链路
- 若当前只启用了 `work` 场景，未命中 `trigger_tag + @bot` 的群消息会直接忽略，不会再落回旧的 `trigger_mode`

### 4.3 legacy fallback

如果 `group_scenes.chat` 与 `group_scenes.work` 都没启用，才会回退到旧的 `trigger_mode` / `trigger_prefix` 逻辑：

- `at`
- `active`
- `prefix`

也就是说，旧的群触发策略还在，但“群消息窗口缓存”已经被移除。

### 4.6 synthetic mention job

还有一个特殊分支：如果触发消息本身因为内容为空而被 `BuildJob` 忽略，但它是“群里 @bot 的文本消息”，系统会构造一条 synthetic job：

```text
用户@了你，请结合其最近发送的消息继续处理。
```

这段 synthetic mention 文案本身现在也来自模板：

- `prompts/connector/synthetic_mention.md.tmpl`

这样即使这条触发消息本身没有正文，只是一个 @，系统也能借助前面缓存的上下文继续工作。

## 5. 第二层：把 Feishu 消息解析成内部 Job

真正的规范化由 `BuildJob(event)` 完成。

### 5.1 当前支持的消息类型

支持的 incoming 类型有：

- `text`
- `image`
- `sticker`
- `audio`
- `file`
- `post`

不在这个列表里的消息会被直接忽略。

### 5.2 不同消息类型是怎么转成文本和附件的

#### text

- 直接抽正文
- 会保留/解析 mention 信息

#### image

- 提取 `image_key` / `file_key`
- 如果正文为空，会补一段默认文本：`用户发送了一张图片。`

#### sticker

- 提取 `file_key` / `image_key`
- 没正文时补：`用户发送了一个表情包。`

#### audio

- 提取 `file_key`
- 没正文时补：`用户发送了一段语音。`

#### file

- 提取 `file_key` / `file_name`
- 没正文时补：`用户发送了一个文件。`
- 如果有文件名，会补成：`用户发送了一个文件：<file_name>`

#### post

- 走专门的富文本提取逻辑
- 最终也会尽量变成可喂给 LLM 的文本 + 附件集合

### 5.3 Job 里会放哪些关键字段

生成后的 `Job` 至少会带这些信息：

- `ReceiveID`
- `ReceiveIDType`
- `ChatType`
- `SenderOpenID`
- `SenderUserID`
- `SenderUnionID`
- `MentionedUsers`
- `SourceMessageID`
- `ReplyParentMessageID`
- `ThreadID`
- `RootID`
- `MessageType`
- `Text`
- `Attachments`
- `RawContent`
- `EventID`
- `ReceivedAt`
- `MemoryScopeKey`
- `SessionKey`

### 5.4 ReceiveID 的选择规则

优先级是：

1. `message.ChatId`
2. 如果没有，再退回说话人的 `open_id`

对应地：

- 有 `ChatId` 时 `ReceiveIDType = chat_id`
- 否则 `ReceiveIDType = open_id`

### 5.5 MemoryScopeKey 和 SessionKey 的区别

这是理解整条链路的关键。

#### MemoryScopeKey

`MemoryScopeKey` 主要表示“记忆范围”，更偏会话级、聊天级。

它通常长这样：

```text
chat_id:oc_xxx
```

也就是说，memory 以聊天范围为主，不细分到每一条 thread/message。

#### SessionKey

`SessionKey` 主要表示“执行串行和 thread 连续性”的范围，会更细。

候选形式可能是：

- `chat_id:oc_xxx|thread:omt_xxx`
- `chat_id:oc_xxx|message:om_root_xxx`
- `chat_id:oc_xxx|message:om_current_xxx`
- 如果都没有，再退回 `chat_id:oc_xxx`

简化理解：

- `MemoryScopeKey` 决定“记忆放在哪一桶”
- `SessionKey` 决定“这次 Job 跟哪条执行链串起来”

## 6. 第三层：SessionKey 归一化

消息刚 build 出来的 `SessionKey` 只是“当前事件看到的最佳候选值”，还不是最终值。

`resolveJobSessionKey(job, message)` 会把它进一步标准化，避免同一条线程被拆成多个 session。

### 6.1 为什么要做这一步

Feishu 的 thread / root / message id 组合并不总是稳定一致：

- 某条消息可能带 `ThreadId`
- 某条 reply 可能同时带 `ThreadId + RootId`
- 某条根消息可能只有自己的 `MessageId`

如果直接拿“当前事件里的字段”作为 session key，就可能出现：

- 第一条消息落到 `|message:om_root`
- 第二条回复落到 `|thread:omt_thread`
- 第三条又落回另一个 key

这样会把本该连续的一段会话拆碎。

### 6.2 归一化怎么做

它会生成一组候选 key，然后按这个顺序找：

1. `App` 当前已知的 `latest` session version
2. `App` 当前 `pending` 队列里的 session
3. `Processor` 已保存 thread id 的 session

如果这些地方都找不到匹配，就用候选列表里的第一个。

换句话说，它在做的事是：

“尽量把这条新消息挂回已有执行链，而不是随手新开一条。”

## 7. 第四层：Job 入队、分配版本号、必要时打断旧任务

`enqueueJob` 负责把 Job 放进队列，并分配一个 `SessionVersion`。

### 7.1 SessionVersion 是干什么的

同一个 `SessionKey` 下的消息会递增版本号：

- 第一条是 `1`
- 第二条是 `2`
- 第三条是 `3`

这个版本号后面用来做两件事：

1. 判断旧任务是否已经过时
2. 判断当前是否需要中断正在运行的旧任务

### 7.2 如果同一 session 的新消息来了，会发生什么

`enqueueJob` 发现：

- 当前 session 有一个 active run
- 而且 active run 的版本号更老

那么它会：

1. 记录这个新 Job 的更高版本号
2. 标记 superseded cutoff
3. 删除同 session 里更老的 pending job
4. 返回旧 run 的 `cancel` 函数

随后 `onMessageReceive` 会立刻调用这个 cancel，把旧任务打断。

### 7.3 队列满了怎么办

如果 channel queue 已满，这条消息会直接被 drop，并打印 `queue full` 日志。

## 8. 第五层：多个 worker 怎么保证“同 session 串行，不同 session 并行”

`App` 可以启动多个 worker，同时从同一个队列里拿任务。

但 worker 拿到 Job 后，不是直接跑，而是先：

1. 看 `SessionKey` 是否有效
2. 看这个 Job 是否已经被 superseded
3. 取到该 session 的专属 mutex
4. `sessionMu.Lock()`
5. 锁内再次检查是否 superseded
6. 才真正进入 `Processor`

这带来的效果是：

- 不同 session 可以并行执行
- 同一个 session 永远不会并发执行两个 LLM run

### 8.1 新消息打断旧消息是怎么传进去的

worker 在调用 `Processor.ProcessJobState` 之前，会创建：

- `runCtx, cancelRun := context.WithCancelCause(ctx)`

然后把一个包装后的 cancel 注册到 active run 里。

新消息到来时，旧 run 的 cancel 会以 `errSessionInterrupted` 作为 cause 被触发。

因此在 `Processor` 或 LLM backend 中，只要发现：

- `context.Canceled`
- 且 `context.Cause(ctx) == errSessionInterrupted`

就知道不是普通 shutdown，而是“同 session 新消息抢占”。

