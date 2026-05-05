# Configure Private Chat

Alice can handle direct messages (private chats) with the same scene routing as group chats.

## Private vs Group Scenes

Group chats use `group_scenes`. Direct messages use `private_scenes`. They are configured identically but under different keys:

```yaml
bots:
  my_bot:
    group_scenes:
      chat: { ... }
      work: { ... }

    private_scenes:
      chat: { ... }
      work: { ... }
```

Private scenes are **disabled by default**. Enable them explicitly.

## Chat Scene (Private)

```yaml
private_scenes:
  chat:
    enabled: true
    session_scope: "per_user"        # one session per DM user
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
    create_feishu_thread: false
```

| Field | Description |
|-------|-------------|
| `session_scope` | `"per_user"` — all DMs from the same user share one session. `"per_message"` — each DM creates a new session |
| `llm_profile` | Same profile reference as group scenes |
| `no_reply_token` | Suppress reply token |

Typical use: a personal assistant available in DMs, maintaining context per user.

## Work Scene (Private)

```yaml
private_scenes:
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_message"     # each #work DM starts fresh
    llm_profile: "work"
    create_feishu_thread: true
```

| Field | Description |
|-------|-------------|
| `session_scope` | `"per_message"` recommended for DMs — each task is isolated |

## Full Example

```yaml
bots:
  my_bot:
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

    private_scenes:
      chat:
        enabled: true
        session_scope: "per_user"
        llm_profile: "chat"
        no_reply_token: "[[NO_REPLY]]"
      work:
        enabled: true
        trigger_tag: "#work"
        session_scope: "per_message"
        llm_profile: "work"
        create_feishu_thread: true
```

## Behavior Differences from Group Chat

- **Mentions are implicit** — DMs don't require @bot. Every message is directed at the bot.
- **user_id resolution** — Alice resolves the DM user's name via Feishu API
- **Thread creation** — When `create_feishu_thread: true`, work replies create threads within the DM
