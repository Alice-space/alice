---
name: alice-codebase-onboarding
description: Understand, diagnose, and self-update an Alice Feishu-to-Codex connector repository. Use when asked to trace runtime flow, verify prompt or bundled-skill wiring, inspect user-level systemd health, or run the canonical self-update path for the deployed connector.
---

# Alice Codebase Onboarding

Use this skill when the task is about the Alice repository or deployed runtime itself, not about normal chat operations inside Alice.

## Defaults

- `ALICE_REPO`: target repo path. If unset, default to `$HOME/alice`.
- `ALICE_HOME`: runtime home. If unset, default to `$HOME/.alice`.
- `CODEX_HOME`: Codex home. If unset, default to `${ALICE_HOME:-$HOME/.alice}/.codex`.
- `ALICE_SERVICE_NAME`: user service name. If unset, default to `alice-codex-connector.service`.

## Workflow

1. Identify task mode first.
- `flow_reading`: explain architecture, prompt flow, runtime CLI, or skill wiring.
- `runtime_triage`: collect deployment/runtime evidence before proposing fixes.
- `self_update`: verify repo state, then run the canonical updater and inspect the sync snapshot.

2. For flow reading, start narrow.
- Read `docs/feishu-message-flow.zh-CN.md` and `docs/architecture.zh-CN.md` first when the question is architectural.
- Then load `references/codebase-map.md`.
- Open code entry points only as needed:
  - `cmd/connector/root.go`
  - `cmd/connector/runtime_root.go`
  - `internal/bootstrap/connector_runtime_builder.go`
  - `internal/connector/app.go`
  - `internal/connector/processor.go`
  - `internal/prompting/*.go`
  - `internal/runtimeapi/*.go`

3. For runtime triage, collect evidence first.
- Run:
  - `scripts/check_alice_runtime.sh`
  - Optional: `scripts/check_alice_runtime.sh --journal 200`
- If service mode is enabled, inspect logs:
  - `journalctl --user-unit ${ALICE_SERVICE_NAME:-alice-codex-connector.service} -n 200 --no-pager`
  - `journalctl --user-unit ${ALICE_SERVICE_NAME:-alice-codex-connector.service} --since "30 min ago" --no-pager`
  - Fallback: `journalctl --user -u ${ALICE_SERVICE_NAME:-alice-codex-connector.service} ...`

4. For self-update, use exactly one updater path.
- Canonical repo command:
  - `$ALICE_REPO/scripts/update-self-and-sync-skill.sh`
- Skill wrapper command:
  - `$CODEX_HOME/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`
- The wrapper must dispatch to the repo script; do not replace this with ad-hoc `git pull` + manual build + manual restart unless the user explicitly asks.
- Before updating, make sure intended repo changes are already committed and pushed.
- After updating, inspect `${CODEX_HOME:-${ALICE_HOME:-$HOME/.alice}/.codex}/state/alice/sync-state.md`.

5. Remember the environment split.
- Runtime skills (`alice-message`, `alice-memory`, `alice-scheduler`) resolve the connector binary via `ALICE_RUNTIME_BIN`, then `${ALICE_HOME:-$HOME/.alice}/bin/alice`, then `PATH`.
- Normal runtime-skill execution does not require Go.
- The canonical self-update path does require `git` and `go` on the target host because it rebuilds the runtime binary.

6. Load references on demand.
- Architecture/runtime chain: `references/codebase-map.md`
- Build/deploy/self-update runbook: `references/deploy-runbook.md`
- Sync snapshot layout and fields: `references/sync-state.md`

7. Output in this order.
- Current state: running/missing/broken evidence.
- Relevant flow or update path: what code path or deployment path actually applies.
- Exact commands: what you ran or what should be run next.
- Risks/blockers: config mismatch, auth state, missing Go toolchain, service restart gaps, skill/repo drift.

## Guardrails

- Treat `${ALICE_HOME:-$HOME/.alice}/config.yaml` secrets as sensitive; never print raw secret values.
- Systemd is optional; do not assume `systemctl --user` exists.
- If Codex auth state is unknown, verify:
  - `HOME=$HOME CODEX_HOME=${CODEX_HOME:-${ALICE_HOME:-$HOME/.alice}/.codex} codex login status`
- Keep conclusions tied to command output and file contents.
- If the host lacks `go`, state clearly that self-update cannot complete there; do not pretend the updater succeeded.
