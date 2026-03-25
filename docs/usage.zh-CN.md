# 使用说明

本文说明 Alice 作为一个完整系统如何使用，以及 `chat` / `work` 两种模式在飞书里的行为和推荐配置。

## 系统模型

Alice 是一个支持多 bot 的飞书长连接连接器：

- 一个 `alice` 进程可以同时托管多个 bot
- 每个 bot 都有自己的工作区、提示词目录和 `SOUL.md`，默认共享 `CODEX_HOME`
- 每条飞书消息会先进入路由层，再被分到某个 scene，最后交给对应 LLM CLI
- Alice 还会暴露本地 runtime HTTP API，给 `alice-message`、`alice-scheduler` 这类自带 skill 使用

整体流程是：

1. 飞书通过 WebSocket 推送 `im.message.receive_v1`
2. Alice 把事件解析成内部 `Job`
3. 根据 bot 配置决定走 `chat`、`work`，还是直接忽略
4. 组装上下文、调用 LLM、把进度和最终回复发回飞书

## 运行时目录

每个 bot 都会解析出自己的运行目录：

- `alice_home`：bot 运行根目录
- `workspace_dir`：工作区，包含 `SOUL.md`
- `prompt_dir`：prompt 模板目录
- `codex_home`：默认共享的 Codex 类 CLI 目录，也可以按 bot 单独覆盖
- `runtime_http_addr`：本地 runtime API 地址

例如 bot id 是 `chat_bot` 时，默认目录在：

```text
${ALICE_HOME}/bots/chat_bot/
```

如果 `bots.<id>.codex_home` 留空，Alice 会先继承进程环境里的 `CODEX_HOME`，否则默认回退到 `~/.codex`。

## Alice 怎么用

典型操作流程：

1. 安装或编译 Alice
2. 复制并编辑 `config.yaml`
3. 给每个 bot 配好飞书应用凭据
4. 选定 LLM provider，并确认对应 CLI 已登录
5. 前台运行或交给 `systemd --user` 托管
6. 在飞书里直接和 bot 对话

## Scene 路由

在群聊或话题群里，Alice 主要支持两种 scene：

- `chat`：低门槛的日常对话模式
- `work`：显式触发的任务模式

这两者配置在 `bots.<id>.group_scenes` 下。

### Chat 模式

`chat` 适合常规聊天。

推荐配置：

- `enabled: true`
- `session_scope: per_chat`
- `llm_profile: chat`
- `no_reply_token: "[[NO_REPLY]]"`
- `create_feishu_thread: false`

行为是：

- 整个群共用一个 chat scene session
- 不需要额外的 work 触发词
- 发送 `/clear` 会切到新的 chat session
- 如果模型输出 `no_reply_token`，Alice 会静默不发言

适合“让 bot 像群成员一样参与聊天”的场景。

### Work 模式

`work` 适合显式任务处理。

推荐配置：

- `enabled: true`
- `trigger_tag: "#work"`
- `session_scope: per_thread`
- `llm_profile: work`
- `create_feishu_thread: true`

行为是：

- 只有命中 work 触发条件的根消息才会开启 work thread
- 通常就是 `#work @bot ...`
- Alice 会为这个飞书 thread 创建或复用一条独立 work session
- 后续都在这个任务 thread 里继续，不污染 bot 的日常聊天上下文

适合编码、排障、执行任务、自动化操作这类强上下文工作。

### 旧触发模式回退

如果 `group_scenes.chat.enabled` 和 `group_scenes.work.enabled` 都是 `false`，Alice 会回退到旧触发逻辑：

- `trigger_mode: at`：只有艾特 bot 的消息会处理
- `trigger_mode: prefix`：只有命中 `trigger_prefix` 的消息会处理

新部署一般建议直接用 `group_scenes`，不要再依赖旧逻辑。

## 推荐配置模式

### 只有 Chat 的 bot

适合“普通群助理”：

```yaml
group_scenes:
  chat:
    enabled: true
    session_scope: "per_chat"
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
  work:
    enabled: false
```

### 同时支持 Chat + Work 的 bot

适合“平时聊天，显式进工作线程后开始认真干活”：

```yaml
llm_profiles:
  chat:
    provider: "codex"
    model: "gpt-5.4-mini"
    reasoning_effort: "low"
  work:
    provider: "claude"
    model: "claude-sonnet-4-20250514"
    reasoning_effort: "high"

group_scenes:
  chat:
    enabled: true
    session_scope: "per_chat"
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_thread"
    llm_profile: "work"
    create_feishu_thread: true
```

不同 scene 可以使用不同 provider；如果某个 profile 没写 `provider`，Alice 会回退到默认 provider。

## `SOUL.md`

每个 bot 的人格和生图元数据都可以放在 `workspace/SOUL.md`。

当前支持的机器可读 frontmatter 字段：

- `image_refs`：给角色生图使用的参考图
- `image_generation`：跟随人格走的生图策略，比如自动生图所需的最小 `reply_will` 分数
- `output_contract`：回复元数据协议，Alice 会在发送前据此解析隐藏块、静默 token 和动作字段

示例：

```md
---
image_refs:
  - refs/base.png
  - refs/closeup.jpg
image_generation:
  min_reply_will: 50
output_contract:
  hidden_tags:
    - reply_will
    - motion
  reply_will_tag: reply_will
  reply_will_field: reply_will
  motion_tag: motion
  suppress_token: "[[NO_REPLY]]"
---

# Persona
...
```

说明：

- 相对路径相对于当前 `SOUL.md` 所在目录解析
- Alice 解析后会把 frontmatter 从正文里剥掉，不会直接塞进 LLM prompt
- 如果你希望模型输出这些标签或静默 token，请直接把对应说明和示例写在 `SOUL.md` 正文里

## Runtime API 与自带 Skill

Alice 还提供本地 runtime HTTP API。自带 skill 通过它来：

- 发文本、图片、文件
- 创建和管理定时任务
- 操作 campaign 等运行时状态

所以通常是：

- 人类用户在飞书里和 Alice 对话
- skill 通过本地 runtime API 和 Alice 协作

## 常见排查

- 群里完全不回：
  先检查 `group_scenes`、`trigger_mode`，以及 `feishu_bot_open_id` / `feishu_bot_user_id`
- `work` 模式进不去：
  检查 `group_scenes.work.enabled`、`trigger_tag`，以及触发消息是否符合预期
- 模型或思考强度不对：
  检查 `llm_profiles`，以及 scene 指向的 profile 名称
- skill 发附件失败：
  检查 `runtime_http_addr`、`runtime_http_token` 和 runtime 权限

## 相关文档

- [README](../README.md)
- [English Usage Guide](./usage.md)
- [架构文档](./architecture.zh-CN.md)
- [CodeArmy 使用指南](./codearmy.zh-CN.md)
- [飞书消息流说明](./feishu-message-flow.zh-CN.md)
