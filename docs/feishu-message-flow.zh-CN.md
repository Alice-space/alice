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
  -> LLM backend 运行 codex / claude / kimi
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
- `runtimeStatePath` 保存 `App` 的运行时状态，比如 `latest session version`、`pending jobs`、`group media window`。

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

所有消息先进入 `App.onMessageReceive`。这个函数一开始就会做三件事：

1. 记 debug 日志。
2. 调 `shouldProcessIncomingMessage(...)` 判断这条消息要不要正式处理。
3. 不管最终接不接，都先交给 `cacheGroupContextWindow(...)` 看是否要进“群最近消息窗口”。

### 3.1 私聊和群聊的处理差异

`shouldProcessIncomingMessage` 的规则是：

- 如果不是群聊/话题群，直接返回 `true`。
- 如果是内建命令，也直接返回 `true`。
- 否则按 trigger mode 判断。

当前内建命令有：

- `/help`
- `/codearmy status [state_key]`

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

## 4. 没被正式接收的群消息，为什么还可能有用

这一层是这套实现里一个很关键、也很容易忽略的设计：在 `at` / `prefix` 模式下，没触发 bot 的群消息并不一定直接浪费掉。

### 4.1 群上下文窗口是什么

`cacheGroupContextWindow` 会把一部分“未触发但有内容”的群消息，暂存进 `mediaWindow`。

它缓存的信息包括：

- `SourceMessageID`
- `MessageType`
- `Speaker`
- `Text`
- `Attachments`
- `RawContent`
- `ReceivedAt`

### 4.2 哪些消息会被缓存

要满足这些条件：

- 当前模式是 `at` 或 `prefix`
- 是群聊 / 话题群
- 这条消息本轮没有被正式接收
- 消息类型在支持范围内
- 这条消息确实有文本或附件内容

### 4.3 窗口不是“整个群共用”，而是“同一发送者 + 同一群 + 同一线程范围”

窗口 key 不是单纯 `chat_id`，而是由这些信息组成：

- 当前群/会话 id
- 当前发送者身份（优先 open_id，其次 user_id）
- 可选 thread/root 范围

这意味着：

- A 在群里发的几条消息，不会被错误合并到 B 后面发给 bot 的请求里。
- 同一用户在同一群里的不同 thread，也尽量隔离。

### 4.4 窗口的生命周期

窗口默认 TTL 是 5 分钟。

此外还有两个约束：

- 每个窗口最多保留 `20` 条
- 超期或空内容 entry 会在读写时被清理

### 4.5 什么时候会把窗口内容并进来

当后面某条消息真正触发 bot 时，`mergeRecentGroupContextWindow(job)` 会把同窗口内最近的文本和附件合并到当前 Job：

- 文本会被整理成“最近消息内容”提示块，附加在 `job.Text` 后面
- 附件会直接 append 到 `job.Attachments`

所以对于用户体验来说，等价于：

1. 用户先在群里发几条普通消息或图片，不触发 bot。
2. 随后再发一条 `@bot` 或带前缀消息。
3. bot 实际看到的，不只是最后一句话，而是“最后一句 + 最近窗口内的上下文消息/附件”。

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

## 9. 第六层：Processor 是整条业务链的中枢

`Processor.ProcessJobState` 进入后，会按这个顺序处理：

1. 规范化 `WorkflowPhase`
2. 解析并补充用户名字
3. 检查是否是内建命令
4. 记录 session scope 和 last message time
5. 决定走“reply 流程”还是“direct send 流程”

### 9.1 一个非常重要的当前实现事实

对于正常 Feishu 入站消息，`BuildJob` 都会把 `message.MessageId` 放进 `job.SourceMessageID`。

而 `ProcessJobState` 的判断是：

- `SourceMessageID != ""` -> 走 `processReplyMessage`
- 否则才走普通 `send` 分支

所以在当前实现里：

**绝大部分真实 Feishu 消息，都会走 reply 风格处理，而不是“裸发送到聊天”。**

`send` 分支更多是给内部 system job、恢复流程或者没有 source message id 的特殊场景预留。

### 9.2 内建命令如何处理

内建命令不会进入 LLM。

当前行为是：

- `/help`
  直接返回当前所有内建命令的 markdown 列表。
- `/codearmy status [state_key]`
  直接读取当前会话的 workflow/task 状态并回飞书。

同时，群聊下这些 builtin slash command 会先于普通 trigger 规则放行，不会被 `trigger_prefix` 改写或拦截。

## 10. 第七层：即时反馈、流式进度、最终回复

`processReplyMessage` 是最常见的用户可见路径。

### 10.1 先发即时反馈

进入 reply 流程后，第一件事不是立刻跑 LLM，而是调用 `sendImmediateFeedback`。

它有两种模式：

- `reaction`
  先给用户消息加一个 reaction emoji
- `reply`
  直接回一条 `收到！`

如果 reaction 失败，会自动回退成 `收到！`

