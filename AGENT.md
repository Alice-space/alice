# AGENT Execution Standard (Alice)

Scope: this standard applies to all human/AI contributors in this repository (`/home/codexbot/alice`).

## 1. Decision Discipline

- Facts first, then inference, then conclusion.
- Any diagnosis or proposal must explicitly separate:
  - `Facts`: command outputs, logs, file references.
  - `Inference`: interpretation based on facts.
  - `Decision`: the action to take.
- Do not make deployment/runtime claims without evidence.

## 2. Change Scope Rules

- One commit, one primary goal.
- Split refactor and behavior change:
  - First commit: behavior-preserving refactor.
  - Second commit: behavior change (if needed).
- Do not mix unrelated files in one commit.

## 3. Pre-Change Checklist

Before coding, define:

- Objective and non-objective.
- Expected behavior invariants (what must not change).
- Risks and rollback path.
- Acceptance criteria (tests/build/runtime checks).

If task involves runtime/deploy/self-troubleshooting, collect facts first:

```bash
/home/codexbot/.codex/skills/alice-codebase-onboarding/scripts/check_alice_runtime.sh
journalctl --user-unit alice-codex-connector.service -n 200 --no-pager
journalctl --user-unit alice-codex-connector.service --since "30 min ago" --no-pager
```

## 4. Mandatory Validation Gates

All required checks must pass before commit:

```bash
gofmt -w <changed-files>
go test ./...
go vet ./...
go test -race ./internal/connector
```

Run full race tests for cross-cutting or concurrency-related changes:

```bash
go test -race ./...
```

No commit if any required check fails.

## 5. Docs and Config Consistency

When behavior/config/interface changes, update in the same change set:

- `README.md`
- `README.zh-CN.md`
- `config.example.yaml` (if config shape/default changed)
- related tests (`*_test.go`)

Implementation and docs must describe the same behavior.

## 6. Commit and Push Workflow

- Stage only intended files.
- Use clear commit messages (what changed + why).
- Suggested flow:

```bash
git -C /home/codexbot/alice status
git -C /home/codexbot/alice add <intended-files>
git -C /home/codexbot/alice commit -m "<clear-message>"
git -C /home/codexbot/alice push
```

## 7. Self-Update and Deployment Rule

For self-update, use unified updater only (do not ad-hoc pull/restart):

```bash
/home/codexbot/.codex/skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh
```

## 8. Definition of Done

A change is done only when all are true:

- Code implemented and formatted.
- Mandatory checks passed.
- Docs/config/tests synced with behavior.
- Commit pushed.
- (If deploy/self-update task) service health re-verified with evidence.
