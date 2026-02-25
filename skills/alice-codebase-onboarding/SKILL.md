---
name: alice-codebase-onboarding
description: Understand and operate an Alice Feishu-to-Codex connector repository. Use when asked to analyze architecture, trace runtime flow, perform self-update/deployment checks, verify user-level systemd health, or troubleshoot startup and operational issues.
---

# Alice Codebase Onboarding

Use this skill to work safely on an Alice repository with reproducible runtime/deployment evidence.

## Defaults

- `ALICE_REPO`: target repo path. If unset, default to `$HOME/alice`.
- `CODEX_HOME`: Codex home. If unset, default to `$HOME/.codex`.

## Workflow

1. Identify task mode first.
- `code_change`: implement in repo, then test, then commit/push.
- `self_update`: commit/push first, then run unified updater.
- `runtime_triage`: collect facts before proposing fixes.

2. For code changes, inspect focused files only.
- Start at:
  - `cmd/connector/main.go`
  - `internal/bootstrap/connector_runtime.go`
  - `internal/config/config.go`
  - `internal/connector/app.go`
  - `internal/connector/processor.go`
  - `internal/llm/codex/codex.go`
  - `internal/memory/memory.go`
- Expand to tests only when needed.

3. For self-update, always use the unified updater.
- Canonical command (repo version):
  - `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- Skill wrapper command:
  - `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`
- Do not replace this with ad-hoc `git pull` + manual restart.

4. For runtime/deploy diagnosis, collect evidence first.
- Run:
  - `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/check_alice_runtime.sh`
  - Optional: `--journal 200`
- Then inspect logs:
  - `journalctl --user-unit alice-codex-connector.service -n 200 --no-pager`
  - `journalctl --user-unit alice-codex-connector.service --since "30 min ago" --no-pager`
  - Fallback: `journalctl --user -u alice-codex-connector.service ...`

5. Load references on demand.
- Architecture/runtime chain: `references/codebase-map.md`
- Build/deploy/self-update runbook: `references/deploy-runbook.md`
- Latest sync snapshot path policy: `references/sync-state.md`

6. Output in this order.
- Current state: running/missing/broken evidence.
- Runtime flow: Feishu event -> queue -> processor -> Codex -> reply/memory.
- Action plan: exact commands.
- Risks: config mismatch, auth state, service restart gaps, skill/repo drift.

## Guardrails

- Treat `<repo>/config.yaml` secrets as sensitive; never print raw secret values.
- Prefer `systemctl --user` operations.
- If Codex auth state is unknown, verify:
  - `HOME=$HOME CODEX_HOME=${CODEX_HOME:-$HOME/.codex} codex login status`
- Keep conclusions tied to command output and file contents.
- Keep code files under 500 lines; split by responsibility when needed.