### 10.2 运行时的流式回包

`processReplyMessage` 会把一个 `sendAgentMessage` 回调传给 backend。

这个回调用于处理流式进度：

- 普通 `agent_message`
  直接 reply 到当前 `SourceMessageID`
- `[file_change] ...`
  会优先尝试 reply 到：
  1. 当前 `SourceMessageID`
  2. `ReplyParentMessageID`
  3. `ThreadID`
  4. `RootID`

### 10.3 去重规则

如果连续两次流式消息文本完全一样，第二次不会重复发送。

### 10.4 最终回复

backend 结束后拿到 `finalReply`：

- 如果失败，用 `failure_message`
- 如果 `finalReply` 为空，或者和最后一次已发出的流式 `agent_message` 相同，则不重复补发
- 否则再发一条最终 reply

### 10.5 被新消息打断时

如果 run 是被同 session 的新消息打断：

- 当前 run 会尽快结束
- 系统会尝试回一条：

```text
已收到你的新消息，当前回复已中断并切换到最新输入。
```

所以从用户视角看，就是：

- 老回答停下
- 新消息接管会话

## 11. 第八层：进入 LLM 之前，Processor 会先准备什么

在真正调用 backend 之前，`Processor` 会做四类准备：

1. 下载附件
2. 组装“当前用户输入”
3. 按需附加被回复消息上下文
4. 构造 runtime env

### 11.1 附件下载

`prepareJobForLLM` 会遍历 `job.Attachments`，通过 `AttachmentDownloader` 下载文件。

下载逻辑由 `LarkSender.DownloadAttachment` 实现，支持：

- `image`
- `sticker`
- `audio`
- `file`

#### 下载来源

优先使用：

- attachment 自己的 `SourceMessageID`

如果没有，再回退到：

- 当前 `job.SourceMessageID`

这是为了支持一种场景：

- 当前触发消息是纯文本 `@bot`
- 真正要读的图片/文件来自前面被合并进来的最近群消息窗口

#### 下载后文件落在哪里

资源根目录来自：

- `resourceDir = <memoryDir>/resources`
- 再按 `MemoryScopeKey` 切 scoped root

最终文件会写入类似目录：

```text
<memoryDir>/resources/scopes/<scope_type>/<scope_id>/<YYYY-MM-DD>/<source_message_id>/<file>
```

例如：

```text
memory/resources/scopes/chat_id/oc_xxx/2026-03-13/om_xxx/screenshot.img
```

下载成功后，`attachment.LocalPath` 会被写回 Job，供后续 prompt 使用。

### 11.2 当前用户输入是怎么拼的

`buildCurrentUserInput(job)` 不只是简单返回 `job.Text`。

它还可能加入这些辅助上下文：

- 当前说话人的显示名和 id 映射
- 被提及用户的显示名和 id 映射
- 一条说明：
  `若需要在回复中艾特某人，请直接写 @姓名 或 @用户id，系统会自动转换`
- 附件信息：
  - 类型
  - 文件名
  - image_key / file_key
  - 本地路径
  - 下载失败原因

结果上，真正传给 LLM 的“用户输入”通常更接近一段结构化说明，而不是用户原始的一句聊天文本。

这一段对应的模板文件是：

- `prompts/connector/current_user_input.md.tmpl`

### 11.3 被回复消息上下文

如果：

- 当前 session 还没有已有 thread id
- 当前消息带 `ReplyParentMessageID`
- sender 支持 `GetMessageText`

那么 `buildUserTextWithReplyContext` 会把“被回复消息”的内容查出来，并拼成：

```text
你正在回复下面这条消息，请基于其上下文回答。
被回复消息：
...

用户当前回复：
...
```

这能显著改善“用户只说了一句‘这个再改一下’”之类 reply 场景的理解效果。

这一段也来自模板：

- `prompts/connector/reply_context.md.tmpl`

### 11.4 已有 thread 时的行为

如果当前 session 已保存过 `ThreadID`，说明这不是一段新会话，而是在续跑旧 thread。

此时 `buildPromptWithMemory` 会直接把本轮用户输入传给 backend，不再重新拼 memory prompt。

也就是说：

- 第一次进入一个 session：会走 memory prompt 组装
- 后续续同一 thread：更偏向“沿用已有上下文继续对话”

另外，如果当前消息本身是 reply 场景，还会在 memory 组装前先加一段 runtime skill hint，提示 agent 发消息/写 memory/建定时任务要走 skill，对应模板：

- `prompts/connector/runtime_skill_hint.md.tmpl`

### 11.5 memory prompt 的作用

当没有已知 thread id 时，且 `memory.Manager` 可用，`BuildPrompt(memoryScopeKey, userText)` 会把这些内容渲染进 memory 模板：

- 全局长期记忆文件
- 当前 scope 的长期记忆文件
- 当前 scope 的短期 daily 目录
- 当前用户输入

最后产出一个完整 prompt，再交给 backend。

