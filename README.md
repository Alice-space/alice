# Alice Connector

[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

A Feishu long-connection connector for Codex / Claude / Kimi.

- Receives Feishu messages over WebSocket (`im.message.receive_v1`)
- Calls the selected LLM CLI backend
- Sends reply / progress / file-change feedback back to Feishu
- Supports memory, automation scheduling, and runtime APIs

## Highlights

- Standalone runtime: binary can run without repository checkout
- Embedded prompts + skills: prompts are bundled in binary; bundled skills are synced on startup
- Isolated runtime home: defaults to `${ALICE_HOME:-~/.alice}`
- Isolated Codex home: defaults to `${ALICE_HOME}/.codex`
- Tag-based release pipeline: test + cross-build + GitHub Release

## Runtime Layout

By default Alice uses:

- Config: `${ALICE_HOME:-~/.alice}/config.yaml`
- Binary: `${ALICE_HOME:-~/.alice}/bin/alice`
- Logs: `${ALICE_HOME:-~/.alice}/log/YYYY-MM-DD.log` (default)
- Runtime state: `${ALICE_HOME:-~/.alice}/memory/`
- Bundled skills: `${ALICE_HOME:-~/.alice}/.codex/skills/`

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

Uninstall (remove service + binary + `~/.alice`):

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- uninstall
```

Uninstall but keep runtime data:

```bash
curl -fsSL https://raw.githubusercontent.com/Alice-space/alice/main/scripts/alice-installer.sh | bash -s -- uninstall --keep-data
```

What the installer does:

- Downloads the latest GitHub Release asset and installs `${ALICE_HOME:-~/.alice}/bin/alice`
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
- `log_file` (default `${ALICE_HOME}/log/YYYY-MM-DD.log`) and rotate options

Default process env behavior:

- Alice enforces `CODEX_HOME=${ALICE_HOME}/.codex` on startup
- The same `CODEX_HOME` is injected into Codex/Claude/Kimi subprocesses unless explicitly overridden in `env`

See [config.example.yaml](./config.example.yaml) for full schema.

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

Tag push to GitHub triggers:

1. `go test ./...`
2. Cross-platform builds
3. GitHub Release creation with uploaded artifacts

Workflow file: [release-on-tag.yml](./.github/workflows/release-on-tag.yml)

Release example:

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
