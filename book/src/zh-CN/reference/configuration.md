# 配置项手册

`config.yaml` 中所有配置项的完整参考。结构按文件布局组织。

---

## 顶级键

### `bots`（必填）

Bot ID 到 bot 配置的映射。`bots` 下的每个 key 是作为运行时标识符的 bot ID。

```yaml
bots:
  engineering_bot:
    # bot 配置...
  support_bot:
    # bot 配置...
```

---

### `log_level`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"info"` |
| 可选值 | `"debug"`、`"info"`、`"warn"`、`"error"` |

整个进程的结构化日志级别。

### `log_file`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `""`（自动：`<ALICE_HOME>/log/YYYY-MM-DD.log`） |

日志文件路径，按日滚动。为空则使用默认值。

### `log_max_size_mb`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `20` |

日志文件滚动前的最大大小（MB）。

### `log_max_backups`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `5` |

保留的滚动日志文件最大数量。

### `log_max_age_days`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `7` |

保留滚动日志文件的最大天数。

### `log_compress`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `false` |

是否 gzip 压缩滚动日志文件。

---

## `bots.<id>`

每个 bot 由其 key 标识，并用以下字段配置。

### `name`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 必填 | 否 |

在 prompt 和状态卡片中使用的显示名称。默认使用 bot ID。

### `feishu_app_id`（必填）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 必填 | 是 |

飞书开放平台 App ID（`cli_...`）。

### `feishu_app_secret`（必填）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 必填 | 是 |

飞书开放平台 App Secret。请安全保管此值。

### `feishu_base_url`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"https://open.feishu.cn"` |

飞书 API base URL。Lark（国际版）用户使用 `"https://open.larksuite.com"`。

---

### 运行时目录

#### `alice_home`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"<ALICE_HOME>/bots/<bot_id>"` |

Bot 专有的运行时根目录。所有按 bot 的状态都位于此路径下。

#### `workspace_dir`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"<alice_home>/workspace"` |

Agent 工作空间目录。这是 LLM 子进程的工作目录。

#### `prompt_dir`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"<alice_home>/prompts"` |

Bot 专有 prompt 模板覆盖目录。此处的文件会覆盖内嵌模板。

#### `codex_home`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"$CODEX_HOME"` 或 `"~/.codex"` |

Codex 配置和认证目录。默认在 bot 间共享，除非在此覆盖。

#### `soul_path`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"SOUL.md"`（相对于 `alice_home`） |

SOUL.md 人格文档路径。相对路径相对于 `alice_home` 解析。如果启动时文件不存在，Alice 会写入内嵌模板。

---

### 消息触发（旧版）

#### `trigger_mode`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"at"` |
| 可选值 | `"at"`、`"prefix"`、`"all"` |

旧版触发模式。仅在 `group_scenes.chat.enabled` 和 `group_scenes.work.enabled` 均为 `false` 时使用。

| 值 | 行为 |
|-------|----------|
| `"at"` | 仅接受 @bot 消息 |
| `"prefix"` | 仅接受以 `trigger_prefix` 开头的消息 |
| `"all"` | 接受所有消息 |

#### `trigger_prefix`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `""` |

`trigger_mode: "prefix"` 时的前缀字符串。

---

### `llm_profiles`

Profile 名称到 LLM profile 配置的映射。每个 profile 选择一个 provider、模型和设置。

#### Profile 字段

##### `provider`（必填）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 可选值 | `"opencode"`、`"codex"`、`"claude"`、`"gemini"`、`"kimi"` |

LLM 后端 provider。

##### `command`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | 与 `provider` 相同（如 `"opencode"`） |

CLI 二进制路径或名称。对不在 `$PATH` 中的二进制使用绝对路径。

##### `timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `172800`（48 小时） |

每次运行的超时时间（秒）。

##### `model`（必填）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |

模型标识符。示例：`"deepseek/deepseek-v4-pro"`、`"gpt-5.4-mini"`、`"claude-sonnet-4-6"`。

##### `variant`（仅 OpenCode）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 可选值 | `"max"`、`"high"`、`"minimal"` |

OpenCode 的 DeepSeek 模型变体。

##### `profile`（仅 Codex）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |

Codex CLI 配置中的命名子 profile。

##### `reasoning_effort`（仅 Codex）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 可选值 | `"low"`、`"medium"`、`"high"`、`"xhigh"` |

推理强度级别。

##### `personality`（仅 Codex）

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |

Codex CLI 配置中的命名人格预设。

##### `prompt_prefix`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `""` |

每次 prompt 发送给模型前添加的文本。

##### `permissions`

| 字段 | 值 |
|-------|-------|
| 类型 | `object` |

沙箱和批准设置。

###### `permissions.sandbox`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"workspace-write"` |
| 可选值 | `"read-only"`、`"workspace-write"`、`"danger-full-access"` |

LLM agent 的文件系统访问级别。

###### `permissions.ask_for_approval`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"never"` |
| 可选值 | `"untrusted"`、`"on-request"`、`"never"` |

agent 执行工具调用前何时需要请求批准。

###### `permissions.add_dirs`

| 字段 | 值 |
|-------|-------|
| 类型 | `string[]` |
| 默认值 | `[]` |

agent 在工作空间之外可访问的额外目录。

##### `profile_overrides`

