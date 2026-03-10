---
name: workflow-management-skill
description: Translate natural-language workflow change requests into structured workflow_change_request artifacts, candidate diffs, and impact summaries. Use in workflow-management workflow steps.
---

# Workflow Management Skill

Model workflow edits as controlled change requests.

## Workflow

1. Identify target workflow and immutable boundaries.
2. Convert request to structured change spec.
3. Generate candidate diff summary (steps, gates, schemas, routes).
4. Produce compatibility and impact notes for approval.

## Guardrails

- Refuse undefined target workflow or revision context.
- Refuse direct publish without validation evidence.
- Preserve explicit old/new behavior mapping.

