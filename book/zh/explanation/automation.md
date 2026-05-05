# 自动化子系统

Alice 的自动化引擎调度并执行定期任务、工作流和系统维护。

## 架构

自动化子系统（`internal/automation/`）使用基于 tick 的执行模型，配合持久化存储。

```
Automation Engine
  ├─ Tick Scheduler（周期性循环）
  │   ├─ 认领到期任务
  │   ├─ 执行任务（send_text / run_llm / run_workflow）
  │   └─ 处理完成 / 失败
  ├─ System Task Scheduler
  │   ├─ Session 状态刷新
  │   └─ Campaign 调和
  ├─ Watchdog
  │   └─ 对超期或卡住的任务发出告警
  └─ Store（bbolt）
      └─ 任务持久化
```

## 任务模型

### 作用域

任务的作用域定义其执行位置：

| 作用域 | 说明 |
|-------|-------------|
| `user` | 限定于特定用户（DM 上下文） |
| `chat` | 限定于特定群聊 |

### 操作

| 操作 | 说明 |
|--------|-------------|
| `send_text` | 向作用域发送预设文本消息 |
| `run_llm` | 在作用域中按指定 prompt 运行 LLM 调用 |
| `run_workflow` | 运行结合 LLM 调用和操作的多步工作流 |

### 调度

任务可通过两种方式调度：

- **Cron 表达式**：`"0 9 * * *"` — 每天 9 点运行
- **一次性时间戳**：ISO 8601 — 在指定时间运行一次

### 任务生命周期

```
Created → Active → Claimed → Executing → Completed
                                  ↓
                              Failed → Active（重试）/ Cancelled
```

- 到期任务在周期性 tick 中被**认领**（每次 tick 认领一个）
- 被认领的任务在作用域的对话上下文中**执行**
- 带 cron 表达式的**已完成**任务会被重新调度到下一次
- **失败**任务可能被重试或取消
- **已取消**任务被删除或标记为不活跃

## 执行模型

任务执行时：

1. 引擎获取任务作用域的 session 锁
2. 任务继承与交互式运行相同的对话上下文：
   - 相同的工作空间目录
   - 相同的 LLM profile 和权限
   - 相同的环境变量
3. 对于 `run_llm` 和 `run_workflow`，任务的 prompt 被发送到 LLM 后端
4. 回复发送到任务的作用域（群聊或用户 DM）

> 用户消息可以中断已获取 session 锁的自动化任务。

## 系统任务

Alice 在引导过程中注册内建的系统任务：

| 任务 | 间隔 | 用途 |
|------|----------|---------|
| Session 状态刷新 | 周期性 | 将内存中的 session 状态持久化到 `session_state.json` |
| Campaign 调和 | 周期性 | 同步 campaign 仓库状态 |

## Watchdog

Watchdog 监控自动化任务的异常：

- **超期任务**：已过调度时间但尚未被认领的任务
- **卡住任务**：执行时间过长的任务

当 Watchdog 检测到问题时，它可以：
- 记录警告日志
- 向配置的群聊发送告警消息
- 强制取消卡住的任务

## 存储

任务持久化在本地 bbolt 数据库中：

```
~/.alice/bots/<bot_id>/run/connector/automation.db
```

进程重启后仍存在。存储支持：
- 任务的 CRUD 操作
- 按作用域、状态和到期时间查询
- 原子性的认领并更新以防止重复执行

## 管理任务

### 通过 Runtime API

```bash
alice runtime automation create '{
  "scope_type": "chat",
  "scope_id": "oc_xxxxxxxxxxxxx",
  "action": "send_text",
  "text": "Daily standup reminder!",
  "cron": "0 10 * * 1-5"
}'
```

### 通过 Bundled Skill

`alice-scheduler` skill 让用户可以直接从飞书对话中创建和管理任务。

完整任务管理端点参见 [Runtime API 参考](../reference/runtime-api.md)。
