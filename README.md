# Alice

[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Release On Tag](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/release-on-tag.yml)

A Feishu long-connection connector for CLI-based LLM agents such as Codex, Claude, Gemini, and Kimi.

Alice runs as a local multi-bot runtime:

- receives Feishu messages over WebSocket
- routes messages into `chat` or `work` scenes
- calls the configured LLM CLI backend
- sends progress, replies, files, and images back to Feishu
- exposes a local runtime API used by bundled skills

For Chinese documentation, see [README.zh-CN.md](./README.zh-CN.md).

## Features

- Multi-bot runtime from a single `config.yaml`
- Per-bot isolated `workspace`, `SOUL.md` (in `alice_home`), and prompts, with shared `CODEX_HOME` by default
- Scene-aware routing for casual chat and explicit work threads
- Runtime HTTP API for bundled skills and automation
- Long-running LLM status cards with backend-activity and file-change signals
- Automation watchdog alerts for overdue or stuck scheduled tasks
- Bundled skills are materialized under `${ALICE_HOME:-~/.alice}/skills`, linked into `~/.agents/skills`, and exposed to Claude via `~/.claude/skills`
- Embedded prompts, skills, config example, and `SOUL.md` example
- Release installer for `systemd --user` deployments

## Requirements

- Go 1.25+ for source builds
- One installed and authenticated backend CLI:
  - `codex`
  - `claude`
  - `gemini`
  - `kimi`
- A Feishu app with:
  - bot capability
  - `im.message.receive_v1` subscription
  - required message permissions
  - long connection mode enabled

## Quick Start

### Install From Release

**Via npm (recommended):**

```bash
npm install -g @alice_space/alice
```

**Via installer script:**

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install
```

Then:

1. Edit `${ALICE_HOME:-~/.alice}/config.yaml`
2. Set `bots.*.feishu_app_id` and `bots.*.feishu_app_secret`
3. Restart the service:

```bash
systemctl --user restart alice.service
```

### Run From Source

```bash
cp config.example.yaml ~/.alice/config.yaml
# edit ~/.alice/config.yaml

go mod tidy
go test ./...
go run ./cmd/connector --feishu-websocket
```

## Configuration

Alice uses a pure multi-bot config model.

Important concepts:

- `bots.<id>`: one runtime bot
- `llm_profiles`: named model presets
- `group_scenes.chat`: conversational scene for group chats
- `group_scenes.work`: explicit task scene for work threads
- `trigger_mode`: legacy fallback when both scenes are disabled
- `workspace_dir` / `prompt_dir`: per-bot runtime directories
- `codex_home`: optional per-bot override for the shared `CODEX_HOME` (default: `~/.codex`)
Start from [config.example.yaml](./config.example.yaml).

## Usage

Alice's operating model and `chat` / `work` scene behavior are documented in:

- [Usage Guide](./docs/usage.md)
- [使用说明](./docs/usage.zh-CN.md)

Additional docs:

- [Documentation Index](./docs/README.md)
- [Architecture](./docs/architecture.md)
- [架构文档](./docs/architecture.zh-CN.md)

Connector startup mode is now explicit: use `--feishu-websocket` for the real Feishu connector, or `--runtime-only` for local runtime/API-only execution. For isolated debug or temporary rerun runtimes, use `alice-headless --runtime-only`; headless binaries no longer allow Feishu websocket startup.

## `SOUL.md`

Each bot can define persona and machine-readable metadata in its configured `soul_path`.
The default is `<alice_home>/SOUL.md`; a relative `soul_path` is resolved relative to `<alice_home>`.

Current frontmatter keys accepted by Alice:

- `image_refs`
- `output_contract`

The embedded example is [prompts/SOUL.md.example](./prompts/SOUL.md.example).

## Installer

The installer script lives at [scripts/alice-installer.sh](./scripts/alice-installer.sh).

Common commands:

```bash
# install or update the latest stable release
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- install

# uninstall
curl -fsSL https://cdn.jsdelivr.net/gh/Alice-space/alice@main/scripts/alice-installer.sh | bash -s -- uninstall
```

## Development

```bash
make check
make build
make run
```

`make check` includes formatting, vet, unit tests, and connector race tests.

Contribution guidelines are in [CONTRIBUTING.md](./CONTRIBUTING.md).

## Release Process

- Day-to-day work happens on `dev`
- `dev -> main` drives the normal release path
- Tagged releases are published through GitHub Actions

Workflow files:

- [.github/workflows/ci.yml](./.github/workflows/ci.yml)
- [.github/workflows/main-release.yml](./.github/workflows/main-release.yml)
- [.github/workflows/release-on-tag.yml](./.github/workflows/release-on-tag.yml)

## License

[MIT](./LICENSE)
