# 消息处理流水线

本页带你走完一条飞书消息的完整生命周期 — 从 WebSocket 接收到最终回复。理解这条流水线有助于调试路由问题和调优行为。

## 概览

```
飞书 WebSocket
  └─ App（job 队列）
      └─ Processor（执行）
          └─ LLM Backend（子进程）
              └─ Reply Dispatcher（发回飞书）
```

## 1. WebSocket 接收

Alice 与飞书的 WebSocket 端点建立长连接。当用户发送 bot 可见的消息时，飞书通过此连接投递 `im.message.receive_v1` 事件。

事件包含：
- 发送者身份（open_id、user_id、名称）
- 消息内容（文本、附件、提及）
- 对话上下文（chat_id、chat_type、如在线程中则为 thread_id）
- Bot 身份（哪个 bot 收到了这条消息）

## 2. Job 创建

原始事件被标准化为一个 `Job` 结构体。此步骤：
- 提取被提及的用户
- 解析接收 ID 类型（`chat_id`、`open_id` 等）
- 设置 bot 所配置的 LLM profile、场景和回复偏好
- 生成 session key 和资源作用域 key
- 附加单调递增的版本号

## 3. 路由

`routeIncomingJob` 决定对 job 做什么：

### 内置命令
如果消息以 `/help`、`/status`、`/clear`、`/stop`、`/session`、`/cd`、`/ls` 或 `/pwd` 开头，由连接器直接处理 — 不调用 LLM。参见[使用内置命令](../how-to/use-builtin-commands.md)。

### Work 场景
如果 `group_scenes.work.enabled` 且消息在 @bot 提及后包含 `trigger_tag`（如 `#work`），job 被路由到 work 场景。Work job 使用 work 作用域的 session key 和 LLM profile。

### Chat 场景
如果 `group_scenes.chat.enabled`，所有其他消息被路由到 chat。Chat job 使用 chat 作用域的 session key 和 LLM profile。

### 旧版回退
如果两个场景都被禁用，Alice 回退到匹配 `trigger_mode` 和 `trigger_prefix`。

## 4. 队列和序列化

每个 session 有一个互斥锁来序列化执行：

- **存在活跃运行** → 首先尝试 provider 原生注入（向正在运行的 session 注入新输入）
- **原生注入不可用** → 新 job 排队。较新的 job 取代队列中较旧的 job。
- **无活跃运行** → 接受 job 并分派给 Processor。

Runtime store（`runtime_store.go`）维护内存中的协调状态：
- 每个 session 的最新版本
- 待处理的排队 job
- 活跃运行的取消句柄
- 每个 session 的互斥锁

## 5. LLM 前处理

在调用 LLM 之前，Processor 会：

1. 加载并解析 `SOUL.md`（仅 chat）— 分离 YAML frontmatter 和 Markdown 正文
2. 将收到的附件下载到作用域内的资源目录
3. 为对话推导运行时环境变量
4. 准备渲染后的 prompt 文本

### Session 状态检查

Alice 检查 `session_state.json`：
- 如果存在 provider thread ID，后端调用将恢复该 thread
- 如果 session 最近活跃，上一轮对话的上下文可用

## 6. LLM 执行

Processor 构建一个 `RunRequest` 并将其分派给 LLM 后端：

```
RunRequest {
    ThreadID       → 来自 session 状态（空 = 新 session）
    UserText       → 渲染后的 prompt
    Provider       → 来自 llm_profile
    Model          → 来自 llm_profile
    ReasoningEffort → 来自 llm_profile
    WorkspaceDir   → 每个 bot 的工作空间
    ExecPolicy     → 沙箱 + 批准设置
    Env            → 每个 bot + 进程环境变量
    OnProgress     → 将进度更新流式传输到飞书
}
```

后端将 provider CLI 作为子进程启动并流式输出。进度更新以状态卡片补丁的形式发送到飞书。

## 7. 回复分发

当 LLM 完成时，Alice 处理回复：

### 内容处理
- 如果回复匹配 `no_reply_token`，保持静默
- 如果在 SOUL.md 中配置了 `output_contract`，剥离隐藏标签
- 应用飞书格式（富文本、@mention）

### 话题
- **Work 场景且 `create_feishu_thread: true`**：回复发布在飞书话题中
- **Chat 场景且 `create_feishu_thread: false`**：回复作为顶级消息发布
- **话题回复**：飞书支持时回复到话题。否则回退到直接回复。

### 即时反馈
在 LLM 开始之前，Alice 发送即时确认：
- `immediate_feedback_mode: "reaction"` → 给源消息添加 reaction 表情
- `immediate_feedback_mode: "reply"` → 发送显式 `收到！` 回复

## 8. 运行后

- Session 状态持久化到 `session_state.json`（thread ID、用量计数器、时间戳）
- 已下载的附件保留在作用域资源目录中
- Runtime 状态定期刷新

## 关键不变量

1. **每个 session 同时最多一个 LLM 运行** — 由每个 session 的互斥锁强制执行
2. **较新的消息取代排队的，但不取代活跃的** — 只有 `/stop` 才能中断正在运行的 LLM
3. **Session 状态以磁盘为后盾** — 进程重启后仍存在
4. **附件有作用域隔离** — 每个对话有自己的资源目录
