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

Install git hooks:

- `pre-commit`: runs `make check` before commit
- `commit-msg`: enforces Conventional Commits format

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
feishu_bot_open_id: ""
feishu_bot_user_id: ""

codex_command: "codex"
codex_timeout_secs: 120
workspace_dir: "."
env:
  HTTPS_PROXY: "http://127.0.0.1:7890"
  ALL_PROXY: "socks5://127.0.0.1:7891"
memory_dir: ".memory"

codex_prompt_prefix: ""
failure_message: "Codex 暂时不可用，请稍后重试。"
thinking_message: "正在思考中..."

queue_capacity: 256
worker_concurrency: 1
idle_summary_hours: 8

log_level: "info"
```

Required keys:

- `feishu_app_id`
- `feishu_app_secret`

Optional:

- `env`: key-value environment variables injected into `codex` process (for example HTTP/HTTPS/SOCKS proxy settings).
- `codex_prompt_prefix`: global instruction prefix prepended for new threads only; default is empty.
- `idle_summary_hours`: idle threshold (hours) before background daily summary write (default `8`).
- `feishu_bot_open_id` / `feishu_bot_user_id`: bot IDs used for strict group mention filtering. In group chats, only messages that mention these IDs are processed.

## Runtime behavior

- Supported incoming message types: `text`, `image`, `sticker`, `audio`, `file`.
- In group/topic-group chats, only messages that mention the bot are processed.
  - If both `feishu_bot_open_id` and `feishu_bot_user_id` are empty, group/topic-group messages are ignored.
- For group/topic-group multimedia (`image`/`sticker`/`audio`/`file`) without mention, the connector caches a per-user 5-minute sliding window.
- When the same user later sends an `@bot` trigger message in that group, cached multimedia from the previous 5 minutes is merged into that request context.
- Mention tags like `<at ...>...</at>` are removed from text before sending to Codex.
- Memory module is enabled by default, writing files under `memory_dir`: long-term `MEMORY.md` and date-based memory in `daily/YYYY-MM-DD.md`.
- Downloaded incoming resources are stored under `memory_dir/resources/YYYY-MM-DD/<source_message_id>/`.
- On first startup, the connector auto-creates `memory_dir` and its `daily/` subdirectory.
- The connector also persists per-chat session state in `memory_dir/session_state.json` to keep thread continuity across restarts.
- The connector persists queued jobs in `memory_dir/runtime_state.json`; after restart it resumes queued jobs that were not started yet.
- If shutdown/restart happens while a job is being processed, that in-progress job is discarded and will not be resumed after restart.
- Before each Codex call, only long-term memory is injected; date-based memory is exposed as a directory path for Codex to search on demand.
- Session reuse is now thread-aware:
  - Messages in the same Feishu thread/topic (`thread_id`, fallback `root_id`) reuse one Codex thread.
  - Messages without thread context start a new Codex session per incoming message.
- If a chat stays idle for `idle_summary_hours` (default 8), a background task asynchronously resumes that thread and appends an "idle summary" to `daily/YYYY-MM-DD.md` once per idle period.
- The message path does not wait for idle-summary writes; new messages are handled immediately.
- In reply flow, the bot prefers topic replies (`reply_in_thread=true`) for ack/progress/final messages; if Feishu rejects topic mode, it falls back to normal replies.
- The bot immediately replies to the source message with `收到！`.
- During Codex execution, each streamed `agent_message` (Markdown content) is sent as a rich-text (`post`) reply to the same source message.
- During Codex execution, each streamed `file_change` event is sent as a rich-text (`post`) reply, for example: `internal/x.go已更改，+23-34`.
- If the current Codex CLI does not emit native `file_change` events, the connector falls back to repo diff snapshots (git numstat) and still emits `file_change`-style updates.
- If a newer user message arrives in the same session, the running task is interrupted immediately and switched to the latest message (steer behavior).
- If no streamed `agent_message` was sent, the final Codex answer is sent as a text reply.
- Reply target priority (fallback path): `chat_id`, fallback to sender `open_id`.
- On Codex failure/timeout, sends `failure_message`.

Note: this project now uses reply-message flow (text replies + rich-text file-change replies) and no longer uses interactive card patch flow.

## Feishu API references

- Reply message: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/reply
- API catalog: https://open.feishu.cn/api_explorer/v1/api_catalog

## Project layout

- `cmd/connector/main.go`: bootstrap and lifecycle
- `internal/config/config.go`: config file loading and validation (`viper`)
- `internal/memory/memory.go`: memory module (long-term + date-based short-term memory files)
- `internal/codex/codex.go`: Codex CLI call + JSONL parsing
- `internal/connector/connector.go`: long connection, queue, workers, Feishu send
