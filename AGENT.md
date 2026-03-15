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
- Suggested flow:

```bash
git -C /home/codexbot/alice status
git -C /home/codexbot/alice add <intended-files>
git -C /home/codexbot/alice commit -m "<clear-message>"
git -C /home/codexbot/alice push github dev
git -C /home/codexbot/alice push origin dev
```

## 7. Definition of Done

A change is done only when all are true:

- Code implemented and formatted.
- Mandatory checks passed.
- Docs/config/tests synced with behavior.
- Commit pushed.
- (If deploy task) service health re-verified with evidence.

## 8. Dev/Main Branch and Release Rules

- `dev` is the default development branch for all routine changes.
- Direct push to `main` is forbidden by policy; use `dev -> main` only.
- Pull requests targeting `main` must come from branch `dev`.
- Merge to `main` should use merge-commit mode (not squash/rebase), because release workflow validates merge ancestry.
- On each `dev` push:
  - CI runs quality gate and builds dev binaries.
  - CI updates prerelease tag `dev-latest`.
- On each valid `dev -> main` merge:
  - CI runs quality gate.
  - CI computes next `vX.Y.Z` tag automatically.
  - CI builds release artifacts and creates GitHub Release.
- Manual `v*` tag push release remains supported for exceptional/manual scenarios.
