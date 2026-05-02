# 使用说明

这份文档面向实际操作者，说明 Alice 怎么运行、`chat` / `work` 在飞书里怎么工作，以及哪些配置最值得关注。包级代码结构请看 [架构文档](./architecture.zh-CN.md)。

## 1. 系统模型

Alice 是一个纯多 bot runtime。

- 一个 `alice` 进程可以从同一个 `config.yaml` 托管多个 bot
- 每个 bot 都有自己的 `alice_home`、workspace、prompt 覆盖目录、runtime state 和 `SOUL.md`（位于 `alice_home` 下）
- 默认共享 `CODEX_HOME`，除非 bot 显式覆盖 `codex_home`
- 每条被接受的消息都会先路由到 scene，再路由到选中的 LLM profile
- Alice 还会暴露本地 runtime API，给 bundled skills 和 automation task 使用

粗略流程：

1. 飞书通过 WebSocket 推送 `im.message.receive_v1`
2. Alice 把事件归一化成 `Job`
3. scene 路由决定这条消息应被忽略、当成内建命令、走 `chat`，还是走 `work`
4. Alice 调用选中的 backend，把进度和最终回复发回飞书

## 2. 启动模式

启动模式现在必须显式指定。

- `alice --feishu-websocket`
  真实飞书连接模式
- `alice --runtime-only`
  本地 runtime/API-only 模式。automation 和 bundled skills 仍然可用，但不会启动飞书 WebSocket
- `alice-headless --runtime-only`
  用于隔离调试或临时 rerun 的 headless runtime-only 模式

`alice-headless` 不能搭配 `--feishu-websocket`。

## 3. 运行目录

对每个 bot，Alice 重点会解析这些路径：

- `alice_home`
  bot runtime 根目录
- `workspace_dir`
  bot 工作区
- `prompt_dir`
  bot 级 prompt 覆盖目录
- `codex_home`
  Codex 类工具共享或覆盖的 CLI home
- `runtime_http_addr`
  本地 runtime API 监听地址

默认情况下，一个名为 `chat_bot` 的 bot 位于：

```text
${ALICE_HOME}/bots/chat_bot/
```

这个 bot 根目录下最关键的持久化文件：

- `run/connector/automation.db`
- `run/connector/campaigns.db`
- `run/connector/session_state.json`
- `run/connector/runtime_state.json`
- `run/connector/resources/scopes/...`

## 4. 最重要的配置概念

日常最常碰到的 key：

- `bots.<id>`
  一个 bot runtime
- `llm_profiles`
  命名执行档位
- `group_scenes.chat`
  群聊闲聊场景
- `group_scenes.work`
  显式任务场景
- `permissions`
  控制 runtime message / automation 能力
- `workspace_dir` / `prompt_dir` / `codex_home`
  运行目录

一个容易混淆的点：

- `group_scenes.*.llm_profile` 指向的是 `llm_profiles` 的外层 key
- 如果那个 profile 里还写了 provider-specific 的内层 `profile`，Alice 仍然把外层 key 当运行时选择器，只把内层值传给 provider CLI

## 5. 群聊 Scene 路由

Alice 在群聊和话题群里支持两种主要 scene：

- `chat`
  低门槛闲聊模式
- `work`
  显式任务执行模式

### Chat Scene

推荐形态：

```yaml
group_scenes:
  chat:
    enabled: true
    session_scope: "per_chat"
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
    create_feishu_thread: false
```

行为：

- 整个群共享一个 chat scene session
- 不需要显式 work trigger 也能响应
- `/clear` 会轮换到新的 chat session
- 如果模型返回 suppress token，Alice 会保持静默

如果你希望 bot 像群里的常驻成员那样参与对话，用 `chat`。

### Work Scene

推荐形态：

```yaml
group_scenes:
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_thread"
    llm_profile: "work"
    create_feishu_thread: true
```

行为：

- 一条匹配 work trigger 的根消息会启动新的 work session
- 默认通常是 `#work @bot ...`
- Alice 会为那个 thread 创建或恢复专用 work session
- 回复保持在任务 thread 内，不会和闲聊状态混在一起
- 空 work trigger，例如 `@Alice #work`，只创建飞书 work thread，不调用 LLM
- `@Alice #work /session <backend-session-id>` 会创建 work thread，并绑定到已有后端 session

