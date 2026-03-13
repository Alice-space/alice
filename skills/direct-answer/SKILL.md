---
name: direct-answer
description: Answer low-risk Alice requests directly from structured execution payloads. Use when a request has already been classified as a read-only/direct-answer path and the agent must produce the final response through Alice's embedded MCP tools.
---

# Direct Answer

Respond to the user request described in `<alice-execution-request>`.

## Request contract

- Expect `operation = direct_answer`.
- Read the user question from `input.user_input`.
- Use `input.intent_kind` and optional `input.context` for domain-specific hints.
- Respect `constraints.read_only`.

## Response rules

- Answer in the user's language.
- Stay concise, accurate, and explicit about uncertainty.
- Use read-only tools when current or external facts are required.
- Prefer citations when the answer depends on retrieved data.
- Submit the final result through `submit_direct_answer`.

## Guardrails

- Do not modify files or external systems.
- Do not claim real-time certainty without evidence.
- Do not return JSON code blocks as the final answer.
