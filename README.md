# Feishu -> Codex Connector (Go, Long Connection)

[中文同步版](./README.zh-CN.md)

A minimal connector that:

1. Uses **Feishu official Go SDK** (`github.com/larksuite/oapi-sdk-go/v3`) long connection (`ws`) mode.
2. Receives `im.message.receive_v1` text events.
3. Calls `codex exec` for each text message.
4. Sends the reply back to Feishu.

This mode works **without a public IP** because it uses Feishu long connection (WebSocket), not webhook callbacks.

## Why Go instead of Rust

Feishu currently provides official server SDKs for Go/Java/Python/Node, and official long connection support is in the official Go SDK. There is no official Rust SDK.

## Requirements

- Go 1.23+ (tested on Go 1.26)
- `codex` CLI installed and logged in (`codex login status`)
- A Feishu app with:
  - Bot capability enabled
  - Event subscription to `im.message.receive_v1`
  - Required message permissions
  - Long connection mode enabled in Feishu platform settings

## Quickstart

```bash
cp config.example.yaml config.yaml
# edit config.yaml

# install dependencies
go mod tidy

# run tests
go test ./...

# start connector
go run ./cmd/connector -c config.yaml
```

## Build

Compile current platform binary:

```bash
go build -o bin/alice-connector ./cmd/connector
```

Then run:

```bash
./bin/alice-connector -c config.yaml
```

## Pre-commit checks

Run all checks manually:

```bash
make check
```

Install git pre-commit hook (runs `make check` before commit):

```bash
make precommit-install
```

## Contributing

Contribution rules are documented in [CONTRIBUTING.md](./CONTRIBUTING.md).

## Config file

The application loads config from YAML file (default: `config.yaml`).

You can provide a custom file path:

```bash
go run ./cmd/connector -c /path/to/config.yaml
```

`config.example.yaml`:

```yaml
feishu_app_id: "cli_xxxxx"
feishu_app_secret: "xxxxxx"
feishu_base_url: "https://open.feishu.cn"

codex_command: "codex"
codex_timeout_secs: 120
workspace_dir: "."
memory_dir: ".memory"

codex_prompt_prefix: "你是一个助手，请用中文简洁回答，不要使用 Markdown 标题。"
failure_message: "Codex 暂时不可用，请稍后重试。"
thinking_message: "正在思考中..."

queue_capacity: 256
worker_concurrency: 1

log_level: "info"
```

Required keys:

- `feishu_app_id`
- `feishu_app_secret`

## Runtime behavior

- Non-text messages are ignored.
- Mention tags like `<at ...>...</at>` are removed from text before sending to Codex.
- Memory module is enabled by default, writing files under `memory_dir`: long-term `MEMORY.md` and date-based memory in `daily/YYYY-MM-DD.md`.
- Before each Codex call, only long-term memory is injected; date-based memory is exposed as a directory path for Codex to search on demand.
- Each interaction is appended to current-day short-term memory; messages containing "记住"/"长期记忆"/"remember this" are also appended to `MEMORY.md`.
- The bot replies with an **interactive card** quoting the source message (`reply` API).
- While Codex is running, the card is patched incrementally with Codex reasoning.
- If a newer user message arrives in the same session, the running task is interrupted immediately and switched to the latest message (steer behavior).
- After completion, the same card is patched with final answer (`patch` API).
- Reply target priority (fallback path for non-card mode): `chat_id`, fallback to sender `open_id`.
- On Codex failure/timeout, sends `failure_message`.

Note: Feishu OpenAPI currently has reply/patch APIs, but no dedicated bot typing-status API in the IM catalog. This project uses card incremental patches as a typing-like experience.

## Feishu API references

- Reply message: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/reply
- Patch message card: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch
- API catalog: https://open.feishu.cn/api_explorer/v1/api_catalog

## Project layout

- `cmd/connector/main.go`: bootstrap and lifecycle
- `internal/config/config.go`: config file loading and validation (`viper`)
- `internal/memory/memory.go`: memory module (long-term + date-based short-term memory files)
- `internal/codex/codex.go`: Codex CLI call + JSONL parsing
- `internal/connector/connector.go`: long connection, queue, workers, Feishu send
