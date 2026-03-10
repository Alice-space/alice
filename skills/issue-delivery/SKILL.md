---
name: issue-delivery
description: "Execute issue-delivery workflow work: triage, plan, code, review, optional merge, and report with strict artifact contracts. Use when a durable task is bound to issue-delivery."
---

# Issue Delivery

Drive code delivery with explicit step outputs.

## Step Guidance

1. `triage`: produce `task_brief` with scope and constraints.
2. `plan`: produce executable plan and acceptance criteria.
3. `code`: produce `candidate_patch` and `test_notes`.
4. `review`: produce structured `review_result`.
5. `merge/report`: execute only if workflow includes these steps and gates are satisfied.

## Guardrails

- Keep all external writes on outbox + MCP path.
- Respect gate outcomes; never bypass approval with local assumptions.
- Route scope changes back to `plan` when requirements drift.
