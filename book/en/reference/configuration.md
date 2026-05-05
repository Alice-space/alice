# Configuration Manual

Complete reference for every configuration key in `config.yaml`. Structure follows the file layout.

---

## Top-Level Keys

### `bots` (required)

Map of bot IDs to bot configurations. Each key under `bots` is a bot ID used as the runtime identifier.

```yaml
bots:
  engineering_bot:
    # bot config...
  support_bot:
    # bot config...
```

---

### `log_level`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"info"` |
| Values | `"debug"`, `"info"`, `"warn"`, `"error"` |

Structured log level for the entire process.

### `log_file`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `""` (auto: `<ALICE_HOME>/log/YYYY-MM-DD.log`) |

Log file path with daily rotation. Empty uses the default.

### `log_max_size_mb`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `20` |

Maximum log file size in megabytes before rotation.

### `log_max_backups`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `5` |

Maximum number of rotated log files to retain.

### `log_max_age_days`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `7` |

Maximum days to keep rotated log files.

### `log_compress`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `false` |

Whether to gzip rotated log files.

---

## `bots.<id>`

Each bot is identified by its key and configured with the following fields.

### `name`

| Field | Value |
|-------|-------|
| Type | `string` |
| Required | No |

Display name used in prompts and status cards. Defaults to the bot ID.

### `feishu_app_id` (required)

| Field | Value |
|-------|-------|
| Type | `string` |
| Required | Yes |

Feishu Open Platform App ID (`cli_...`).

### `feishu_app_secret` (required)

| Field | Value |
|-------|-------|
| Type | `string` |
| Required | Yes |

Feishu Open Platform App Secret. Keep this value secure.

### `feishu_base_url`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"https://open.feishu.cn"` |

Feishu API base URL. Use `"https://open.larksuite.com"` for Lark (international edition).

---

### Runtime Directories

#### `alice_home`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"<ALICE_HOME>/bots/<bot_id>"` |

Bot-specific runtime root directory. All per-bot state lives under this path.

#### `workspace_dir`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"<alice_home>/workspace"` |

Agent workspace directory. This is the working directory for LLM subprocesses.

#### `prompt_dir`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"<alice_home>/prompts"` |

Directory for bot-specific prompt template overrides. Files here override embedded templates.

#### `codex_home`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"$CODEX_HOME"` or `"~/.codex"` |

Codex configuration and authentication directory. Shared across bots by default unless overridden here.

#### `soul_path`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"SOUL.md"` (relative to `alice_home`) |

Path to the SOUL.md persona document. Relative paths resolve against `alice_home`. If the file doesn't exist at startup, Alice writes the embedded template.

---

### Message Trigger (Legacy)

#### `trigger_mode`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"at"` |
| Values | `"at"`, `"prefix"`, `"all"` |

Legacy trigger mode. Only used when both `group_scenes.chat.enabled` and `group_scenes.work.enabled` are `false`.

| Value | Behavior |
|-------|----------|
| `"at"` | Only @bot messages accepted |
| `"prefix"` | Only messages starting with `trigger_prefix` |
| `"all"` | Every message accepted |

#### `trigger_prefix`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `""` |

Prefix string for `trigger_mode: "prefix"`.

---

### `llm_profiles`

Map of profile names to LLM profile configurations. Each profile selects a provider, model, and settings.

#### Profile Fields

##### `provider` (required)

| Field | Value |
|-------|-------|
| Type | `string` |
| Values | `"opencode"`, `"codex"`, `"claude"`, `"gemini"`, `"kimi"` |

LLM backend provider.

##### `command`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | Same as `provider` (e.g., `"opencode"`) |

Path or name of the CLI binary. Use an absolute path for binaries outside `$PATH`.

##### `timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `172800` (48 hours) |

Per-run timeout in seconds.

##### `model` (required)

| Field | Value |
|-------|-------|
| Type | `string` |

Model identifier. Examples: `"deepseek/deepseek-v4-pro"`, `"gpt-5.4-mini"`, `"claude-sonnet-4-6"`.

##### `variant` (OpenCode only)

| Field | Value |
|-------|-------|
| Type | `string` |
| Values | `"max"`, `"high"`, `"minimal"` |

Model variant for DeepSeek models via OpenCode.

##### `profile` (Codex only)

| Field | Value |
|-------|-------|
| Type | `string` |

Named sub-profile from Codex CLI configuration.

##### `reasoning_effort` (Codex only)

| Field | Value |
|-------|-------|
| Type | `string` |
| Values | `"low"`, `"medium"`, `"high"`, `"xhigh"` |

Thinking intensity level.

##### `personality` (Codex only)

| Field | Value |
|-------|-------|
| Type | `string` |

Named personality preset from Codex CLI config.

##### `prompt_prefix`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `""` |

Text prepended to every prompt before sending to the model.

##### `permissions`

| Field | Value |
|-------|-------|
| Type | `object` |

Sandbox and approval settings.

###### `permissions.sandbox`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"workspace-write"` |
| Values | `"read-only"`, `"workspace-write"`, `"danger-full-access"` |

Filesystem access level for the LLM agent.

###### `permissions.ask_for_approval`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"never"` |
| Values | `"untrusted"`, `"on-request"`, `"never"` |

When the agent should ask for approval before executing tool calls.