memory 总模板在：

- `prompts/memory/prompt.md.tmpl`

## 12. 第九层：LLM 执行环境里到底注入了什么

`buildLLMRunEnv(job)` 会构造一组运行环境变量。

主要包含：

- `ReceiveIDType`
- `ReceiveID`
- `SourceMessageID`
- `ActorUserID`
- `ActorOpenID`
- `ChatType`
- `SessionKey`
- `ResourceRoot`

如果启用了 runtime API，还会额外带上：

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`

这组 env 的意义非常大：

- skill 不需要自己猜“我该把消息发到哪里”
- 工具不需要自己组装 Feishu chat id / source message id
- 当前会话上下文会自动透传给 runtime API 和 skill 脚本

## 13. 第十层：Codex / Claude / Kimi backend 是怎么跑起来的

`Processor.runLLM` 最终只依赖统一接口 `llm.Backend.Run(...)`。

因此：

- 上层 `Processor` 不需要知道底层到底是 codex、claude 还是 kimi
- 不同 provider 共享同一套 connector 编排逻辑

### 13.1 以 Codex 为例

`internal/llm/codex/codex.go` 的 `Runner.RunWithThreadAndProgress` 会：

1. 渲染最终 prompt
2. 计算 timeout
3. 启动本地 `codex` 命令
4. 把 env 合并进进程环境
5. 读取 stdout/stderr
6. 逐行解析 JSONL 事件

这里的“最终 prompt”统一来自磁盘模板：

- `prompts/llm/initial_prompt.md.tmpl`

如果调用链上没显式注入 prompt loader，各个 runner 会回退到 `internal/prompting.DefaultLoader()` 自动查找仓库里的 `prompts/` 目录，而不是退回硬编码 prompt。

### 13.2 它会解析哪些事件

对 connector 影响最大的有这些：

- `thread.started`
  提取新的 thread id
- `item.completed -> agent_message`
  作为流式进度和最终答案来源
- `item.completed -> file_change`
  作为文件变化提示来源
- `command_execution`
  用于调试和 synthetic diff 辅助判断

### 13.3 thread id 是怎么延续下去的

backend 返回 `NextThreadID` 后，`Processor` 会把它写回 session state。

下一次同 session 再来消息时，就能沿用这条 thread，形成连续对话。

### 13.4 debug trace

在 debug 日志级别下，Codex runner 还会记录：

- provider
- agent
- thread id
- model / profile
- 输入 prompt
- 观察到的 tool calls
- 最终输出或错误

## 14. 第十一层：消息是怎么发回飞书的

回复动作最终由两层完成：

1. `replyDispatcher`
2. `LarkSender`

### 14.1 replyDispatcher 的职责

`replyDispatcher` 不直接感知业务，只负责“发什么格式，如果失败怎么降级”。

它的核心策略是：

- 对 reply 场景：
  1. 先试卡片 `ReplyCard`
  2. 再试富文本 markdown `ReplyRichTextMarkdown`
  3. 最后回退到 `ReplyText`
- 对 direct send 场景：
  1. 先试卡片 `SendCard`
  2. 失败后回退 `SendText`

另外，如果内容里带 `@姓名` / `@用户id`，还会先经过 mention 归一化，把它转成飞书可识别的 mention 结构。

### 14.2 LarkSender 的 reply 策略

`LarkSender.Reply*` 不是单纯调用一次飞书 reply API。

它会优先尝试：

- `reply_in_thread = true`

如果飞书 API 返回错误，再回退到：

- `reply_in_thread = false`

因此从效果上看，它会尽量把回答挂在线程里；但如果线程回复受限，也尽量不要让整条消息丢掉。

## 15. 第十二层：agent 调工具或 skill 时，为什么还能回到同一条飞书消息

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
- `media_window`：群最近消息窗口

作用：

- 进程重启后可恢复未处理完的 pending job
- 保留群上下文窗口

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
2. 因为没 @bot，所以 `accepted = false`
3. 不正式入队
4. 但 `cacheGroupContextWindow` 会把这条图片缓存到：
   - 当前群
   - 当前发送者 A
   - 当前 thread/root 范围
5. 图片 attachment 里的 `SourceMessageID` 会被记下来

这一步结束时：

- 队列里没有 Job
- 但 `mediaWindow` 里多了一条图片上下文

### 17.3 第二条 @bot 文本进入系统时

系统会：

1. 识别为群消息
2. 因为 @ 到 bot，所以 `accepted = true`
3. `BuildJob` 生成一个 text job
4. `resolveJobSessionKey` 规范化 session key
5. `mergeRecentGroupContextWindow` 找到刚才那条图片缓存
6. 把最近图片作为附件并入当前 job
7. 在 `job.Text` 末尾追加“系统补充：已自动合并过去几分钟内的多媒体消息...”

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
- 近期自动合并的上下文说明
- 附件资源信息：
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
7. `internal/connector/media_window.go`
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
