---
name: repo-understanding
description: Read repository context and produce structured evidence for issue, PR, and code-change decisions. Use when preparing triage, planning, or review input for durable workflows.
---

# Repo Understanding

Produce evidence first, conclusions second.

## Workflow

1. Locate target files/modules from issue or PR context.
2. Extract relevant symbols, call paths, and behavior constraints.
3. Record test impact and risk hotspots.
4. Return structured notes with file pointers and confidence.

## Guardrails

- Avoid speculative architecture claims without code evidence.
- Prefer precise references over broad summaries.
- Separate observed facts from inferred hypotheses.

