---
name: repo-contribution
description: "Execute a complete contribution workflow for /Users/alice/Developer/alicespace. Use when the user asks to implement a feature/fix/refactor/docs change in this repository end-to-end: create a new codex/* branch, implement scoped changes, add or update tests when behavior changes, run go test ./... and make check, commit with Conventional Commits, and stop for manual merge."
---

# Repo Contribution

Complete exactly one scoped contribution and enforce mandatory quality gates before commit.

## Workflow

1. Confirm scope and existing workspace state.
2. Create or switch to a dedicated branch named `codex/<short-task>`.
3. Implement the requested changes with minimal unrelated edits.
4. Add or update tests when behavior changes.
5. Run verification gates:
   - `go test ./...`
   - `make check`
   - `./.codex/skills/repo-contribution/scripts/verify_contribution.sh`
6. Stage only intended files.
7. Commit with Conventional Commit format.
8. Stop after commit and wait for manual merge.

## Branch Rules

- Never commit directly on `master`.
- Use `codex/*` branch names.
- Do not rewrite or reset unrelated user changes.

## Implementation Rules

- Keep each contribution focused on one task.
- Follow existing code style and conventions.
- Update docs when user-visible behavior changes.
- If config keys are added or changed, update all required config/doc files listed in `references/repo-rules.md`.

## Validation Rules

- Treat `make check` as mandatory.
- If tests fail, fix code or tests and rerun all checks until green.
- Never claim completion when checks are skipped.

## Commit Rules

- Use `feat:`, `fix:`, `docs:`, `chore:`, `test:`, or `refactor:` prefixes.
- Keep commit message specific to this contribution.
- Include only related files in the commit.

## Reporting Template

Report with:
- Branch name
- Commit hash
- Commands run
- Test/check results
- Note that merge is pending manual action

## Resources

- Read `references/repo-rules.md` when deciding repo-specific requirements.
- Run `scripts/verify_contribution.sh` as the final local gate before commit.
