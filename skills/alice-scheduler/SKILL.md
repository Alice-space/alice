---
name: alice-scheduler
description: Manage Alice automation tasks in the current chat through Alice's local runtime HTTP API. Use when the user wants to create, list, inspect, patch, pause, resume, or delete scheduled tasks, including `run_llm` and `run_workflow` jobs.
---

# Alice Scheduler

Use `scripts/alice-scheduler.sh` to operate automation tasks for the current chat. The script uses the local runtime HTTP API and current session context automatically.

## Commands

- List tasks in current scope:
  `scripts/alice-scheduler.sh list`
- Create a task from JSON:
  `scripts/alice-scheduler.sh create <<'JSON'`
  `{ "title": "daily sync", "schedule": { "type": "cron", "cron_expr": "0 1 * * *" }, "action": { "type": "run_llm", "prompt": "总结今天的进展" } }`
  `JSON`
- Get one task:
  `scripts/alice-scheduler.sh get task_xxx`
- Patch a task with merge patch JSON:
  `scripts/alice-scheduler.sh patch task_xxx '{"status":"paused"}'`
- Delete a task:
  `scripts/alice-scheduler.sh delete task_xxx`
- Inspect `code_army` workflow state in current chat:
  `scripts/alice-scheduler.sh code-army-status`
  `scripts/alice-scheduler.sh code-army-status rust-cli-calculator`

## Task Shape

- `schedule.type`: `interval` or `cron`
- `schedule.every_seconds`: required for `interval`, minimum `60`
- `schedule.cron_expr`: required for `cron`
- `action.type`: `send_text`, `run_llm`, or `run_workflow`
- `action.workflow`: for workflow tasks, currently use `code_army`
- `manage_mode`: `creator_only` or `scope_all` (`scope_all` only makes sense in group chats)

## Workflow

1. Use `list` before patching or deleting when task id is unknown.
2. Prefer `patch` with a narrow merge patch instead of rewriting the whole task.
3. For one-off runs, create an `interval` task with `every_seconds: 60` and `max_runs: 1`.
4. Use `code-army-status` to inspect workflow progress after scheduling `run_workflow`.

## Reply Pattern

- State the operation performed and the relevant `task.id`.
- Include the exact `next_run_at` for new or rescheduled tasks.
- Call out whether the task is one-off or recurring.