| 字段 | 值 |
|-------|-------|
| 类型 | `map[string]ProfileRunnerConfig` |
| 默认值 | `{}` |

高级：按 profile 的 runner 覆盖。key 为 profile 名称。每个覆盖可设置：

- `command` — 二进制路径覆盖
- `timeout` — 超时覆盖（秒）
- `provider_profile` — provider 专属的 profile 名称
- `exec_policy` — 按覆盖的沙箱和批准设置

---

### `env`

| 字段 | 值 |
|-------|-------|
| 类型 | `map[string]string` |
| 默认值 | `{}` |

传递给所有 LLM 子进程的环境变量。适用于 `PATH`、代理设置（`HTTPS_PROXY`、`ALL_PROXY`）和 API key。

---

### 回复消息

#### `failure_message`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"暂时不可用，请稍后重试。"` |

LLM 后端失败时显示的消息。

#### `thinking_message`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"正在思考中..."` |

LLM 处理期间在进度卡片中显示的消息。

---

### 即时反馈

#### `immediate_feedback_mode`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"reaction"` |
| 可选值 | `"reaction"`、`"reply"` |

Alice 在 LLM 响应之前确认收到消息的方式。

#### `immediate_feedback_reaction`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"OK"` |

Reaction 反馈的飞书表情名称（如 `"OK"`、`"WINK"`、`"THUMBSUP"`）。

---

### `group_scenes`

#### `group_scenes.chat`

| 字段 | 值 |
|-------|-------|
| 类型 | `object` |

群聊 / 话题群的 chat 场景配置。

##### `enabled`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `true` |

##### `session_scope`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"per_chat"` |
| 可选值 | `"per_chat"`、`"per_thread"` |

##### `llm_profile`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |

使用的 `llm_profiles` 下的 LLM profile 名称。

##### `no_reply_token`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `""` |

模型返回此精确字符串时，Alice 保持静默。

##### `create_feishu_thread`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `false` |

是否为回复创建飞书话题。

#### `group_scenes.work`

| 字段 | 值 |
|-------|-------|
| 类型 | `object` |

群聊 / 话题群的 work 场景配置。

##### `enabled`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `true` |

##### `trigger_tag`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"#work"` |

消息中（在 @bot 提及后）触发 work 模式所需的标签。

##### `session_scope`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"per_thread"` |
| 可选值 | `"per_thread"`、`"per_chat"` |

##### `llm_profile`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |

##### `create_feishu_thread`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `true` |

##### `no_reply_token`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `""` |

---

### `private_scenes`

与 `group_scenes` 结构相同。`chat` 和 `work` 子部分**默认均禁用**。

#### `private_scenes.chat`

额外 session 作用域：`"per_user"` — 同一用户的所有 DM 消息共用一个 session。

#### `private_scenes.work`

额外 session 作用域：`"per_message"` — 每条带 `#work` 的 DM 创建全新 session。

---

### Runtime HTTP API

#### `runtime_http_addr`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | `"127.0.0.1:7331"` |

Runtime HTTP API 的监听地址。多 bot 设置会自动递增端口（`7332`、`7333`……）。

#### `runtime_http_token`

| 字段 | 值 |
|-------|-------|
| 类型 | `string` |
| 默认值 | 自动生成 |

API 认证的 Bearer token。为空则自动生成。显式设置用于跨进程调用。

---

### `permissions`

#### `runtime_message`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `true` |

允许 bundled skill 通过 runtime API 发送消息。

#### `runtime_automation`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `true` |

允许 bundled skill 通过 runtime API 管理自动化任务。

#### `allowed_skills`

| 字段 | 值 |
|-------|-------|
| 类型 | `string[]` |
| 默认值 | `["alice-message", "alice-scheduler"]` |

此 bot 启用的 bundled skill。内置 skill：`alice-message`、`alice-scheduler`、`alice-goal`。

---

### Worker 池

#### `queue_capacity`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `256` |

最大待处理 job 数。超过后新消息被丢弃。

#### `worker_concurrency`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `3` |

处理 job 的并发 worker 数量。

---

### 超时

所有值单位为秒。

#### `automation_task_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `6000` |

定时自动化和工作流运行的外部超时。

#### `auth_status_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `15` |

启动时 provider 认证状态检查的超时。

#### `runtime_api_shutdown_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `5` |

关闭 Runtime HTTP API 服务器时的宽限期。

#### `local_runtime_store_open_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `10` |

启动时打开本地 BoltDB Runtime Store 的超时。

#### `codex_idle_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `900` |

Codex 默认/中等推理强度的空闲超时。

#### `codex_high_idle_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `1800` |

Codex 高推理强度的空闲超时。

#### `codex_xhigh_idle_timeout_secs`

| 字段 | 值 |
|-------|-------|
| 类型 | `int` |
| 默认值 | `3600` |

Codex 极高推理强度的空闲超时。

---

### 显示选项

#### `show_shell_commands`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `true` |

是否在心跳状态卡片中显示最近执行的 shell 命令。

#### `disable_identity_hints`

| 字段 | 值 |
|-------|-------|
| 类型 | `bool` |
| 默认值 | `false` |

为 `true` 时，消息以原始文本发送给 LLM，不带身份上下文（`Name`说：、@mention 规则）。为 `false`（默认）时，包含身份提示。