###### `permissions.add_dirs`

| Field | Value |
|-------|-------|
| Type | `string[]` |
| Default | `[]` |

Extra directories accessible to the agent beyond the workspace.

##### `profile_overrides`

| Field | Value |
|-------|-------|
| Type | `map[string]ProfileRunnerConfig` |
| Default | `{}` |

Advanced: per-profile runner overrides. Keys are profile names. Each override can set:

- `command` — binary path override
- `timeout` — timeout override (seconds)
- `provider_profile` — provider-specific profile name
- `exec_policy` — per-override sandbox and approval settings

---

### `env`

| Field | Value |
|-------|-------|
| Type | `map[string]string` |
| Default | `{}` |

Environment variables passed to all LLM subprocesses. Useful for `PATH`, proxy settings (`HTTPS_PROXY`, `ALL_PROXY`), and API keys.

---

### Reply Messages

#### `failure_message`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"暂时不可用，请稍后重试。"` |

Message shown when the LLM backend fails.

#### `thinking_message`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"正在思考中..."` |

Message shown in the progress card while the LLM is processing.

---

### Immediate Feedback

#### `immediate_feedback_mode`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"reaction"` |
| Values | `"reaction"`, `"reply"` |

How Alice acknowledges a received message before the LLM responds.

#### `immediate_feedback_reaction`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"OK"` |

Feishu emoji name for the reaction feedback (e.g., `"OK"`, `"WINK"`, `"THUMBSUP"`).

---

### `group_scenes`

#### `group_scenes.chat`

| Field | Value |
|-------|-------|
| Type | `object` |

Chat scene configuration for group / topic-group chats.

##### `enabled`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `true` |

##### `session_scope`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"per_chat"` |
| Values | `"per_chat"`, `"per_thread"` |

##### `llm_profile`

| Field | Value |
|-------|-------|
| Type | `string` |

Name of the LLM profile under `llm_profiles` to use.

##### `no_reply_token`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `""` |

If the model returns this exact string, Alice stays silent.

##### `create_feishu_thread`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `false` |

Whether to create a Feishu thread for replies.

#### `group_scenes.work`

| Field | Value |
|-------|-------|
| Type | `object` |

Work scene configuration for group / topic-group chats.

##### `enabled`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `true` |

##### `trigger_tag`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"#work"` |

Tag required in the message (after @bot mention) to trigger work mode.

##### `session_scope`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"per_thread"` |
| Values | `"per_thread"`, `"per_chat"` |

##### `llm_profile`

| Field | Value |
|-------|-------|
| Type | `string` |

##### `create_feishu_thread`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `true` |

##### `no_reply_token`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `""` |

---

### `private_scenes`

Same structure as `group_scenes`. Both `chat` and `work` sub-sections are **disabled by default**.

#### `private_scenes.chat`

Additional session scope: `"per_user"` — all DM messages from the same user share one session.

#### `private_scenes.work`

Additional session scope: `"per_message"` — each DM with `#work` creates a fresh session.

---

### Runtime HTTP API

#### `runtime_http_addr`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | `"127.0.0.1:7331"` |

Listen address for the runtime HTTP API. Multi-bot setups auto-increment the port (`7332`, `7333`, ...).

#### `runtime_http_token`

| Field | Value |
|-------|-------|
| Type | `string` |
| Default | auto-generated |

Bearer token for API authentication. Auto-generated if empty. Set explicitly for cross-process calls.

---

### `permissions`

#### `runtime_message`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `true` |

Allow bundled skills to send messages via the runtime API.

#### `runtime_automation`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `true` |

Allow bundled skills to manage automation tasks via the runtime API.

#### `allowed_skills`

| Field | Value |
|-------|-------|
| Type | `string[]` |
| Default | `["alice-message", "alice-scheduler"]` |

Bundled skills enabled for this bot. Built-in skills: `alice-message`, `alice-scheduler`, `alice-goal`.

---

### Worker Pool

#### `queue_capacity`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `256` |

Maximum pending jobs. Beyond this, new messages are dropped.

#### `worker_concurrency`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `3` |

Number of concurrent workers processing jobs.

---

### Timeouts

All values in seconds.

#### `automation_task_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `6000` |

Outer timeout for scheduled automation and workflow runs.

#### `auth_status_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `15` |

Timeout for provider auth status checks on startup.

#### `runtime_api_shutdown_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `5` |

Grace period when shutting down the runtime HTTP API server.

#### `local_runtime_store_open_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `10` |

Timeout for opening the local BoltDB runtime store on startup.

#### `codex_idle_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `900` |

Codex idle timeout for default/medium reasoning effort.

#### `codex_high_idle_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `1800` |

Codex idle timeout for high reasoning effort.

#### `codex_xhigh_idle_timeout_secs`

| Field | Value |
|-------|-------|
| Type | `int` |
| Default | `3600` |

Codex idle timeout for xhigh reasoning effort.

---

### Display Options

#### `show_shell_commands`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `true` |

Show recently executed shell commands in the heartbeat status card.

#### `disable_identity_hints`

| Field | Value |
|-------|-------|
| Type | `bool` |
| Default | `false` |

When `true`, messages are sent to the LLM as raw text without identity context (`Name`说：, @mention rules). When `false` (default), identity hints are included.
