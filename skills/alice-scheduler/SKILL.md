---
name: alice-scheduler
description: 管理当前 session 的定时任务
---

# Alice Scheduler

定时执行 prompt。每次执行是独立 LLM 会话。长期连续目标请用 alice-goal。

## 命令

- `scripts/alice-scheduler.sh list` - 列出任务
- `scripts/alice-scheduler.sh create '{"prompt":"...","every_seconds":3600}'` - 创建
- `scripts/alice-scheduler.sh create '{"prompt":"...","cron":"0 9 * * *","max_runs":5}'` - cron 方式
- `scripts/alice-scheduler.sh get task_xxx` - 查看
- `scripts/alice-scheduler.sh patch task_xxx '{"status":"paused"}'` - 修改
- `scripts/alice-scheduler.sh delete task_xxx` - 删除

## 参数

| 字段 | 必须 | 说明 |
|------|------|------|
| `prompt` | 是 | 执行内容，支持 `{{now}}` `{{date}}` `{{time}}` `{{unix}}` |
| `every_seconds` | 与 cron 二选一 | ≥60 秒 |
| `cron` | 与 every_seconds 二选一 | cron 表达式 |
| `max_runs` | 否 | 最大次数，0=无限 |
| `title` | 否 | 标题 |

## 建议

先 list 再操作。更新用 patch。不要传递 scope/用户参数，由 session 自动确定。
