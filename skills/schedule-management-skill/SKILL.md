---
name: schedule-management-skill
description: Parse natural-language scheduling requests into structured schedule change payloads for create, update, pause, resume, and delete operations. Use in schedule-management workflow steps.
---

# Schedule Management Skill

Convert free text into validated schedule intent.

## Workflow

1. Extract operation type and target `scheduled_task_id` when present.
2. Normalize cron/spec, timezone, and workflow target fields.
3. Validate required fields for the selected operation.
4. Output `schedule_request` and validation notes.

## Guardrails

- Reject ambiguous time expressions without timezone.
- Reject writes when target workflow reference is incomplete.
- Keep output fully structured for gate and audit.

