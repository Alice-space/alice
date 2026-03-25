# AGENTS Collaboration Guide (Alice)

This file is the canonical collaboration and execution standard for contributors in this repository.

## Scope

This standard applies to all human and AI contributors in this repository.

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

If task involves runtime/deploy troubleshooting, collect facts first:

```bash
# default installer unit; if installed with --service, replace alice.service accordingly
journalctl --user-unit alice.service -n 200 --no-pager
journalctl --user-unit alice.service --since "30 min ago" --no-pager
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
- Default target branch is `dev` (not `main`).
- For GitHub release work, first check the repo-local skill at `.agents/skills/github/SKILL.md`.
- If that skill exists, follow its `scripts/github.sh` workflow for release promotion and post-release self-update.
- If the repo-local GitHub skill is unavailable, fall back to `gh` with the same `dev -> main` / merge-commit release policy.
- Suggested flow:

```bash
git status
git add <intended-files>
git commit -m "<clear-message>"
git push github dev
git push origin dev
```

## 7. Definition of Done

A change is done only when all are true:

- Code implemented and formatted.
- Mandatory checks passed.
- Docs/config/tests synced with behavior.
- Commit pushed.
- (If deploy task) service health re-verified with evidence.

## 8. Branch And Release Workflow (Mandatory)

- Default development branch: `dev`.
- Do not push directly to `main`.
- Only merge `dev -> main`.
- PRs to `main` must come from `dev`.
- Use merge-commit for `dev -> main` (do not squash/rebase for release path).

## 9. CI Behavior Summary

- `dev` push:
  - run quality gate
  - build dev binaries
  - update prerelease `dev-latest`
- `main` merge from `dev`:
  - run quality gate
  - auto-create next `vX.Y.Z` tag
  - build and publish GitHub Release
- manual `v*` tags:
  - still trigger release workflow

## 10. Runtime Home Defaults By Build Channel

- release build default: `~/.alice`
- dev build default: `~/.alice-dev`
- explicit `ALICE_HOME` / `--alice-home` overrides both defaults
