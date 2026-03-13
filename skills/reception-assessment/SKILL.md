---
name: reception-assessment
description: Classify inbound Alice requests into PromotionDecision fields using route metadata, side-effect risk, and workflow hints. Use when Alice reception receives new user input, webhook events, or control-plane requests and must decide direct answer versus workflow promotion.
---

# Reception Assessment

Classify first. Do not answer the user here.

## Request contract

- Expect `operation = reception_assessment`.
- Read route facts from `input.event`, `input.required_refs`, `input.route_snapshot`, and `input.route_target`.
- Treat `input.event.source_ref` as the primary user request text.

## Decision rules

- Choose `direct_query` for read-only factual questions, status lookups, and lightweight explanations with no durable side effects.
- Choose `issue_delivery` for repo changes, bug fixes, PR/issue handling, or any request that implies code delivery.
- Choose `research_exploration` for experiments, evaluations, comparisons, or iterative investigation that needs budget or recovery controls.
- Choose `schedule_management` for creating, editing, pausing, or deleting scheduled tasks.
- Choose `workflow_management` for modifying workflow definitions, workflow manifests, or control-plane workflow objects.

## Field expectations

- Set `risk_level` conservatively.
- Set `external_write`, `create_persistent_object`, `async`, `multi_step`, `multi_agent`, `approval_required`, `budget_required`, and `recovery_required` from the real execution shape, not from user wording alone.
- Populate `proposed_workflow_ids` when promotion is needed:
  `issue-delivery`, `research-exploration`, `schedule-management`, `workflow-management`.
- Add short `reason_codes` that explain the classification.
- Submit the result through `submit_promotion_decision`.

## Guardrails

- Do not guess missing route-critical references.
- Prefer escalation when confidence is low or the request crosses intent boundaries.
- Do not emit a final user answer from this skill.
