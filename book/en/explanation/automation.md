# Automation Subsystem

Alice's automation engine schedules and executes recurring tasks, workflows, and system maintenance.

## Architecture

The automation subsystem (`internal/automation/`) uses a tick-based execution model with persistent storage.

```
Automation Engine
  ├─ Tick Scheduler (periodic loop)
  │   ├─ Claim due tasks
  │   ├─ Execute tasks (send_text / run_llm / run_workflow)
  │   └─ Handle completion / failures
  ├─ System Task Scheduler
  │   ├─ Session state flush
  │   └─ Campaign reconcile
  ├─ Watchdog
  │   └─ Alert on overdue or stuck tasks
  └─ Store (bbolt)
      └─ Task persistence
```

## Task Model

### Scope

A task's **scope** defines where it executes:

| Scope | Description |
|-------|-------------|
| `user` | Scoped to a specific user (DM context) |
| `chat` | Scoped to a specific group chat |

### Actions

| Action | Description |
|--------|-------------|
| `send_text` | Send a predetermined text message to the scope |
| `run_llm` | Run an LLM call with a specified prompt in the scope |
| `run_workflow` | Run a multi-step workflow combining LLM calls and actions |

### Scheduling

Tasks can be scheduled in two ways:

- **Cron expressions**: `"0 9 * * *"` — runs at 9 AM daily
- **One-shot timestamps**: ISO 8601 — runs once at the specified time

### Task Lifecycle

```
Created → Active → Claimed → Executing → Completed
                                  ↓
                              Failed → Active (retry) / Cancelled
```

- Due tasks are **claimed** on a periodic tick (one claim per tick)
- Claimed tasks are **executed** in the scope's conversation context
- **Completed** tasks with cron expressions are re-scheduled for the next occurrence
- **Failed** tasks may be retried or cancelled
- **Cancelled** tasks are deleted or marked inactive

## Execution Model

When a task executes:

1. The engine acquires the session gate for the task's scope
2. The task inherits the same conversation context as interactive runs:
   - Same workspace directory
   - Same LLM profile and permissions
   - Same environment variables
3. For `run_llm` and `run_workflow`, the task's prompt is sent to the LLM backend
4. Replies are dispatched to the task's scope (chat or user DM)

> User messages can interrupt automation tasks that have acquired the session gate.

## System Tasks

Alice registers built-in system tasks during bootstrap:

| Task | Interval | Purpose |
|------|----------|---------|
| Session state flush | Periodic | Persist in-memory session state to `session_state.json` |
| Campaign reconcile | Periodic | Sync campaign repository state |

## Watchdog

The watchdog monitors automation tasks for anomalies:

- **Overdue tasks**: Tasks past their scheduled time that haven't been claimed
- **Stuck tasks**: Tasks that have been executing for too long

When the watchdog detects an issue, it can:
- Log a warning
- Send an alert message to a configured chat
- Force-cancel the stuck task

## Storage

Tasks are persisted in a local bbolt database:

```
~/.alice/bots/<bot_id>/run/connector/automation.db
```

This survives process restarts. The store supports:
- CRUD operations on tasks
- Querying by scope, status, and due time
- Atomic claim-and-update to prevent duplicate execution

## Managing Tasks

### Via Runtime API

```bash
alice runtime automation create '{
  "scope_type": "chat",
  "scope_id": "oc_xxxxxxxxxxxxx",
  "action": "send_text",
  "text": "Daily standup reminder!",
  "cron": "0 10 * * 1-5"
}'
```

### Via Bundled Skills

The `alice-scheduler` skill lets users create and manage tasks directly from Feishu conversations.

See the [Runtime API Reference](../reference/runtime-api.md) for the complete task management endpoints.
