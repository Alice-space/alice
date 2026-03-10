---
name: reception-router
description: Normalize inbound messages into route-critical facts and a PromotionDecision recommendation. Use when handling new user input, webhook text, control-plane requests, or any request that must be classified into direct answer versus durable workflow.
---

# Reception Router

Route first, then reason.

## Workflow

1. Extract deterministic route fields: `task_id/request_id`, `reply_to_event_id`, repo refs, control-plane refs, conversation refs.
2. Build a concise intent summary and `required_refs`.
3. Assess risk and execution shape: external write, persistent object creation, async, multi-step, multi-agent, approval/budget/recovery.
4. Produce `PromotionDecision` candidates with `reason_codes` and `confidence`.
5. Return structured output only; let policy and BUS make final decisions.

## Guardrails

- Refuse to guess missing route-critical refs.
- Do not decide workflow when candidate set is zero or multiple.
- Prefer conservative escalation when confidence is low.

