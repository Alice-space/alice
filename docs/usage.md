# Usage Guide

This document explains how Alice is operated as a system, how `chat` and `work` scenes behave, and how to configure bots for common Feishu workflows.

## System Model

Alice is a Feishu long-connection connector with a multi-bot runtime model:

- One `alice` process can host multiple bots from a single `config.yaml`.
- Each bot owns its own workspace, prompts, `SOUL.md`, and isolated `CODEX_HOME`.
- Each incoming message is routed into a scene, then into an LLM backend (`codex`, `claude`, `gemini`, or `kimi`).
- Alice can also expose a local runtime HTTP API used by bundled skills such as `alice-message` and `alice-scheduler`.

At a high level:

1. Feishu sends `im.message.receive_v1` over WebSocket.
2. Alice builds a `Job` from the event.
3. The bot's routing rules decide whether the message belongs to `chat`, `work`, or should be ignored.
4. Alice assembles prompt context, runs the selected backend, and sends progress/replies back to Feishu.

## Runtime Layout

For each bot, Alice resolves these paths:

- `alice_home`: bot runtime root
- `workspace_dir`: workspace files, including `SOUL.md`
- `prompt_dir`: prompt templates
- `codex_home`: isolated CLI home for Codex-compatible tools
- `runtime_http_addr`: local runtime API for skills and automation

By default, a bot named `chat_bot` lives under:

```text
${ALICE_HOME}/bots/chat_bot/
```

## Operating Alice

Typical operator flow:

1. Install or build Alice.
2. Copy and edit `config.yaml`.
3. Set Feishu app credentials for each bot.
4. Pick an LLM provider and verify the CLI login state.
5. Start Alice in foreground or through `systemd --user`.
6. Talk to the bot in Feishu.

## Scene Routing

Alice supports two primary scenes in group or topic-group chats:

- `chat`: low-friction conversational mode
- `work`: explicit task mode for focused threads

These are configured under `bots.<id>.group_scenes`.

### Chat Scene

`chat` is for normal conversation.

Recommended behavior:

- `enabled: true`
- `session_scope: per_chat`
- `llm_profile: chat`
- `no_reply_token: "[[NO_REPLY]]"`
- `create_feishu_thread: false`

What it does:

- The whole chat shares one scene session.
- Alice responds without requiring a dedicated work trigger.
- `/clear` rotates to a new chat session.
- If the model returns `no_reply_token`, Alice stays silent.

Use `chat` when you want a bot that behaves like a persistent group participant.

### Work Scene

`work` is for explicit task execution.

Recommended behavior:

- `enabled: true`
- `trigger_tag: "#work"`
- `session_scope: per_thread`
- `llm_profile: work`
- `create_feishu_thread: true`

What it does:

- A new work thread starts from a root message that matches the work trigger.
- By default this means a message like `#work @bot ...`.
- Alice creates or continues a dedicated work session for that Feishu thread.
- Replies stay scoped to the work thread instead of contaminating the bot's casual chat memory.

Use `work` when you want thread-local task execution, coding help, or automation-style interactions.

### Fallback Triggering

If both `group_scenes.chat.enabled` and `group_scenes.work.enabled` are `false`, Alice falls back to legacy trigger matching:

- `trigger_mode: at`: only messages that mention the bot are accepted
- `trigger_mode: prefix`: only messages starting with `trigger_prefix` are accepted

This fallback is mostly for simpler or older setups. New deployments should prefer explicit `group_scenes`.

## Recommended Patterns

### Chat-Only Bot

Use this when the bot should behave like a normal group assistant:

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

Use this when the same bot should chat casually but also support explicit task threads:

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

Different scenes can use different providers. If a profile omits `provider`, Alice falls back to the default provider.

## `SOUL.md`

Each bot can define persona and image metadata in `workspace/SOUL.md`.

Current machine-readable frontmatter keys:

- `image_refs`: reference images used for roleplay image generation
- `image_generation`: bot-level image generation policy stored with the soul, such as the minimum reply-will score required before Alice auto-generates an image
- `output_contract`: parsed reply metadata contract used for hidden-block stripping, silence suppression, and motion extraction

Example:

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

Notes:

- Relative paths are resolved relative to the directory containing `SOUL.md`.
- The frontmatter is parsed by Alice and stripped before the remaining body is appended to the LLM prompt.
- If you want the model to emit the matching tags or suppress token, write the corresponding instruction and examples directly in the `SOUL.md` body.

## Runtime API And Bundled Skills

Alice also exposes a local runtime HTTP API. Bundled skills use that API to:

- send messages or attachments
- schedule automation tasks
- interact with campaigns and other runtime state

This is why operators normally interact with Alice in Feishu while skills interact with Alice through the local runtime endpoint.

## Troubleshooting

- Bot never responds in group chats:
  Check `group_scenes`, `trigger_mode`, and whether `feishu_bot_open_id` / `feishu_bot_user_id` are configured.
- `work` mode never starts:
  Check that `group_scenes.work.enabled` is true, `trigger_tag` is set, and the triggering message matches the expected pattern.
- Wrong model or reasoning level:
  Check `llm_profiles` and confirm the scene points at the right profile.
- Skills fail to send attachments:
  Check `runtime_http_addr`, `runtime_http_token`, and runtime permissions.

## Related Docs

- [README](../README.md)
- [Chinese Usage Guide](./usage.zh-CN.md)
- [Architecture](./architecture.md)
- [Feishu Message Flow (Chinese)](./feishu-message-flow.zh-CN.md)
