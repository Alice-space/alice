# Usage Guide

This guide is operator-facing. It explains how to run Alice, how `chat` and `work` behave in Feishu, and which configuration knobs matter in day-to-day use. For the package-level code map, see [Architecture](./architecture.md).

## 1. System Model

Alice is a Feishu connector with a pure multi-bot runtime model.

- One `alice` process can host multiple bots from one `config.yaml`.
- Each bot gets its own `alice_home`, workspace, prompt overrides, runtime state, and `SOUL.md` (in `alice_home`).
- Bots share `CODEX_HOME` by default unless a bot overrides `codex_home`.
- Each accepted message is routed into a scene and then into a selected LLM profile.
- Alice can also expose a local runtime API used by bundled skills and automation tasks.

High-level flow:

1. Feishu sends `im.message.receive_v1` over WebSocket.
2. Alice normalizes the event into a `Job`.
3. Scene routing decides whether that job should be ignored, treated as a built-in command, handled as `chat`, or handled as `work`.
4. Alice runs the configured backend and sends progress/replies back to Feishu.

## 2. Startup Modes

Startup mode is explicit.

- `alice --feishu-websocket`
  Real Feishu connector mode.
- `alice --runtime-only`
  Local runtime/API-only mode. Automation and bundled skills still work; Feishu WebSocket does not start.
- `alice-headless --runtime-only`
  Headless runtime-only mode for isolated debugging or temporary reruns.

`alice-headless` may not be used with `--feishu-websocket`.

## 3. Runtime Layout

For each bot, Alice resolves these important paths:

- `alice_home`
  Bot runtime root
- `workspace_dir`
  Bot workspace.
- `prompt_dir`
  Bot-specific prompt override root
- `codex_home`
  Shared or overridden CLI home for Codex-compatible tools
- `runtime_http_addr`
  Local runtime API bind address

By default, a bot named `chat_bot` lives under:

```text
${ALICE_HOME}/bots/chat_bot/
```

Important persisted files under that bot root:

- `run/connector/automation.db`
- `run/connector/campaigns.db`
- `run/connector/session_state.json`
- `run/connector/runtime_state.json`
- `run/connector/resources/scopes/...`

## 4. Config Concepts That Matter

The keys operators usually care about most:

- `bots.<id>`
  One bot runtime
- `llm_profiles`
  Named execution profiles
- `group_scenes.chat`
  Conversational group-chat scene
- `group_scenes.work`
  Explicit task/thread scene
- `permissions`
  Gates runtime message and automation operations
- `workspace_dir` / `prompt_dir` / `codex_home`
  Runtime directories

Important detail: `group_scenes.*.llm_profile` points to the outer key under `llm_profiles`. If that selected profile also contains an inner provider-specific `profile`, Alice still uses the outer key as the runtime selector and only passes the inner `profile` value to the provider CLI.

## 5. Group Scene Routing

Alice supports two primary scenes in group and topic-group chats:

- `chat`
  Low-friction conversational mode
- `work`
  Explicit task execution mode

### Chat Scene

Recommended shape:

```yaml
group_scenes:
  chat:
    enabled: true
    session_scope: "per_chat"
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
    create_feishu_thread: false
```

What it does:

- one long-lived scene session per chat
- no dedicated work trigger required
- `/clear` rotates to a new chat session
- if the model returns the configured suppress token, Alice stays silent

Use `chat` when the bot should behave like a persistent participant in the group.

### Work Scene

Recommended shape:

```yaml
group_scenes:
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_thread"
    llm_profile: "work"
    create_feishu_thread: true
```

What it does:

- a new work session starts from a root message that matches the work trigger
- by default that usually means something like `#work @bot ...`
- Alice creates or resumes a dedicated work-scoped session for that thread
- replies stay in the task thread instead of mixing with casual chat state

Use `work` when you want thread-local engineering, debugging, planning, or automation tasks.

### Built-In Commands

These commands bypass the normal LLM flow:

- `/help`
  Show the built-in command help card
- `/status`
  Show usage totals and active automation tasks in the current scope
- `/clear`
  Rotate the current group `chat` session
- `/stop`
  Stop the current in-flight run for that session

### Fallback Triggering

If both `group_scenes.chat.enabled` and `group_scenes.work.enabled` are `false`, Alice falls back to legacy trigger matching:

- `trigger_mode: at`
  Only messages that mention the bot are accepted
- `trigger_mode: prefix`
  Only messages starting with `trigger_prefix` are accepted

New deployments should prefer explicit `group_scenes`.

## 6. Recommended Patterns

### Chat-Only Bot

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

### Split Chat + Work Bot

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

Different scenes may use different providers and different CLI commands.

## 7. `SOUL.md`

Each bot can define persona and machine-readable reply metadata in its configured `soul_path`.
The default is `<alice_home>/SOUL.md`; a relative `soul_path` is resolved relative to `<alice_home>`.

Current frontmatter keys:

- `image_refs`
- `output_contract`

Example:

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

Notes:

- relative paths are resolved relative to the directory containing `SOUL.md`
- Alice parses and strips the frontmatter before appending the remaining body to the prompt
- `SOUL.md` is used for `chat`; work-scene runs intentionally skip bot-soul injection

## 8. Runtime API And Bundled Skills

Alice exposes a local runtime API. Bundled skills use that API to:

- send attachments back to Feishu
- create and manage automation tasks
- create and manage runtime campaign records

Current bundled skills in this repository:

- `alice-message`
- `alice-scheduler`

Current runtime permissions:

- `permissions.runtime_message`
- `permissions.runtime_automation`
- `permissions.runtime_campaigns`
- `permissions.allowed_skills`

Important boundary: plain text replies normally go through the main reply pipeline. The runtime message API is for image/file sends and related captions.

## 9. Typical Operator Flow

1. Install Alice from release or build from source.
2. Copy and edit `config.yaml`.
3. Fill `bots.*.feishu_app_id` and `bots.*.feishu_app_secret`.
4. Verify the target provider CLI is installed and logged in.
5. Start Alice with the correct startup mode.
6. Test the bot in Feishu with `/help`, then with normal `chat` or `work` traffic.

## 10. Troubleshooting

- Bot never responds in group chats:
  Check `group_scenes` and `trigger_mode`. Bot `open_id` is fetched automatically at startup now, so there is no manual `feishu_bot_open_id` / `feishu_bot_user_id` knob anymore.
- `work` mode never starts:
  Check that `group_scenes.work.enabled` is true, `trigger_tag` is set, and the triggering message actually matches the expected pattern.
- Wrong model or reasoning level:
  Check `llm_profiles` and confirm the scene points at the expected outer profile key.
- Bundled skills cannot send attachments or manage tasks:
  Check `runtime_http_addr`, `runtime_http_token`, and the `permissions.*` runtime gates.
- Config changes do not apply:
  Single-bot mode supports limited hot reload; multi-bot mode requires a restart.

## Related Docs

- [README](../README.md)
- [Chinese Usage Guide](./usage.zh-CN.md)
- [Architecture](./architecture.md)
- [Documentation Index](./README.md)
