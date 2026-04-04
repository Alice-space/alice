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
- `action.resume_thread_id`：可选；Claude Code session UUID，用于 `--resume`，实现跨次运行的对话续接（sticky thread）；每次成功执行后系统自动更新为最新 session ID
- `resume_session_key`：可选顶层字段；指定要 Resume 的原始会话键；系统自动从中解析发送渠道和 `session_key`，覆盖默认的当前会话路由。**格式说明见 Resume 模式章节。**
- `manage_mode`：`creator_only` 或 `scope_all`（`scope_all` 仅群聊有意义）

## Resume 模式

Resume 模式允许定时任务在特定已有会话上下文中执行，并把结果回送到该会话的原始渠道（群 + thread）。

### session key 格式

- **群 P2P 会话**：`chat_id:oc_xxx` 或 `user_id:xxx`，直接发到该渠道
- **Work thread（推荐）**：使用**规范形式** `chat_id:oc_xxx|scene:work|seed:om_xxx`，其中 `seed` 是 thread 的锚定消息 ID。系统用 Reply API + `reply_in_thread=true` 把消息发到正确的 Feishu thread
- **Thread 别名形式** `|thread:omt_xxx` **不支持 thread 级投递**（Feishu 不允许以 `thread_id` 为 `receive_id_type` 发消息），会回退到群主渠道

**如何获取规范 session key**：在目标 thread 里向 Alice 发消息，Alice 运行时的 `SessionKey` 环境变量即为规范形式（`|scene:work|seed:om_xxx`）。Alice 在 thread 内创建任务时会自动使用该形式。

**创建 Resume workflow 任务示例**（从群里的 work thread 定时续接）：

```
scripts/alice-scheduler.sh create <<'JSON'
{
  "title": "每日 thread 续跑",
  "resume_session_key": "chat_id:oc_xxx|scene:work|seed:om_xxx",
  "schedule": { "type": "cron", "cron_expr": "0 9 * * *" },
  "action": {
    "type": "run_workflow",
    "workflow": "code_army",
    "prompt": "继续推进昨天的工作，检查 CI 状态并处理 pending 任务。"
  }
}
JSON
```

触发时的行为：
1. 若 `action.resume_thread_id` 非空，以 `claude --resume <id>` 续接 Claude 会话；首次可为空（全新开始）
2. 以 Reply API + `reply_in_thread=true` 回复 `seed:om_xxx` 消息，消息出现在正确的 Feishu thread 里
3. 每次成功后，系统自动把新 Claude session ID 写回 `action.resume_thread_id`，下次继续续接

**安全约束**：`resume_session_key` 的 channel 必须与创建请求的当前 scope 一致（同一个群或同一个 P2P 会话），不能跨渠道重定向。

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
