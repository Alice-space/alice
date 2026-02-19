# Repository Contribution Rules

Use this reference for `/Users/alice/Developer/alicespace`.

## Mandatory Quality Gates

- Run `go test ./...`.
- Run `make check` before commit.
- `make check` includes:
  - `make fmt-check`
  - `go vet ./...`
  - `go test ./...`

## Branch and Commit

- Base work on latest `master`.
- Work on a dedicated branch.
- For Codex-driven contributions, use `codex/*`.
- Use Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`).

## Code and Test Expectations

- Format Go code with `gofmt`.
- Add or update tests when behavior changes.
- Avoid logging sensitive values.

## Config Change Requirements

When adding or changing config keys, update all of:

- `/Users/alice/Developer/alicespace/config.example.yaml`
- `/Users/alice/Developer/alicespace/internal/config/config.go`
- `/Users/alice/Developer/alicespace/README.md`
- `/Users/alice/Developer/alicespace/README.zh-CN.md`

## Completion Definition

A contribution is complete only when:

1. Code change is implemented.
2. Required tests pass.
3. `make check` passes.
4. Changes are committed on a non-master branch.
5. Merge is left for manual action.
