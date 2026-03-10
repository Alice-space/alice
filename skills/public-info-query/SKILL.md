---
name: public-info-query
description: Answer low-risk public information requests with read-only lookups. Use for weather and public facts that should stay in EphemeralRequest and avoid durable workflow promotion.
---

# Public Info Query

Answer quickly and with bounded scope.

## Workflow

1. Parse target, time window, and output style.
2. Confirm request is read-only and one-shot.
3. Query minimal external sources.
4. Return a short, source-grounded summary.

## Guardrails

- Do not perform write actions.
- Do not request promote unless governance conditions are triggered.
- Mark uncertainty instead of fabricating missing facts.

