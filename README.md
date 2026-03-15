# Alice Connector

[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

A Feishu long-connection connector for Codex / Claude / Kimi.

- Receives Feishu messages over WebSocket (`im.message.receive_v1`)
- Calls the selected LLM CLI backend
- Sends reply / progress / file-change feedback back to Feishu
- Supports memory, automation scheduling, and runtime APIs

## Highlights

- Standalone runtime: binary can run without repository checkout
- Embedded prompts + skills: prompts are bundled in binary; bundled skills are synced on startup
- Isolated runtime home: release builds default to `~/.alice`, dev builds default to `~/.alice-dev`
- Isolated Codex home: defaults to `${ALICE_HOME}/.codex`
- Dev/Main branch release pipeline with automatic tagging on main merge

## Runtime Layout

Release builds use this default layout (`~/.alice`):

- Config: `${ALICE_HOME:-~/.alice}/config.yaml`
- Binary: `${ALICE_HOME:-~/.alice}/bin/alice`
- Logs: `${ALICE_HOME:-~/.alice}/log/YYYY-MM-DD.log` (default)
- Runtime state: `${ALICE_HOME:-~/.alice}/memory/`
- Bundled skills: `${ALICE_HOME:-~/.alice}/.codex/skills/`

Dev builds use the same layout under `~/.alice-dev` by default.

## Requirements

- Go 1.25+ (source build only)
- `codex` CLI (or `claude` / `kimi`) installed and logged in
- Linux host with `systemd --user` (for one-line install script)
- Feishu app with:
  - bot capability
  - `im.message.receive_v1` subscription
  - required message permissions
  - long connection mode enabled

## Quick Start (Source Build)

```bash
mkdir -p ~/.alice
cp config.example.yaml ~/.alice/config.yaml
# edit ~/.alice/config.yaml

go mod tidy
go test ./...
go run ./cmd/connector
```

## One-Line Install / Update / Uninstall (Recommended)

Installer script (in this repo): [`scripts/alice-installer.sh`](./scripts/alice-installer.sh)

Install latest release (also works as update if run again):

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- install
```

Update to latest release (explicit action):

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- update
```

Install/update to a pinned version:

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- install --version vX.Y.Z
```

Install dev prerelease explicitly (default is stable release):

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- install --channel dev
```

`--channel dev` defaults to `~/.alice-dev` unless `--home` or `ALICE_HOME` is provided.

Uninstall (remove service + binary + `~/.alice`):

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- uninstall
```

Uninstall but keep runtime data:

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- uninstall --keep-data
```

What the installer does:

- Downloads stable GitHub Release assets by default (`~/.alice`), and uses dev prerelease channel only when `--channel dev` is provided (`~/.alice-dev`)
- Verifies release checksum against `SHA256SUMS` when available
- Initializes `${ALICE_HOME:-~/.alice}` directories and default `config.yaml` (if missing)
- Copies existing Codex auth (`auth.json`) into `${ALICE_HOME}/.codex/` when available
- Installs and manages `systemd --user` service (`alice.service` by default; override with `--service NAME`) for auto-restart
- Attempts to enable user linger so the service can stay alive after logout

After first install:

1. Edit `${ALICE_HOME:-~/.alice}/config.yaml` and set `feishu_app_id` + `feishu_app_secret`
2. Re-run the install command once to start/restart the service with your config

## Configuration

Required keys:

- `feishu_app_id`
- `feishu_app_secret`

Common optional keys:

- `llm_provider`: `codex` (default), `claude`, `kimi`
- `<provider>_command` / `<provider>_timeout_secs`
- `runtime_http_addr` / `runtime_http_token`
- `env`: extra env vars passed to backend process
- `alice_home`: runtime home directory (default `~/.alice`)
- `workspace_dir` / `memory_dir` / `prompt_dir`
- `trigger_mode`: `at` / `active` / `prefix`
- `trigger_prefix`
- automation cron scheduling uses OS timezone (`time.Local`)
- `log_file` (default `${ALICE_HOME}/log/YYYY-MM-DD.log`) and rotate options

Default process env behavior:

- Alice enforces `CODEX_HOME=${ALICE_HOME}/.codex` on startup
- The same `CODEX_HOME` is injected into Codex/Claude/Kimi subprocesses unless explicitly overridden in `env`

See [config.example.yaml](./config.example.yaml) for full schema.

## Branch And CI Policy

- Day-to-day commits go to `dev`.
- Pull requests into `main` are limited to `dev -> main` by workflow policy.
- Push to `main` must be a merge commit from `dev`; workflow enforces this.
- When `dev` is merged to `main`, CI runs quality gate, computes next `vX.Y.Z`, pushes tag, and creates GitHub Release.
- Manual `v*` tag pushes are still supported through [release-on-tag.yml](./.github/workflows/release-on-tag.yml).
- In GitHub settings, enable branch protection for `main` and disallow direct pushes for hard enforcement.

## Bundled Skills

Bundled skills are embedded into the binary (`skills/**`) and extracted to `${CODEX_HOME}/skills` on startup.

Included skills:

- `alice-memory`
- `alice-message`
- `alice-scheduler`
- `alice-code-army`
- `file-printing`
- `feishu-task`

## Release Pipeline

Current release paths:

1. `dev` push: quality gate + dev binary build + update prerelease tag `dev-latest`.
2. `dev` merged into `main`: quality gate + auto semver tag + cross-build + GitHub Release.
3. Manual `v*` tag push: test + cross-build + GitHub Release.

Workflow files:

- [ci.yml](./.github/workflows/ci.yml)
- [main-release.yml](./.github/workflows/main-release.yml)
- [release-on-tag.yml](./.github/workflows/release-on-tag.yml)

Manual release tag example:

```bash
git tag v1.2.3
git push github v1.2.3
```

## Development

```bash
make check
make build
make run
```

`make check` includes:

- secret scan
- shell script syntax check
- gofmt check
- `go vet ./...`
- `go test ./...`
- `go test -race ./internal/connector`

## Docs

- [README.zh-CN.md](./README.zh-CN.md)
- [Architecture](./docs/architecture.md)
- [Feishu Message Flow (ZH)](./docs/feishu-message-flow.zh-CN.md)

## License

[MIT](./LICENSE)
