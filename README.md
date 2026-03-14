# Alice Connector

[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

A Feishu long-connection connector for Codex / Claude / Kimi.

- Receives Feishu messages over WebSocket (`im.message.receive_v1`)
- Calls the selected LLM CLI backend
- Sends reply / progress / file-change feedback back to Feishu
- Supports memory, automation scheduling, and runtime APIs

## Highlights

- Standalone runtime: binary can run without repository checkout
- Embedded prompts and skills: extracted on first run
- Isolated runtime home: defaults to `${ALICE_HOME:-~/.alice}`
- Isolated Codex home: defaults to `${ALICE_HOME}/.codex`
- Tag-based release pipeline: test + cross-build + GitHub Release

## Runtime Layout

By default Alice uses:

- Config: `${ALICE_HOME:-~/.alice}/config.yaml`
- Binary: `${ALICE_HOME:-~/.alice}/bin/alice`
- Runtime state: `${ALICE_HOME:-~/.alice}/memory/`
- Bundled skills: `${CODEX_HOME:-${ALICE_HOME:-~/.alice}/.codex}/skills/`

## Requirements

- Go 1.23+ (source build only)
- `codex` CLI (or `claude` / `kimi`) installed and logged in
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

## Standalone Install & Deployment (No Repo Required)

This mode is for production/runtime hosts where you only keep the binary and runtime files.

1. Download a release asset from GitHub Releases (`linux_amd64`, `linux_arm64`, `darwin_*`, `windows_amd64`).
2. Place it under `${ALICE_HOME:-~/.alice}/bin/alice`.
3. Create `${ALICE_HOME:-~/.alice}/config.yaml`.
4. Run the binary directly.

Example (Linux):

```bash
export ALICE_HOME="$HOME/.alice"
mkdir -p "$ALICE_HOME/bin" "$ALICE_HOME/logs"

# Replace VERSION with an actual tag, for example v1.2.3
VERSION="vX.Y.Z"
ASSET="alice_${VERSION}_linux_amd64.tar.gz"

curl -fL "https://github.com/Alice-space/alice/releases/download/${VERSION}/${ASSET}" -o "/tmp/${ASSET}"
tar -xzf "/tmp/${ASSET}" -C "$ALICE_HOME/bin"
mv "$ALICE_HOME/bin/alice_${VERSION}_linux_amd64" "$ALICE_HOME/bin/alice"
chmod +x "$ALICE_HOME/bin/alice"
```

Create config (`$ALICE_HOME/config.yaml`):

```yaml
feishu_app_id: "cli_xxxxx"
feishu_app_secret: "xxxxxx"
llm_provider: "codex"
codex_command: "codex"
# Optional: defaults are already under ALICE_HOME
workspace_dir: ""
memory_dir: ""
prompt_dir: ""
```

Run foreground:

```bash
ALICE_HOME="$HOME/.alice" "$HOME/.alice/bin/alice"
```

Run background (without systemd):

```bash
nohup env ALICE_HOME="$HOME/.alice" "$HOME/.alice/bin/alice" \
  >"$HOME/.alice/logs/connector.log" 2>&1 &
```

Notes:

- Alice writes pid file by default at `${ALICE_HOME}/run/alice.pid`.
- On startup, embedded skills are materialized to `${CODEX_HOME}/skills`.
- Legacy skill symlinks are auto-migrated to real directories.

## Configuration

Required keys:

- `feishu_app_id`
- `feishu_app_secret`

Common optional keys:

- `llm_provider`: `codex` (default), `claude`, `kimi`
- `<provider>_command` / `<provider>_timeout_secs`
- `runtime_http_addr` / `runtime_http_token`
- `env`: extra env vars passed to backend process
- `workspace_dir` / `memory_dir` / `prompt_dir`
- `trigger_mode`: `at` / `active` / `prefix`
- `trigger_prefix`
- `log_file` and rotate options

Default process env behavior:

- Alice enforces `CODEX_HOME=${ALICE_HOME}/.codex` on startup
- The same `CODEX_HOME` is injected into Codex/Claude/Kimi subprocesses unless explicitly overridden in `env`

See [config.example.yaml](./config.example.yaml) for full schema.

## Bundled Skills

Bundled skills are embedded into the binary (`skills/**`) and extracted to `${CODEX_HOME}/skills` on startup.

Included skills:

- `alice-codebase-onboarding`
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
- gofmt check
- `go vet ./...`
- `go test ./...`

## Docs

- [README.zh-CN.md](./README.zh-CN.md)
- [Architecture](./docs/architecture.md)
- [Feishu Message Flow (ZH)](./docs/feishu-message-flow.zh-CN.md)

## License

[MIT](./LICENSE)
