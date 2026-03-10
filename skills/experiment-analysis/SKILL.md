---
name: experiment-analysis
description: Analyze evaluation outputs, training logs, and metric deltas to recommend continue, rewind, or stop decisions. Use after evaluate steps in research workflows.
---

# Experiment Analysis

Assess outcomes with reproducible evidence.

## Workflow

1. Collect run metadata: code version, dataset version, eval config.
2. Compare metrics against baseline and threshold.
3. Detect data-quality or environment anomalies.
4. Return structured recommendation: pass, iterate, or recover.

## Guardrails

- Do not ignore failed or partial runs.
- Keep recommendations tied to observed metrics.
- Mark confidence and uncertainty explicitly.

