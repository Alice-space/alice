# 系统模型

本页讲解 Alice 背后的基本概念：多 bot 架构、场景路由、session 和启动模式。理解这些有助于你高效配置和排错。

## Alice 是什么（以及不是什么）

Alice 是一个**连接器**，而非 bot 框架。它不直接实现聊天逻辑、NLU 或自定义集成。实际上，它：

1. 从飞书接收消息
2. 决定调用哪个 LLM 后端以及如何调用
3. 以子进程方式调用 LLM CLI
4. 将响应发回飞书

"智能"部分在 LLM 后端（Codex、Claude 等）。Alice 处理"管道"工作：路由、排队、session 管理、附件 I/O 和进度展示。

## 多 Bot 模型

一个 `alice` 进程可以通过一份 `config.yaml` 托管多个独立的 bot：

```yaml
bots:
  engineering_bot:
    feishu_app_id: "cli_11111"
    # ...
  support_bot:
    feishu_app_id: "cli_22222"
    # ...
```

每个 bot 拥有自己独立的：
- **Runtime 目录**（`~/.alice/bots/<bot_id>/`）
- **工作空间**、prompt 和 SOUL.md
- **飞书凭据**（App ID、App Secret）
- **LLM profile** — 可以使用不同的 provider 和模型
- **场景配置** — 独立的 chat/work 路由
- **Runtime API 端口** — 自动递增（7331、7332……）

Bot 之间共享：
- 相同的进程和 worker 池
- 默认的 `CODEX_HOME`（可按 bot 覆盖）

### Bot 目录布局

```
~/.alice/bots/<bot_id>/
├── workspace/                        # Agent 工作空间
├── prompts/                          # Prompt 模板覆盖
├── SOUL.md                           # Bot 人格
└── run/connector/
    ├── automation.db                 # 持久化任务存储（bbolt）
    ├── campaigns.db                  # 活动索引（bbolt）
    ├── session_state.json            # Session 别名、用量计数器
    ├── runtime_state.json            # 可变运行时状态
    └── resources/scopes/             # 已下载的附件和产物
```

## 场景路由

每条收到的群消息经过一个决策树：

```
收到消息
  │
  ├─ 是内置命令？(/help、/status、/stop、/clear、/session)
  │   └─ 是 → 直接处理，不涉及 LLM
  │
  ├─ 匹配 work 触发词？(@Bot #work ...)
  │   └─ 是 → 路由到 work 场景
  │
  ├─ chat 场景已启用？
  │   └─ 是 → 路由到 chat 场景
  │
  └─ 两个场景都禁用？
      └─ 回退到旧版 trigger_mode（at / prefix / all）
```

### 场景 vs 旧版触发

旧版 `trigger_mode`（at/prefix/all）是一个简单的闸门：它决定是接受还是忽略消息。如果接受，只有一个 LLM 流水线。

场景更进一步：它们为每个场景分配不同的 LLM profile、session 作用域、话题行为和 SOUL.md 处理方式。新部署应始终使用场景。

## Session 管理

**Session** 是 LLM 的上下文窗口。Alice 决定何时开始新 session，何时继续已有 session。

### Session Key

Alice 使用规范 key 来标识 session：

| 格式 | 示例 |
|--------|---------|
| `{receive_id_type}:{receive_id}` | `chat_id:oc_123` |
| `{key}|scene:{scene}` | `chat_id:oc_123|scene:chat` |
| `{key}|scene:{scene}|thread:{thread_id}` | `chat_id:oc_123|scene:work|thread:om_456` |

### Session 作用域

`session_scope` 控制何时创建和复用 session：

| 作用域 | 行为 |
|-------|----------|
| `per_chat` | 整个群/DM 共用一个 session |
| `per_thread` | 每个飞书话题一个 session |
| `per_user` | （仅 DM）每个用户一个 session |
| `per_message` | （仅 DM）每条消息新建 session |

### Session 持久化

Alice 将 session 元数据持久化到 `session_state.json`：
- Provider thread ID（用于与后端恢复）
- Session 别名
- 用量计数器
- 最后消息时间戳
- Work-thread ID 别名

当收到一个新 job 时，Alice 检查是否存在活跃 session。如果存在：
- **Provider 原生注入**：部分后端（Codex、Claude）允许向正在运行的 session 注入新输入。Alice 优先尝试此方式。
- **排队**：如果原生注入失败且 LLM 运行仍活跃，新 job 排队等待。较新的 job 会取代队列中较旧的 job。
- **新运行**：如果没有活跃的运行，则向 LLM 后端发送新的 RunRequest。

### 取消和中断

- `/stop` 立即通过 context 取消取消活跃的 LLM 运行
- 较新的用户消息会取代排队的 job，但不会中断活跃的运行
- 自动化任务也可能被获取了 session 锁的用户消息中断

## 启动模式

Alice 支持两种显式的启动模式：

### `--feishu-websocket`
完整模式。连接飞书 WebSocket，处理实时消息，运行自动化，并暴露 Runtime API。

### `--runtime-only`
仅本地模式。Runtime API 和自动化引擎运行，但飞书连接器不启动。用于：
- 调试和开发
- 仅运行自动化调度器
- 无头环境（使用 `alice-headless --runtime-only`）

> `alice-headless` 是一个专用二进制文件，*不能*启动飞书连接器。尝试 `alice-headless --feishu-websocket` 会报错。

## 配置热重载

- **单 bot 模式**：支持有限的局部热重载。部分配置项会被监听变化。
- **多 bot 模式**：热重载被刻意禁用。配置更改后务必重启 Alice。

## Runtime Home

| 构建渠道 | 默认 Home |
|---------------|-------------|
| Release（npm / 安装脚本） | `~/.alice` |
| Dev（源码编译） | `~/.alice-dev` |

可通过 `--alice-home` 或环境变量 `ALICE_HOME` 覆盖。
