# 配置 Chat 和 Work 场景

Alice 将收到的群消息路由到两个**场景**之一：`chat` 用于日常对话，`work` 用于明确的任务执行。

## 场景路由概览

```
收到消息
  ├─ 是内置命令？(/help、/status、/stop、/clear、/session)
  │   └─ 直接处理，不使用 LLM
  ├─ 匹配 work 触发词？(@Alice #work ...)
  │   └─ 路由到 work 场景
  └─ 其他情况
      └─ 路由到 chat 场景（如已启用）
```

两个场景都在 `bots.<id>.group_scenes` 下配置。

## Chat 场景

Chat 场景适用于低门槛的持久化对话。每个群聊共用一个 session。

```yaml
group_scenes:
  chat:
    enabled: true
    session_scope: "per_chat"
    llm_profile: "chat"
    no_reply_token: "[[NO_REPLY]]"
    create_feishu_thread: false
```

| 字段 | 说明 |
|-------|-------------|
| `enabled` | 设为 `true` 启用 chat 场景 |
| `session_scope` | `"per_chat"` — 整个群共用一个 session。`"per_thread"` — 每个飞书话题一个 session |
| `llm_profile` | `llm_profiles` 下的 LLM profile 名称 |
| `no_reply_token` | 模型返回此字符串时，Alice 保持静默不回复 |
| `create_feishu_thread` | 是否将回复包裹在飞书话题中 |

使用 `/clear` 重置 chat session 重新开始。

## Work 场景

Work 场景适用于面向任务的执行。每个 work 任务拥有独立的话题和 session。

```yaml
group_scenes:
  work:
    enabled: true
    trigger_tag: "#work"
    session_scope: "per_thread"
    llm_profile: "work"
    create_feishu_thread: true
```

| 字段 | 说明 |
|-------|-------------|
| `enabled` | 设为 `true` 启用 work 场景 |
| `trigger_tag` | 消息中必须（在 @bot 提及之后）包含的标签才能触发 work 模式 |
| `session_scope` | `"per_thread"` — 每个飞书话题独立的 session。`"per_chat"` — 共享 session |
| `llm_profile` | 要使用的 LLM profile 名称（通常使用更强大的模型） |
| `create_feishu_thread` | 自动为 work 回复创建飞书话题 |

Work 模式用法：

```
@Alice #work fix the login bug              → 启动 work，调用 LLM
@Alice #work                                 → 创建 work 话题但不调用 LLM
@Alice #work /session <backend-session-id>   → 将话题绑定到已有的后端 session
```

## 常见模式

### 仅 Chat 的 Bot

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

### Chat + Work 分离

Chat 使用轻量模型，work 使用更强模型：

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

## 旧版触发模式

如果 `chat` 和 `work` 都被禁用，Alice 将回退到旧版触发系统：

```yaml
bots:
  my_bot:
    trigger_mode: "at"       # at | prefix | all
    trigger_prefix: ""       # 仅当 trigger_mode 为 "prefix" 时使用
```

| 模式 | 行为 |
|------|----------|
| `at` | 仅接受 @bot 的消息 |
| `prefix` | 仅接受以 `trigger_prefix` 开头的消息 |
| `all` | 接受所有消息（无过滤） |

新部署建议优先使用显式的场景路由。
