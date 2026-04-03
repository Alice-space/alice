# 飞书消息处理详解 Part 2：Processor、LLM 与回复链路

返回总览：[feishu-message-flow.zh-CN.md](./feishu-message-flow.zh-CN.md)

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
- `/status`
  直接返回当前会话 scope 下的活跃自动化任务，以及非终态的 code-army campaigns。
- `/clear`
  仅在群聊 `chat` 模式下可用；会把当前群聊切到新的 chat session，相当于清空当前上下文。

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
