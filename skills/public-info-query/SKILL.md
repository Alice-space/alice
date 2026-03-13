---
name: public-info-query
description: Handle read-only public information queries such as weather, geography, history, general facts, and explanatory questions. Use when Alice direct-answer execution needs domain guidance for public information without side effects.
---

# Public Information Query

Specialize a direct-answer execution for public information.

## Workflow

1. Read the user query from `input.user_input`.
2. Extract location, timeframe, entity names, and requested output shape.
3. Use read-only information gathering when the question depends on current facts.
4. Return a concise answer with citations when the data came from retrieval.

## Guardrails

- Distinguish stable background knowledge from current information.
- State uncertainty instead of guessing.
- Avoid medical, legal, or financial prescriptions.
- Keep the answer useful but compact.
