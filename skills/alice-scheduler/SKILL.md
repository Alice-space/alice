---
name: alice-scheduler
description: 通过 Alice 本地 runtime HTTP API 管理当前会话的定时任务。适用于创建、列出、查看、补丁更新、暂停、恢复、删除周期性或一次性定时任务。
---

# Alice 调度器

使用 `scripts/alice-scheduler.sh` 管理当前会话的定时任务。每次执行都是独立的 LLM 会话，不会续接之前的对话上下文。如需在同一线程内持续执行的长期目标，请使用 `alice-goal`。

## 常用命令

- 列出当前作用域任务： `scripts/alice-scheduler.sh list`
- 用 JSON 创建任务： `scripts/alice-scheduler.sh create '{"prompt":"总结今天的进展","every_seconds":3600,"max_runs":1}'`
- 查看单个任务： `scripts/alice-scheduler.sh get task_xxx`
- 用 merge patch 更新任务： `scripts/alice-scheduler.sh patch task_xxx '{"status":"paused"}'`
- 删除任务： `scripts/alice-scheduler.sh delete task_xxx`

## 任务字段

模型只需要填以下字段：

| 字段 | 类型 | 必须 | 说明 |
|------|------|------|------|
| `prompt` | string | 是 | 醒来后执行什么操作，支持 `{{now}}` `{{date}}` `{{time}}` `{{unix}}` 模板变量 |
| `every_seconds` | int | 与cron二选一 | 间隔秒数，最小60 |
| `cron` | string | 与every_seconds二选一 | cron 表达式 |
| `max_runs` | int | 否 | 最大执行次数，0=无限（默认） |
| `title` | string | 否 | 任务标题，不填自动显示"未命名任务" |

## 使用建议
1. 不知道任务 ID 时，先 `list` 再改删。更新任务优先用 patch。
2. `prompt` 写清楚产出格式和边界，避免周期任务逐次漂移。
3. 需要同一线程内持续执行的长期目标，使用 `alice-goal` 而非 alice-scheduler。
