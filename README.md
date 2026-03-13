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

The same binary also exposes the skill-facing runtime CLI used by bundled skills:

```bash
./bin/alice-connector runtime memory context
```

## Pre-commit checks

Run all checks manually:

```bash
make check
```

`make check` includes secret scanning (`make secret-check`) to block accidental key/token commits.

Install git hooks:

- `pre-commit`: runs `make check` before commit
- `commit-msg`: enforces Conventional Commits format

```bash
make precommit-install
```

## Contributing

Contribution rules are documented in [CONTRIBUTING.md](./CONTRIBUTING.md).

## Architecture

- [Architecture and refactor plan](./docs/architecture.md)

## Bundled Skills

This repository now bundles reusable skills under [`skills/`](./skills):

- `alice-codebase-onboarding`
- `alice-memory`
- `alice-message`
- `alice-scheduler`
- `alice-code-army`
- `file-printing`
- `feishu-task`

On connector startup, Alice automatically links all bundled skills to `$CODEX_HOME/skills` (default: `~/.codex/skills`). Existing non-symlink skill directories with the same name are backed up once and replaced by symlinks.

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
trigger_mode: "at"
trigger_prefix: ""

llm_provider: "codex"
codex_command: "codex"
codex_timeout_secs: 120
claude_command: "claude"
claude_timeout_secs: 120
kimi_command: "kimi"
kimi_timeout_secs: 120
runtime_http_addr: "127.0.0.1:7331"
runtime_http_token: ""
workspace_dir: "."
env:
  HTTPS_PROXY: "http://127.0.0.1:7890"
  ALL_PROXY: "socks5://127.0.0.1:7891"
memory_dir: ".memory"

codex_prompt_prefix: ""
claude_prompt_prefix: ""
kimi_prompt_prefix: ""
failure_message: "Codex 暂时不可用，请稍后重试。"
thinking_message: "正在思考中..."
immediate_feedback_mode: "reply"
immediate_feedback_reaction: "SMILE"

queue_capacity: 256
worker_concurrency: 1
automation_task_timeout_secs: 600
idle_summary_hours: 8
group_context_window_minutes: 5

