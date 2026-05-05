# 配置私聊场景

Alice 可以使用与群聊相同的场景路由来处理私聊消息。

## 私聊 vs 群聊场景

群聊使用 `group_scenes`。私聊使用 `private_scenes`。两者的配置方式相同，但位于不同的配置键下：

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

私聊场景**默认禁用**。需要显式启用。

## Chat 场景（私聊）

```yaml
private_scenes:
  chat:
    enabled: true
    session_scope: "per_user"        # 每个 DM 用户共用一个 session
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
    create_feishu_thread: false
```

| 字段 | 说明 |
|-------|-------------|
| `session_scope` | `"per_user"` — 同一用户的所有 DM 共用一个 session。`"per_message"` — 每条 DM 创建新 session |
| `llm_profile` | 与群聊场景相同的 profile 引用 |
| `no_reply_token` | 抑制回复的 token |

典型用途：在 DM 中提供个人助手服务，按用户维护上下文。

## Work 场景（私聊）

```yaml
private_scenes:
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_message"     # 每条 #work DM 都是全新 session
    llm_profile: "work"
    create_feishu_thread: true
```

| 字段 | 说明 |
|-------|-------------|
| `session_scope` | DM 推荐 `"per_message"` — 每个任务隔离 |

## 完整示例

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

## 与群聊的行为差异

- **提及是隐式的** — DM 不需要 @bot。每条消息都直接面向 bot。
- **user_id 解析** — Alice 通过飞书 API 解析 DM 用户的名称
- **话题创建** — 当 `create_feishu_thread: true` 时，work 回复会在 DM 内创建话题
