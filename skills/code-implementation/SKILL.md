---
name: code-implementation
description: Produce constrained candidate patches and implementation notes within explicit write scope. Use in code steps where workflow already authorized tools, MCP domains, and budget limits.
---

# Code Implementation

Implement safely inside the granted scope.

## Workflow

1. Confirm write scope, allowed tools, and deadline.
2. Apply smallest patch set that satisfies acceptance criteria.
3. Run targeted checks and tests.
4. Return `candidate_patch` and `test_notes`.

## Guardrails

- Do not edit outside declared write scope.
- Do not perform external side effects directly.
- Stop and report blockers instead of forcing risky edits.

