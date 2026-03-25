---
name: alice-scheduler
description: 通过 Alice 本地 runtime HTTP API 管理当前会话的自动化任务。适用于创建、列出、查看、补丁更新、暂停、恢复、删除任务，以及处理 `send_text` / `run_llm` / `run_workflow` 任务。
---

# Alice 调度器

使用 `scripts/alice-scheduler.sh` 管理当前会话自动化任务。脚本会自动使用本地 runtime HTTP API 与当前会话上下文。

维护约束：当前会话里 `.agents/skills/...` 的已安装 skill 副本来自 Alice 安装/更新流程，不应直接修改；需要变更 skill 时，应修改 Alice 仓库里的 `alice/skills/...` 源文件，再通过安装流程同步进去。

## 常用命令

- 列出当前作用域任务：
  `scripts/alice-scheduler.sh list`
- 用 JSON 创建任务：
  `scripts/alice-scheduler.sh create <<'JSON'`
  `{ "title": "daily sync", "schedule": { "type": "cron", "cron_expr": "0 1 * * *" }, "action": { "type": "run_llm", "prompt": "总结今天的进展" } }`
  `JSON`
- 用 JSON 创建 workflow 任务：
  `scripts/alice-scheduler.sh create <<'JSON'`
  `{ "title": "fm16 reconcile", "schedule": { "type": "interval", "every_seconds": 900 }, "action": { "type": "run_workflow", "workflow": "code_army", "prompt": "/alice reconcile campaign camp_xxx；尽可能利用所有允许且可用的资源并行推进；若有清晰且安全的下一步就直接动手，并在必要时实际调整；只有确实做不了或继续做不安全时，才允许发 `/alice needs-human ...`，并在最终回复中追加 `<alice_command>/alice needs-human ...</alice_command>`。", "reasoning_effort": "high", "personality": "pragmatic" } }`
  `JSON`
- 查看单个任务：
  `scripts/alice-scheduler.sh get task_xxx`
- 用 merge patch 更新任务：
  `scripts/alice-scheduler.sh patch task_xxx '{"status":"paused"}'`
- 删除任务：
  `scripts/alice-scheduler.sh delete task_xxx`
## 任务结构

- `schedule.type`：`interval` 或 `cron`
- `schedule.every_seconds`：`interval` 必填，最小 `60`
- `schedule.cron_expr`：`cron` 必填
- `action.type`：`send_text`、`run_llm`、`run_workflow`
- `action.workflow`：`run_workflow` 必填；用于指定 workflow 名，例如 `code_army`
- `action.prompt`：`run_llm` / `run_workflow` 必填；workflow 的目标、命令或运行准则都由这里注入
- `action.state_key`：可选；给 workflow 一个稳定状态槽位，便于同一类任务持续推进
- `manage_mode`：`creator_only` 或 `scope_all`（`scope_all` 仅群聊有意义）

## 工作流

1. 不知道任务 ID 时，先 `list` 再改删。
2. 更新任务优先用小范围 `patch`，不要整对象重写。
3. 一次性执行推荐：`interval + every_seconds: 60 + max_runs: 1`。
4. `run_llm` 适合纯汇报；`run_workflow` 适合真正推进任务，例如 reconcile、triage、resubmit 这类会调用工具并产生状态变化的流程。
5. 创建 workflow 定时任务时，优先把 `workflow` 写死，把可变部分放进 `prompt` / `state_key`；不要让 agent 每次自由改 workflow 名。
6. 创建 `heartbeat` / `reconcile` 一类周期 `run_workflow` 任务时，`prompt` 应明确两点：尽可能利用所有允许资源并行推进；若已经识别出清晰且安全的下一步，就在同一轮实际动手，并在必要时实际调整任务、试验或资源分配，不能只看不动。若确实需要人工介入，应要求 workflow 明确发出 `/alice needs-human ...`，并带上隐藏指令块 `<alice_command>...</alice_command>`，让 runtime 自动暂停任务并发警告卡。

## 回复模式

- 明确说明执行了什么操作，以及对应的 `task.id`。
- 新建或重排任务时，给出精确 `next_run_at`。
- 说明这是一次性任务还是周期任务。
