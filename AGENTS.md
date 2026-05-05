# AGENTS Collaboration Guide (Alice)

This file is the canonical collaboration and execution standard for contributors in this repository.

## Scope

This standard applies to all human and AI contributors in this repository.

## Repo-Local Skills First

- When working inside the Alice repository, treat repo-local skills under `.agents/skills/...` as the default execution path for repository operations that already have a bundled workflow.
- In particular, if the task is to update Alice itself, promote a release, merge `dev -> main`, wait for GitHub Release artifacts, or run the post-release self-update flow, use the repo-local GitHub skill first:
  - read `.agents/skills/github/SKILL.md`
  - run `.agents/skills/github/scripts/github.sh`
- Do not bypass the repo-local skill with ad-hoc `gh`, installer, or release commands unless the skill is missing or clearly blocked.

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

If task involves isolated debugging or temporary rerun runtimes:

- Connector startup mode is explicit: use `--feishu-websocket` for the real Feishu connector, or `--runtime-only` for local runtime/API-only execution.
- Any temporary `alice-headless` runtime must be started with explicit `--runtime-only`.
- Do not allow isolated test runtimes to connect to the real Feishu websocket.
- After startup, verify logs show `runtime-only mode enabled; Feishu websocket connector disabled`.
- If logs show `feishu-codex connector started (long connection mode)` for an isolated runtime, stop it immediately and treat that as an incident.

## 4. Mandatory Validation Gates

Every commit MUST pass `make check` before being made:

```bash
make check
```

`make check` runs the following gates in order:
1. `secret-check` — scans for secrets/credentials in staged files
2. `script-check` — validates shell scripts with `bash -n`
3. `fmt-check` — verifies `gofmt -l` returns nothing on all `.go` files
4. `vet` — runs `go vet ./...`
5. `test` — runs `go test ./...`
6. `race` — runs `go test -race ./internal/connector`

For cross-cutting or concurrency-related changes, also run full race tests:

```bash
go test -race ./...
```

**Do not commit until `make check` passes with zero failures.** If a pre-existing flaky test fails, fix or isolate it before proceeding.

## 5. Docs and Config Consistency

When behavior/config/interface changes, update in the same change set:

- `README.md`
- `README.zh-CN.md`
- `config.example.yaml` (if config shape/default changed)
- related tests (`*_test.go`)

Implementation and docs must describe the same behavior.

### 5.1 Book Documentation (Mandatory)

**Any new feature, configuration key, CLI command, or behavior change MUST include corresponding documentation in `book/src/` — both English and Chinese (`zh-CN/`).**

When adding a new feature, the minimum doc update:

- If it's a user-facing feature → add to `how-to/` (both EN + zh-CN)
- If it introduces a new concept → add to `explanation/` (both EN + zh-CN)
- If it adds/changes config keys → update `reference/configuration.md` (both EN + zh-CN)
- If it adds/changes CLI commands → update `reference/cli.md` (both EN + zh-CN)
- If it adds/changes runtime API → update `reference/runtime-api.md` (both EN + zh-CN)
- If it changes architecture → update `development/architecture.md` (both EN + zh-CN)

The book is the canonical documentation. `README.md` / `README.zh-CN.md` serve as landing pages that link into the book. Features without book docs are considered incomplete — mark the PR as draft or add the docs before merging.

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
