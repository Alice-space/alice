# Contributing

Contributions are welcome. This guide covers workflow, standards, and review expectations for all contributors (human and AI).

**中文版见下方。**

## 1. Branch and Change Scope

- Base daily work on latest `dev`. Submit PRs to `dev`.
- `main` only accepts merge commits from `dev`.
- Branch naming: `feat/*`, `fix/*`, `docs/*`, `chore/*`.
- **One commit, one goal.** Don't mix unrelated changes in one commit.

## 2. Commit Message Format

Conventional Commits are enforced by a `commit-msg` hook:

```
type(scope): subject
type: subject
```

Allowed types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.

Examples:
- `feat(connector): support codex resume thread`
- `fix: keep proxy env for codex exec`
- `docs: add configuration reference`

## 3. Pre-Commit Checks

First-time setup:

```bash
make precommit-install
```

Every commit must pass:

```bash
make check
```

`make check` runs in order:

| Gate | Command |
|------|---------|
| Secret scan | `secret-check` |
| Shell syntax | `script-check` |
| Format check | `fmt-check` (gofmt) |
| Vet | `go vet ./...` |
| Unit tests | `go test ./...` |
| Race tests | `go test -race ./internal/connector` |

**Do not commit until `make check` passes with zero failures.**

For cross-cutting or concurrency changes, also run:

```bash
go test -race ./...
```

If formatting fails, run `make fmt` first.

## 4. Code Rules

- Use `gofmt` for all Go code.
- Files over 500 lines must be split in the same change (prevent mega-files).
- New or changed behavior must include/update tests.
- Never log sensitive information (app secrets, tokens, user content).
- CLI flag changes may be breaking but must be clearly documented with migration instructions.

## 5. Configuration Change Rules

- This project uses YAML config (`${ALICE_HOME}/config.yaml`), not environment variables as primary config.
- New config keys require updates to:
  - `config.example.yaml`
  - `internal/config` (defaults and validation)
  - Documentation (both English README and docs site)
- Config keys affecting session/memory behavior (e.g., `idle_summary_hours`) must have corresponding tests.

## 6. Documentation Sync

Any user-visible change (commands, flags, config, behavior) must sync:
- `README.md`
- `README.zh-CN.md`
- `book/src/` (docs site)

Keep English and Chinese docs consistent.

## 7. Merge Checklist

- [ ] `make check` passes locally
- [ ] Key path runs (at minimum one start-up test):
  ```bash
  go run ./cmd/connector --feishu-websocket
  ```
- [ ] Documentation synced with changes
- [ ] No unrelated files or debug content included

## 8. Runtime Isolation Rules

When debugging or testing with isolated runtimes:
- Use explicit startup mode: `--feishu-websocket` or `--runtime-only`
- `alice-headless` must use `--runtime-only` only
- Never connect isolated debug runtimes to the real Feishu WebSocket
- After startup, verify logs show `runtime-only mode enabled; Feishu websocket connector disabled`
- If logs show `feishu-codex connector started` for an isolated runtime, stop it immediately

---

# 贡献指南

欢迎贡献。本指南涵盖所有贡献者（人类和 AI）的工作流、标准和评审要求。

## 1. 分支与变更范围

- 日常开发基于最新 `dev` 分支，提交到 `dev`。
- `main` 只接受 `dev -> main` 的合并提交。
- 分支命名：`feat/*`、`fix/*`、`docs/*`、`chore/*`。
- **每次提交只做一件事**，避免无关改动混在一起。

## 2. 提交信息规范

强制使用 Conventional Commits，由 `commit-msg` hook 校验：

```
type(scope): subject
type: subject
```

允许的 type：`feat`、`fix`、`docs`、`style`、`refactor`、`perf`、`test`、`build`、`ci`、`chore`、`revert`。

示例：
- `feat(connector): support codex resume thread`
- `fix: keep proxy env for codex exec`

## 3. 提交前必须检查

首次执行：

```bash
make precommit-install
```

每次提交前必须通过：

```bash
make check
```

`make check` 包含：secret-check → script-check → fmt-check → go vet → go test → go test -race

**未通过 `make check` 不得提交。**

格式不通过时先执行 `make fmt`。

## 4. 代码规则

- 统一使用 `gofmt` 格式化代码。
- 单文件超过 500 行必须拆分（防止巨型文件增长）。
- 新增或修改行为必须补充/更新测试。
- 不要在日志中输出敏感信息。
- CLI 参数变更允许破坏性调整，但必须明确说明。

## 5. 配置变更规则

- 使用 YAML 配置文件，不用环境变量作主配置入口。
- 新增配置项必须同步更新：`config.example.yaml`、`internal/config`、文档。
- 会话相关配置项需补充对应测试。

## 6. 文档同步规则

任何用户可见变更必须同步更新文档，保持中英文一致。

## 7. 合并前自检清单

- [ ] 本地 `make check` 通过
- [ ] 至少验证一次启动：`go run ./cmd/connector --feishu-websocket`
- [ ] 文档已同步更新
- [ ] 不包含无关文件或调试内容

## 8. 运行时隔离规则

调试或测试隔离 runtime 时：
- 使用显式启动模式
- `alice-headless` 只能用 `--runtime-only`
- 不允许隔离 runtime 连接真实飞书 WebSocket
- 启动后确认日志显示 `runtime-only mode enabled; Feishu websocket connector disabled`
