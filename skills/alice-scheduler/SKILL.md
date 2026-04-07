---
name: alice-scheduler
description: 通过 Alice 本地 runtime HTTP API 管理当前会话的自动化任务。适用于创建、列出、查看、补丁更新、暂停、恢复、删除任务，以及处理 `run_llm` 任务。
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
  **注意**：`create` 子命令会自动从环境变量注入 `resume_session_key`（来自 `ALICE_SESSION_KEY`）和 `action.resume_thread_id`（来自 `ALICE_RESUME_THREAD_ID`），如果 JSON 中这两个字段为空/未设置。JSON 中显式提供的值优先，不会被覆盖。
- **获取当前 session 信息**（仅当需要手动查看或调试会话上下文时使用）：
  `scripts/alice-scheduler.sh current-session`
  输出：`{"session_key":"...","resume_thread_id":"..."}`
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
- `action.type`：固定为 `run_llm`
- `action.prompt`：必填；本次定时运行要执行的提示词、目标或操作说明
- `action.model`：可选；指定模型名
- `action.provider`：可选；指定 provider 名
- `action.state_key`：可选；给 Codex/Kimi 这类 provider 提供稳定 thread-ID 槽位，让同一类任务持续落在同一会话状态上
- `action.resume_thread_id`：可选；sticky thread ID。若已知上一次运行对应的 provider thread/session，可在这里显式续接；每次成功执行后系统会自动更新为最新 thread/session ID
- `action.source_message_id`：可选；Feishu message ID（`om_xxx`）。设置后，每次发送改走 Reply API + `reply_in_thread=true`，结果落在同一 thread。首次留空时，系统会在第一次成功发送后自动写回该字段，后续运行自动 in-thread 回复
- `resume_session_key`：可选顶层字段；把结果路由到指定 Feishu 渠道或 work thread，而不是默认当前会话。适合把定时任务固定回复到某个群、P2P 会话或现有 thread
- `manage_mode`：`creator_only` 或 `scope_all`（`scope_all` 仅群聊有意义）

## run_llm 使用指南

### 基础字段

最小可用任务只需要 `action.type: run_llm` 和 `action.prompt`。若需要固定模型栈，可以同时指定 `action.model` 与 `action.provider`。

```yaml
title: daily-sync
schedule:
  type: cron
  cron_expr: "0 9 * * *"
action:
  type: run_llm
  provider: openai
  model: gpt-5.4
  prompt: |
    总结昨天进展，列出今天优先级最高的三件事。
```

### 持续续接同一线程

`action.state_key`、`action.resume_thread_id`、`action.source_message_id` 解决的是不同层面的“续接”：

- `action.state_key`：给 Codex/Kimi provider 一个稳定状态槽位。适合同一类定时任务长期复用同一 provider 侧线程标识
- `action.resume_thread_id`：显式保存并续接某个 provider thread/session。系统每次成功运行后都会自动刷新它，适合 sticky thread
- `action.source_message_id`：控制 Feishu 投递 thread。第一次成功发送后可自动 bootstrap，后续始终 reply 到同一条消息线程

如果你想让任务既续接 LLM 会话，又持续回复到同一个 Feishu thread，可以三者一起使用：

```yaml
title: campaign-followup
resume_session_key: chat_id:oc_xxx|scene:work|seed:om_seed_xxx
schedule:
  type: interval
  every_seconds: 1800
action:
  type: run_llm
  provider: codex
  model: gpt-5.4
  state_key: camp_active
  resume_thread_id: uuid-xxx
  source_message_id: om_seed_xxx
  prompt: |
    延续当前任务上下文，检查最新状态，只汇报新的阻塞和下一步。
```

### 用 `resume_session_key` 固定回复渠道

`resume_session_key` 是顶层字段，不放在 `action` 里。它决定结果发往哪个 Feishu 渠道或 thread：

- **群或 P2P 会话**：`chat_id:oc_xxx` 或 `user_id:xxx`
- **Work thread（推荐）**：`chat_id:oc_xxx|scene:work|seed:om_xxx`
- **不推荐别名**：`|thread:omt_xxx` 不能作为 thread 级投递目标，最终会回退到群主渠道

`create` 命令会自动注入 `resume_session_key` 和 `action.resume_thread_id`，无需手动调用 `current-session` 再复制粘贴。

下面是把旧的 Resume 模式直接写成 `run_llm` YAML 的方式：

```yaml
title: thread-resume
resume_session_key: chat_id:oc_xxx|scene:work|seed:om_xxx
schedule:
  type: cron
  cron_expr: "0 9 * * *"
action:
  type: run_llm
  provider: codex
  model: gpt-5.4
  resume_thread_id: uuid-xxx
  prompt: |
    继续昨天的线程上下文，汇总最新进展并指出仍未解决的问题。
```

触发时的行为：
1. 若 `action.resume_thread_id` 非空，系统会按该 thread/session 续接；首次可留空，由第一次成功运行自动写回
2. 若 `resume_session_key` 指向 work thread，系统会按 `seed:om_xxx` 回复到正确的 Feishu thread
3. 若 `action.source_message_id` 为空，系统会在首次成功发送后自动 bootstrap，供后续继续 in-thread 回复

安全约束：`resume_session_key` 的 channel 必须与创建请求的当前 scope 一致，不能跨渠道重定向。

### 自动 bootstrap thread

如果不想手动准备 thread 锚点，可以只创建普通 `run_llm` 任务，不填 `action.source_message_id`。系统会在第一次成功发送后自动把首条消息 ID 写回 `action.source_message_id`，之后每次运行都回复到同一 thread。

```yaml
title: weekly-report
schedule:
  type: cron
  cron_expr: "0 18 * * 5"
action:
  type: run_llm
  provider: kimi
  model: kimi-latest
  state_key: weekly_report
  prompt: |
    生成本周总结，突出已完成事项、风险和下周计划。
```

## 使用建议

1. 不知道任务 ID 时，先 `list` 再改删。
2. 更新任务优先用小范围 `patch`，不要整对象重写。
3. 一次性执行推荐：`interval + every_seconds: 60 + max_runs: 1`。
4. `action.prompt` 要写清楚产出格式、边界和是否只汇报不执行，避免周期任务逐次漂移。
5. `action.state_key` 适合按任务类别复用上下文；`action.resume_thread_id` 适合续接某个具体会话；两者可以同时存在，但要明确谁是你要固定的主键。
6. 需要把结果持续发回同一个 Feishu 线程时，优先使用 `resume_session_key` 的规范形式 `chat_id:...|scene:work|seed:...`；如果没有现成 thread，就依赖 `action.source_message_id` 的自动 bootstrap。

## 回复模式

- 明确说明执行了什么操作，以及对应的 `task.id`。
- 新建或重排任务时，给出精确 `next_run_at`。
- 说明这是一次性任务还是周期任务。
