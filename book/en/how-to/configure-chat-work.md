# Configure Chat & Work Scenes

Alice routes incoming group messages into one of two **scenes**: `chat` for casual conversation, and `work` for explicit task execution.

## Scene Routing Overview

```
Incoming Message
  ├─ Built-in command? (/help, /status, /stop, /clear, /session)
  │   └─ Handle directly, no LLM
  ├─ Matches work trigger? (@Alice #work ...)
  │   └─ Route to work scene
  └─ Otherwise
      └─ Route to chat scene (if enabled)
```

Both scenes are configured under `bots.<id>.group_scenes`.

## Chat Scene

The chat scene is for low-friction, persistent conversation. One session per chat group.

```yaml
group_scenes:
  chat:
    enabled: true
    session_scope: "per_chat"
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
    create_feishu_thread: false
```

| Field | Description |
|-------|-------------|
| `enabled` | Set to `true` to activate the chat scene |
| `session_scope` | `"per_chat"` — one session for the whole group. `"per_thread"` — one session per Feishu thread |
| `llm_profile` | Name of the LLM profile under `llm_profiles` to use |
| `no_reply_token` | If the model returns this exact string, Alice stays silent instead of replying |
| `create_feishu_thread` | Whether to wrap replies in a Feishu thread |

Use `/clear` to reset the chat session and start fresh.

## Work Scene

The work scene is for task-oriented execution. Each work task gets its own thread and session.

```yaml
group_scenes:
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_thread"
    llm_profile: "work"
    create_feishu_thread: true
```

| Field | Description |
|-------|-------------|
| `enabled` | Set to `true` to activate the work scene |
| `trigger_tag` | The tag that must appear in a message to trigger work mode (after the @bot mention) |
| `session_scope` | `"per_thread"` — each Feishu thread gets its own session. `"per_chat"` — shared session |
| `llm_profile` | Name of the LLM profile to use (typically a more capable model) |
| `create_feishu_thread` | Automatically create a Feishu thread for work replies |

Work mode usage:

```
@Alice #work fix the login bug              → Starts work, calls LLM
@Alice #work                                 → Creates work thread without calling LLM
@Alice #work /session <backend-session-id>   → Binds thread to existing backend session
```

## Common Patterns

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

### Split Chat + Work

Use a lighter model for chat and a more capable one for work:

```yaml
llm_profiles:
  chat:
    provider: "opencode"
    model: "deepseek/deepseek-v4-flash"
  work:
    provider: "opencode"
    model: "deepseek/deepseek-v4-pro"
    variant: "max"
    permissions:
      sandbox: "danger-full-access"
      ask_for_approval: "never"

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

## Legacy Trigger Mode

If both `chat` and `work` are disabled, Alice falls back to a legacy trigger system:

```yaml
bots:
  my_bot:
    trigger_mode: "at"       # at | prefix | all
    trigger_prefix: ""       # only used when trigger_mode is "prefix"
```

| Mode | Behavior |
|------|----------|
| `at` | Only @bot messages are accepted |
| `prefix` | Only messages starting with `trigger_prefix` |
| `all` | Every message is accepted (no filter) |

New deployments should prefer explicit scene routing.
