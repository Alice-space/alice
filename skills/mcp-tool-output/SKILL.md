---
name: mcp-tool-output
description: Define the Alice embedded MCP submission contract for local-agent executions. Use when another Alice runtime skill must finish by calling `submit_promotion_decision`, `submit_direct_answer`, or `submit_tool_call` instead of returning free-form JSON.
---

# MCP Tool Output

Consume the structured payload inside `<alice-execution-request>`.

## Workflow

1. Read `operation`, `input`, and `constraints`.
2. Honor `constraints.read_only`; do not invent write actions when read-only is true.
3. Use the embedded Alice MCP tools for terminal output instead of emitting JSON code blocks.

## Terminal tools

- Use `submit_promotion_decision` for routing/classification results.
- Use `submit_direct_answer` for user-facing direct answers.
- Use `submit_tool_call` only when another skill explicitly requires a tool or MCP invocation before the final result.

## Guardrails

- End each execution with one terminal MCP submission.
- Keep tool arguments complete and type-correct.
- Do not print fake tool payloads in plain text.
- If the runtime does not expose the embedded MCP tools, fall back to concise plain text.
