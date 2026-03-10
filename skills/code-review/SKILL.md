---
name: code-review
description: Generate structured review outcomes for patches and PR evidence. Use when workflow needs reviewer judgments, failure reasons, and required follow-up changes.
---

# Code Review

Review against requirements, not style preferences.

## Workflow

1. Validate correctness against plan and acceptance criteria.
2. Check regressions, edge cases, and test sufficiency.
3. Classify findings by severity and confidence.
4. Return structured `review_result` with pass/fail and required actions.

## Guardrails

- Separate blocking defects from optional improvements.
- Cite concrete evidence (file, behavior, failing path).
- Avoid unverifiable claims.

