---
name: alice-scheduler
description: 通过 Alice 本地 runtime HTTP API 管理当前会话的自动化任务。适用于创建、列出、查看、补丁更新、暂停、恢复、删除任务。
---

# Alice 调度器

使用 `scripts/alice-scheduler.sh` 管理当前会话自动化任务。脚本会自动处理当前会话上下文（session key、thread ID 等），无需手动填写。

## 常用命令

- 列出当前作用域任务：
  `scripts/alice-scheduler.sh list`

- 用 JSON 创建任务（最小字段）：
  `scripts/alice-scheduler.sh create '{"prompt":"总结今天的进展","every_seconds":300,"max_runs":1}'`

- 查看单个任务：
  `scripts/alice-scheduler.sh get task_xxx`

- 用 merge patch 更新任务：
  `scripts/alice-scheduler.sh patch task_xxx '{"status":"paused"}'`

- 删除任务：
  `scripts/alice-scheduler.sh delete task_xxx`

## 任务字段

模型只需要填以下字段：

| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `prompt` | string | 是 | 醒来后执行什么操作，支持 `{{now}}` `{{date}}` `{{time}}` `{{unix}}` 模板变量 |
| `every_seconds` | int | 与cron二选一 | 间隔秒数，最小60 |
| `cron` | string | 与every_seconds二选一 | cron 表达式 |
| `max_runs` | int | 否 | 最大执行次数，0=无限（默认） |
| `fresh` | bool | 否 | 每次新开 LLM 线程，默认 false（续接当前对话） |
| `title` | string | 否 | 任务标题，不填自动显示"未命名任务" |

**以下字段由脚本自动注入，模型不需要填写：**
- `resume_thread_id`：当前 LLM 线程 ID（从 `$ALICE_RESUME_THREAD_ID` 注入）
- session 路由、scope、creator 等上下文信息（服务端自动处理）

## 使用模式

### 1. 一次性唤醒（wake-up）

在当前 thread 里工作，需要等待外部结果，N秒后醒来继续：

```json
{"prompt":"检查上一步的脚本输出，汇报结果","every_seconds":300,"max_runs":1}
```

`fresh` 默认 false → 续接当前 LLM 会话上下文，醒来后能看到之前的对话。

### 2. 每日定时报告

在群聊里创建，每次独立运行：

```json
{"prompt":"总结昨天群里的讨论要点","cron":"0 9 * * *","fresh":true}
```

`fresh: true` → 每次都是新的 LLM 会话，不续接上一次。

### 3. 暂停/恢复任务

```bash
scripts/alice-scheduler.sh patch task_xxx '{"status":"paused"}'
scripts/alice-scheduler.sh patch task_xxx '{"status":"active"}'
```

## 使用建议

1. 不知道任务 ID 时，先 `list` 再改删。
2. 更新任务优先用 patch，不要整体重写。
3. `prompt` 写清楚产出格式和边界，避免周期任务逐次漂移。
4. 续接对话用默认的 `fresh: false`，日常报表用 `fresh: true`。