log_level: "info"
log_file: ""
log_max_size_mb: 20
log_max_backups: 5
log_max_age_days: 7
log_compress: false
```

Required keys:

- `feishu_app_id`
- `feishu_app_secret`

Optional:

- `llm_provider`: LLM backend provider selector. Supported values: `codex` (default), `claude`, and `kimi`.
- `codex_command` / `codex_timeout_secs`, `claude_command` / `claude_timeout_secs`, `kimi_command` / `kimi_timeout_secs`: CLI command path and timeout (seconds) for each backend.
- `runtime_http_addr` / `runtime_http_token`: bind address and bearer token for Alice's local runtime HTTP API. If `runtime_http_token` is empty, Alice generates a per-process token and injects it into agent environments automatically.
- `env`: key-value environment variables injected into the selected LLM process (for example HTTP/HTTPS/SOCKS proxy settings).
- `codex_prompt_prefix` / `claude_prompt_prefix` / `kimi_prompt_prefix`: global instruction prefix prepended for new threads only; default is empty.
- `immediate_feedback_mode`: immediate feedback strategy for reply-style messages. Supports `reply` (default, send `收到！`) and `reaction` (prefer a message reaction, then fall back to `收到！` if reaction fails).
- `immediate_feedback_reaction`: Feishu reaction type used when `immediate_feedback_mode=reaction`, default `SMILE`.
- `automation_task_timeout_secs`: timeout window for a single automation user task execution (`send_text`/`run_llm`), default `600`.
- `idle_summary_hours`: idle threshold (hours) before background daily summary write (default `8`).
- `group_context_window_minutes`: sliding window duration (minutes) for caching non-triggered group messages (text + multimedia), merged on the next trigger in `at`/`prefix` mode (default `5`).
- `log_file` / `log_max_size_mb` / `log_max_backups` / `log_max_age_days` / `log_compress`: optional rotating log file settings backed by `zerolog + lumberjack`.
- `trigger_mode`: group trigger strategy. Supported values: `at` (default), `active`, `prefix`.
- `trigger_prefix`: prefix used by group trigger strategy. In `active`, messages starting with this prefix are ignored unless they mention the bot; in `prefix`, messages are processed when either the prefix matches or the bot is mentioned. When the prefix matches in `prefix` mode, it is stripped before sending to Codex.
- `feishu_bot_open_id` / `feishu_bot_user_id`: bot IDs used for mention matching in group/topic-group chats.

## Runtime behavior

- Supported incoming message types: `text`, `image`, `sticker`, `audio`, `file`.
- Group/topic-group trigger behavior is controlled by `trigger_mode`:
  - `at`: only messages mentioning bot IDs are processed. If both `feishu_bot_open_id` and `feishu_bot_user_id` are empty, group/topic-group messages are ignored.
  - `active`: all group/topic-group messages are processed, except messages starting with `trigger_prefix`; bot mentions still force processing.
  - `prefix`: messages are processed when they start with `trigger_prefix` or mention bot IDs.
- Group context window (`group_context_window_minutes`) is applied in `trigger_mode=at` and `trigger_mode=prefix`: non-trigger messages are cached, then merged on the next trigger in the same sender/chat thread scope.
- Mention tags like `<at ...>...</at>` are removed from text before sending to Codex.
- Prompt speaker context still injects id mappings and mention text for participants, with an explicit hint that `@name`/`@id` can be used directly, but it filters out the bot's own identity (`feishu_bot_open_id`/`feishu_bot_user_id`) from those injected lines.
- Outgoing replies auto-normalize `@name`/`@id` to Feishu mention tags (`<at user_id="...">...</at>`) using identities in the current message context.
- User display-name enrichment first uses Contact `GetUser`; if name is empty in group/topic-group chats, it falls back to `GetChatMembers` by `chat_id`.
- To enable the group member name fallback, grant one of: `im:chat.members:read`, `im:chat.group_info:readonly`, `im:chat:readonly`, `im:chat`.
- Memory module is enabled by default, writing files under `memory_dir`: long-term `MEMORY.md` and date-based memory in `daily/YYYY-MM-DD.md`.
- Downloaded incoming resources are stored under `memory_dir/resources/YYYY-MM-DD/<source_message_id>/`.
- On first startup, the connector auto-creates `memory_dir` and its `daily/` subdirectory.
- The connector also persists per-chat session state in `memory_dir/session_state.json` to keep thread continuity across restarts.
- The connector persists queued jobs in `memory_dir/runtime_state.json`; after restart it resumes queued jobs that were not started yet.
- Automation tasks are persisted in `memory_dir/automation.db` via `bbolt`; existing legacy `automation_state.json` data is imported automatically on first open.
- If shutdown/restart happens while a job is being processed, that in-progress job is discarded and will not be resumed after restart.
- Before each Codex call, only long-term memory is injected; date-based memory is exposed as a directory path for Codex to search on demand.
- Session reuse is now thread-aware:
  - Messages in the same Feishu thread/topic (`thread_id`, fallback `root_id`) reuse one Codex thread.
  - Messages without thread context start a new Codex session per incoming message.
- If a chat stays idle for `idle_summary_hours` (default 8), a background task asynchronously resumes that thread and appends an "idle summary" to `daily/YYYY-MM-DD.md` once per idle period.
- The message path does not wait for idle-summary writes; new messages are handled immediately.
- In reply flow, the bot prefers topic replies (`reply_in_thread=true`) for ack/progress/final messages; if Feishu rejects topic mode, it falls back to normal replies.
- Bundled skills route text/image/file sending through the local runtime HTTP API. The target is always derived from current session context: private chats send to the current private chat; group/topic chats with `source_message_id` reply to that message (thread-preferred).
- The bot sends immediate feedback according to `immediate_feedback_mode`: by default it replies with `收到！`, and it can be switched to prefer a message reaction instead.
- During Codex execution, streamed `agent_message` updates are sent as card replies first; if card reply fails, fallback is rich-text (`post`) then plain text.
- If outgoing content contains resolved mentions, the connector sends plain text directly (instead of card/post) to ensure Feishu mention delivery works.
- Streamed `file_change` updates use the same card-first reply path, for example: `internal/x.go已更改，+23-34`.
- If the current Codex CLI does not emit native `file_change` events, the connector falls back to repo diff snapshots (git numstat) and still emits `file_change`-style updates.
- If a newer user message arrives in the same session, the running task is interrupted immediately and switched to the latest message (steer behavior).
- If no streamed `agent_message` was sent, the final Codex answer is sent via the same card-first fallback chain.
- Reply target priority (fallback path): `chat_id`, fallback to sender `open_id`.
- On Codex failure/timeout, sends `failure_message`.

Note: this project now uses a card-first reply flow and no longer uses interactive card patch flow.

## Feishu API references

- Reply message: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/reply
- API catalog: https://open.feishu.cn/api_explorer/v1/api_catalog

## Project layout

- `cmd/connector/main.go`: bootstrap and lifecycle
- `cmd/connector/runtime_*.go`: skill-facing runtime subcommands on the same `alice-connector` binary
- `internal/config/config.go`: config file loading and validation (`viper`)
- `internal/bootstrap/`: startup/runtime assembly helpers shared by binaries, including staged connector runtime builder
- `internal/automation/`: scheduler, persistence, and action execution for Alice automation tasks
- `internal/llm/`: LLM backend abstraction and backend factory
- `internal/memory/memory.go`: memory module (long-term + date-based short-term memory files)
- `internal/llm/codex/codex.go`: Codex CLI call + JSONL parsing
- `internal/connector/app.go`: long-connection app loop, websocket lifecycle, worker orchestration
- `internal/connector/app_queue.go`: session routing, queueing, and active-run steering
- `internal/connector/processor.go`: prompt building, Codex invocation, and job-level orchestration
- `internal/connector/reply_dispatcher.go`: centralized card/post/text fallback policy for replies and sends
