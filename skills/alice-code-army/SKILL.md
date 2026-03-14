---
name: alice-code-army
description: 通过 Alice 本地调度 API 操作内置 `code_army` 工作流。适用于在当前飞书会话中启动、续跑、查看、暂停、恢复、删除或测试 `code_army`。
---

# Alice 代码军队

通过 `alice-scheduler` 驱动 `code_army`，不要重复扫描整个仓库。所有操作都限定在当前会话，由 Alice 自动路由回复。

## 默认约定

- 启动任务：`../alice-scheduler/scripts/alice-scheduler.sh create`
- 管理任务：`../alice-scheduler/scripts/alice-scheduler.sh list|get|patch|delete`
- 查看工作流状态：`../alice-scheduler/scripts/alice-scheduler.sh code-army-status`
- `action.type` 必须为 `run_workflow`，`workflow` 必须为 `code_army`
- 一次性测试推荐 `every_seconds: 60` 且 `max_runs: 1`
- `code_army` 没有“立即执行”接口，首次执行发生在 `next_run_at`，回复用户时必须写明时间
- 同一会话中并行多条工作流时，使用明确 `state_key`
- 复用同一个 `state_key` 会延续旧状态与目标，不应在后续 prompt 里重置目标
- 单次执行应走完当前阶段循环：`manager -> worker -> reviewer -> gate`，并停在下一轮稳定阶段
- `model` / `profile` 仅在用户明确要求或已有流程依赖时设置

## 启动一次性运行

1. 把用户需求整理成具体的工作流目标。
2. 若后续要可追踪/可续跑，先选一个短 `state_key`。
3. 创建一次性任务：`schedule.type: "interval"`、`schedule.every_seconds: 60`、`action.type: "run_workflow"`、`action.workflow: "code_army"`、`max_runs: 1`。
4. 创建后反馈 `task.id`、`next_run_at`、`state_key`。

示例：

```sh
../alice-scheduler/scripts/alice-scheduler.sh create <<'JSON'
{
  "title": "code_army: rust calculator",
  "schedule": { "type": "interval", "every_seconds": 60 },
  "action": {
    "type": "run_workflow",
    "workflow": "code_army",
    "state_key": "rust-cli-calculator",
    "prompt": "制作一个使用 Rust 编写的终端计算器，支持加减乘除即可。按 code_army 工作流推进一轮，并在回复中给出当前阶段进展。"
  },
  "max_runs": 1
}
JSON
```

## 继续推进

要延续已有状态，复用同一个 `state_key`。常用做法是再次创建一次性任务：

```sh
../alice-scheduler/scripts/alice-scheduler.sh create <<'JSON'
{
  "title": "code_army: continue rust calculator",
  "schedule": { "type": "interval", "every_seconds": 60 },
  "action": {
    "type": "run_workflow",
    "workflow": "code_army",
    "state_key": "rust-cli-calculator",
    "prompt": "继续推进 rust-cli-calculator 这一条 code_army 工作流。"
  },
  "max_runs": 1
}
JSON
```

如果用户希望持续运行而不是手动触发，改为周期任务（更大 interval 或 cron）。

## 查看状态

执行 `../alice-scheduler/scripts/alice-scheduler.sh code-army-status`（可带或不带 `state_key`）。

- 不带 `state_key`：列出当前会话全部 `code_army` 状态
- 带 `state_key`：读取指定工作流快照
- 优先阅读字段：`phase`、`iteration`、`last_decision`、`updated_at`、`history`

阶段含义：

- `manager`：规划本轮工作
- `worker`：产出实现结果
- `reviewer`：审查产物
- `gate`：决定进入下一迭代或退回返工

状态解释：

- `last_decision: "pass"`：下一次 gate 会推进到下一迭代
- `last_decision: "fail"`：下一次 gate 会打回给 worker 返工
- `history`：最快速的变更摘要来源

## 管理任务

- 不确定任务 ID 时，先 `list` 再编辑/删除。
- 暂停/恢复：
  `../alice-scheduler/scripts/alice-scheduler.sh patch <task_id> '{"status":"paused"}'`
  或
  `../alice-scheduler/scripts/alice-scheduler.sh patch <task_id> '{"status":"active"}'`
- 节奏调整使用 `patch`。
- 删除不再需要的任务：
  `../alice-scheduler/scripts/alice-scheduler.sh delete <task_id>`

## Cron 说明

Alice 自动化当前仅接受标准 5 段 cron。`cron_expr` 中不要写 `CRON_TZ=...`。调度基于运行机器的操作系统时区（`time.Local`），回复时应明确写出“按当前机器时区执行”。

## 回复模式

操作该工作流时，回复需包含：

- 本次动作（创建、更新、列出、查看、删除）
- 相关 `task.id` 与 `state_key`
- 新建或重排任务的精确 `next_run_at`
- 一次性还是周期任务
- 影响预期的限制（尤其“无立即执行”）