当你需要 thread-local 的排障、编码、计划或自动化任务时，用 `work`。

### 内建命令

这些命令会绕过正常的 LLM 主链路：

- `/help`
  查看内建命令帮助卡片
- `/status`
  查看当前 scope 下的 usage、活跃 automation task，以及当前后端/session 信息
- `/clear`
  轮换当前群聊 `chat` session
- `/stop`
  停止当前 session 正在运行的回复
- `/session <backend-session-id> [instruction]`
  在 work thread 中把当前飞书 thread 绑定到已有后端 session；如果带 `instruction`，Alice 会立即 resume 该后端 session。
- `/cd <path>`、`/ls [path]`、`/pwd`
  查看或切换当前 work session 的工作目录。

### 回退触发模式

如果 `group_scenes.chat.enabled` 和 `group_scenes.work.enabled` 都是 `false`，Alice 会回退到旧触发逻辑：

- `trigger_mode: at`
  只有提到 bot 的消息才接受
- `trigger_mode: prefix`
  只有以 `trigger_prefix` 开头的消息才接受
- `trigger_mode: all`
  接受群里的所有消息（无需 @bot 或 prefix）

新部署应优先使用显式 `group_scenes`。

## 6. 推荐配置模式

### 纯 Chat Bot

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

### Chat + Work 混合 Bot

```yaml
llm_profiles:
  chat:
    provider: "codex"
    model: "gpt-5.4-mini"
    reasoning_effort: "low"
  work:
    provider: "claude"
    model: "claude-sonnet-4-6"

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

不同 scene 可以选不同 provider，也可以用不同 CLI 命令。

## 7. `SOUL.md`

每个 bot 的人格和机器可读回复元数据都可以写在自己配置的 `soul_path`。
默认路径为 `<alice_home>/SOUL.md`；相对 `soul_path` 会相对于 `<alice_home>` 解析。

当前 frontmatter 键：

- `image_refs`
- `output_contract`

示例：

```md
---
image_refs:
  - refs/base.png
  - refs/closeup.jpg
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
- Alice 会先解析并剥离 frontmatter，再把正文拼到 prompt 里
- `SOUL.md` 只用于 `chat`；`work` 场景故意不注入 bot soul

## 8. Runtime API 与 Bundled Skills

Alice 会暴露本地 runtime API。bundled skill 通过它来：

- 把附件发回飞书
- 创建和管理 automation task
- 创建和管理 runtime campaign 记录

当前仓库里实际自带的 skill：

- `alice-message`
- `alice-scheduler`

当前 runtime 权限控制：

- `permissions.runtime_message`
- `permissions.runtime_automation`
- `permissions.runtime_campaigns`
- `permissions.allowed_skills`

一个重要边界：

- 纯文本回复正常应走主回复链路
- runtime message API 主要用于图片 / 文件发送以及相关 caption

## 9. 典型操作流程

1. 通过 release 安装，或从源码构建 Alice
2. 复制并编辑 `config.yaml`
3. 填好 `bots.*.feishu_app_id` 和 `bots.*.feishu_app_secret`
4. 确认目标 provider CLI 已安装并登录
5. 用正确的启动模式启动 Alice
6. 在飞书里先试 `/help`，再测试正常的 `chat` 或 `work` 流量

## 10. 排障

- 群里完全不回复：
  先检查 `group_scenes` 和 `trigger_mode` 是否配置正确。机器人的 open_id 现在由系统自动 fetch，无需手动填写。
- `work` 模式起不来：
  检查 `group_scenes.work.enabled` 是否为 true，`trigger_tag` 是否设置，触发消息是否真的匹配。
- 模型或推理强度不对：
  检查 `llm_profiles`，确认 scene 指向的是你预期的外层 profile key。
- bundled skill 发附件或建任务失败：
  检查 `runtime_http_addr`、`runtime_http_token` 和 `permissions.*` runtime gate。
- 改了配置却没生效：
  单 bot 模式支持有限热更新；多 bot 模式需要重启。

## 相关文档

- [README](../README.zh-CN.md)
- [英文 Usage Guide](./usage.md)
- [架构文档](./architecture.zh-CN.md)
- [文档索引](./README.md)
