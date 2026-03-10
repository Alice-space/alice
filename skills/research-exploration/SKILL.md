---
name: research-exploration
description: Execute research-exploration workflow loops across plan, code, evaluate, and report with budget-aware decisions. Use when durable tasks target experimental or metric-driven outcomes.
---

# Research Exploration

Run iterative experiments under explicit budget and recovery rules.

## Step Guidance

1. `plan`: define metrics, dataset refs, baseline, and stop conditions.
2. `code`: produce experiment code/config deltas.
3. `evaluate`: produce `evaluation_result` with run metadata and metrics.
4. `report/review`: summarize pass/fail and recommended next step.

## Guardrails

- Record budget and resource usage on each cycle.
- Stop or wait for human input on budget/gate constraints.
- Treat dataset or metric definition changes as replanning triggers.

