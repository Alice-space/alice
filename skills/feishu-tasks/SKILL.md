---
name: feishu-tasks
description: Manage official Feishu Task v2 objects via OpenAPI for task and tasklist CRUD, assignment, deadline updates, and member collaboration. Use only when the user explicitly says `飞书任务`, `Feishu Task`, `Task v2`, or otherwise clearly requests the official Feishu task system. Do not trigger for Alice automation tasks unless the user explicitly asks for Alice tasks.
---

# Feishu Task

## Overview

Use official Feishu OpenAPI `task/v2` to manage tasks and tasklists.
Keep this skill strictly separated from Alice automation tasks.

## Boundary Rules

1. Treat `Alice任务` and `飞书任务` as different systems.
- `Alice任务`: connector-local automation tasks (for example `automation_task_*` operations).
- `飞书任务`: official Feishu OpenAPI Task objects (`/open-apis/task/v2/...`).

2. Trigger this skill only with explicit Feishu-task intent.
- Positive signals: `飞书任务`、`Feishu Task`、`task/v2`、`任务清单` with OpenAPI context.
- Negative/ambiguous signals: generic `任务` with no qualifier. Ask a clarification question before acting.

3. Prefer Task v2 APIs for all operations in this skill.
- Do not fallback to `task/v1` unless user explicitly asks for compatibility behavior.

## Workflow

1. Confirm system scope before execution.
- If user says `飞书任务`, continue with this skill.
- If user says `Alice任务`, switch to Alice automation path.

2. Confirm token mode and identity type.
- For `当前人的任务`, use `user_access_token` (Task v2 list endpoint requires user token).
- For create/update/delete operations, user token or tenant token can be used by API support.
- Default `user_id_type` to `open_id` unless user requests otherwise.

3. Execute operation via script first.
- Use `scripts/feishu-task-v2.sh` for deterministic CRUD calls.
- If script cannot satisfy a specific payload, call the same Task v2 endpoint directly.

4. Return concise operation output.
- Include task/tasklist identifiers, key fields changed, and next action suggestion.
- When API returns non-zero code, include code and msg and stop blind retries.

## Required Operation Coverage

Use the following mappings when user asks for CRUD:

1. Check tasks currently managed by me.
- `list-managed-tasklists` then `list-tasklist-tasks <tasklist_guid>`.
- Endpoints: `GET /open-apis/task/v2/tasklists`, `GET /open-apis/task/v2/tasklists/:tasklist_guid/tasks`.

2. Check current person's tasks.
- `list-my-tasks`.
- Endpoint: `GET /open-apis/task/v2/tasks` (with `type=my_tasks`).

3. Publish a new task.
- `create-task`.
- Endpoint: `POST /open-apis/task/v2/tasks`.

4. Assign specific people to a task.
- `assign-task`.
- Endpoint: `POST /open-apis/task/v2/tasks/:task_guid/add_members`.

5. Set task deadline.
- `set-deadline`.
- Endpoint: `PATCH /open-apis/task/v2/tasks/:task_guid` with `update_fields=["due"]`.

6. Create and manage tasklists.
- `create-tasklist`, `list-tasklists`, `update-tasklist-name`, `delete-tasklist`, `add-tasklist-member`, `remove-tasklist-member`.
- Endpoints under `/open-apis/task/v2/tasklists`.

7. Must-have extra operations.
- `get-task`, `update-task-summary`, `delete-task`, `remove-task-member`.
- Keep `client_token` for idempotent writes where possible.

## Quick Commands

```bash
# show help
/home/codexbot/alice/skills/feishu-tasks/scripts/feishu-task-v2.sh help

# list my tasks (requires FEISHU_USER_ACCESS_TOKEN)
/home/codexbot/alice/skills/feishu-tasks/scripts/feishu-task-v2.sh list-my-tasks

# create task
/home/codexbot/alice/skills/feishu-tasks/scripts/feishu-task-v2.sh create-task "准备周会材料" "周三前完成"

# set deadline (timestamp in ms)
/home/codexbot/alice/skills/feishu-tasks/scripts/feishu-task-v2.sh set-deadline <task_guid> 1767225600000 false
```

## References

- Read `references/task-v2-api-map.md` for API and SDK mapping.
