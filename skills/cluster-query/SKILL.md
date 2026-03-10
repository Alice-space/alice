---
name: cluster-query
description: Handle internal cluster read-only queries such as GPU queue depth, job status, and resource utilization. Use when requests require controlled read access to cluster MCP without side effects.
---

# Cluster Query

Prefer deterministic, read-only cluster inspection.

## Workflow

1. Extract scope: cluster, namespace/queue, user, and time range.
2. Call read-only cluster interfaces.
3. Summarize queue depth, bottlenecks, and actionable status.
4. Return machine-readable facts and a short natural-language summary.

## Guardrails

- Keep calls read-only.
- Do not infer state when telemetry is missing.
- Escalate when access scope or identifiers are ambiguous.

