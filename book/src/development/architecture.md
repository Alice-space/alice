# Architecture Overview

This is the code-first architecture reference for Alice. Package names, runtime objects, and file paths match the live code under `cmd/connector`, `internal/`, `prompts/`, and `skills/`.

## Reading Paths

Start with the section that matches your goal:

| Goal | Start here |
|------|-----------|
| Understand the whole system | §1 Process Model → §2 Bootstrap Path → §5 Message Pipeline |
| Add a new LLM backend | §2 Bootstrap Path → §7 Prompt Assembly → [Adding a New LLM Backend](add-backend.md) |
| Modify message handling | §5 Inbound Message Pipeline → §6 Session Keys → §8 Reply Dispatch |
| Add a Runtime API endpoint | §9 Runtime API |
| Add or modify automation | §10 Automation Subsystem |
| Understand configuration | §2 Bootstrap Path → §12 Configuration Model |

## 1. Process Model

Alice is a multi-bot runtime. One `alice` process can host multiple bots from one `config.yaml`.

At startup, the process:

1. Loads `config.yaml`
2. Expands `bots.*` into per-bot runtime configs
3. Verifies CLI auth where needed
4. Syncs embedded bundled skills into the local skill directories
5. Builds one `ConnectorRuntime` per bot
6. Runs all runtimes under one `RuntimeManager`

The main runtime object per bot:

```text
ConnectorRuntime
  ├─ App
  ├─ Processor
  ├─ llm.MultiBackend
  ├─ LarkSender
  ├─ automation.Engine
  ├─ runtimeapi.Server
  ├─ automation.Store
  └─ campaign.Store
```

Startup mode is explicit:
- `--feishu-websocket`: connect to Feishu and process live events
- `--runtime-only`: run automation and the local runtime API without the Feishu WebSocket
- `alice-headless`: runtime-only only; may not start the Feishu connector

## 2. Bootstrap Path

The process entrypoint is `cmd/connector`.

Key bootstrap steps:
- `cmd/connector/root.go`: CLI flags, startup mode selection, config creation, PID locking, logging, auth preflight, bundled-skill sync, and runtime manager startup.
- `internal/config`: Pure multi-bot config model, path derivation, normalization, validation, and per-bot runtime expansion.
- `internal/bootstrap`: Builds the per-bot runtime graph and wires cross-cutting features such as prompt loading, runtime API auth, campaign reconcile loops, and config hot reload.

`BuildRuntimeManager` expands `Config` into `[]Config` via `RuntimeConfigs()`, then builds one `ConnectorRuntime` for each bot.

Current hot-reload behavior:
- Single-bot mode: partial config hot reload is supported
- Multi-bot mode: hot reload is intentionally disabled; restart the process after config changes

## 3. Runtime Layout And Persisted State

Each bot gets its own runtime root under:

```text
${ALICE_HOME}/bots/<bot_id>/
```

Important per-bot paths:
- `workspace/` — Bot workspace
- `prompts/` — Optional prompt overrides for that bot
- `run/connector/automation.db` — Persistent automation task store (bbolt)
- `run/connector/campaigns.db` — Persistent lightweight campaign index (bbolt)
- `run/connector/session_state.json` — Session aliases, provider thread ids, usage counters, work-thread metadata
- `run/connector/runtime_state.json` — Mutable connector runtime state
- `run/connector/resources/scopes/<scope_type>/<scope_id>/` — Downloaded inbound attachments and uploadable local artifacts scoped to the current conversation

The source tree also embeds:
- `prompts/`
- `skills/`
- `config.example.yaml`
- `prompts/SOUL.md.example`

Disk files override embedded prompt files when present; embedded assets are the fallback.

## 4. Package Map

### Core Packages

| Package | Responsibility |
|---------|---------------|
| `cmd/connector` | CLI entrypoint, `runtime` subcommands, and `skills sync` |
| `internal/bootstrap` | Runtime construction, path resolution, auth checks, skill materialization, campaign reconcile bridging, and config reload |
| `internal/config` | Config schema, validation, defaults, path derivation, and multi-bot expansion |
| `internal/connector` | Feishu ingress, message normalization, scene routing, queueing, session serialization, native steer fallback, `/stop` interruption, prompt assembly, reply dispatch, attachment download, session persistence, and built-in commands |
| `internal/llm` | Provider-agnostic Backend interface plus provider adapters for `codex`, `claude`, `gemini`, `kimi`, and `opencode` |
| `internal/prompting` | Template loader with disk-first / embedded-fallback behavior, `sprig` helpers, and compiled-template caching |
| `internal/runtimeapi` | Local authenticated HTTP server and client used by bundled skills and runtime-facing shell scripts |
| `internal/automation` | Task model, persistence, claiming, execution, system-task scheduling, and workflow dispatch |
| `internal/statusview` | Aggregates usage and automation data for `/status` |
| `internal/platform/feishu` | Feishu sender implementation, attachment I/O, bot self-info lookup, message lookup, and user-name resolution helpers |

### Support Packages

| Package | Responsibility |
|---------|---------------|
| `internal/sessionctx` | Session-context environment bridge for runtime API calls and bundled skills |
| `internal/runtimecfg` | Helpers for scene-derived profile selection and thread-reply preference |
| `internal/sessionkey` | Canonical session-key and visibility-key helpers |
| `internal/messaging` | Narrow sender/uploader interfaces shared across connector and runtime API layers |
| `internal/storeutil` | Shared bbolt helpers and string utilities |
| `internal/logging` | Zerolog plus rotating file output configuration |
| `internal/buildinfo` | Version reporting |

## 5. Inbound Message Pipeline

`internal/connector.App` owns the live Feishu connection and the per-bot job queue.

High-level flow:

1. Feishu delivers `im.message.receive_v1` over WebSocket
2. `App` normalizes the event into a `Job`
3. `routeIncomingJob` decides whether the message should be ignored, treated as a built-in command, handled as `chat`, or handled as `work`
4. If the same session has an active provider-native interactive run, Alice first tries to steer the new input into that run
5. If native steer is unavailable, the job is queued and serialized by session; newer queued jobs supersede older queued jobs without interrupting the active LLM run
6. `/stop` still interrupts the active run, and user messages can still interrupt automation tasks that acquired the session gate
7. `Processor` executes the accepted job

Scene routing rules:
- Group/topic-group chats can use `group_scenes.chat` and `group_scenes.work`
- Work threads are identified by a trigger plus a stable work-scene session key
- If both scenes are disabled, Alice falls back to legacy `trigger_mode` / `trigger_prefix`
- Built-in commands such as `/help`, `/status`, `/clear`, and `/stop` bypass the LLM path

## 6. Session Keys, Aliases, And Serialization

Alice routes and resumes work through canonical session keys plus aliases.

Common formats:
- `{receive_id_type}:{receive_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}`
- `{receive_id_type}:{receive_id}|scene:{scene}|thread:{thread_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}|message:{message_id}`

Special cases:
- Work-scene seed key: `{receive_id_type}:{receive_id}|scene:work|seed:{source_message_id}`
- Chat reset alias: `{chat_key}|reset:{message_id}`

Persisted in `session_state.json`:
- Provider thread id
- Work-thread id alias
- Session aliases
- Usage counters
- Last-message timestamp
- Scope key for status aggregation

`internal/connector/runtime_store.go` keeps the live in-memory coordination state:
- Latest version per session
- Pending job per session
- Active run cancellation handle
- Per-session mutex for serialization
- Superseded-version tracking

## 7. Prompt Assembly And LLM Execution

`internal/connector.Processor` is the execution core for one accepted job.

Before an LLM call it:
- Loads and parses `SOUL.md` if needed
- Downloads inbound attachments into the scoped resource directory
- Derives runtime env vars for the current conversation
- Prepares prompt text

Current prompt assets:
- `prompts/llm/initial_prompt.md.tmpl`
- `prompts/connector/bot_soul.md.tmpl`
- `prompts/connector/current_user_input.md.tmpl`
- `prompts/connector/reply_context.md.tmpl`
- `prompts/connector/runtime_skill_hint.md.tmpl`
- `prompts/connector/synthetic_mention.md.tmpl`

Important prompt behavior:
- First-turn or non-resumed runs render the current-user-input template and may append reply context, bot soul, and runtime-skill hints
- Resumed provider threads send only the current user input; Alice relies on the provider-side thread/session to hold prior context
- `chat` runs can prepend `SOUL.md`; `work` runs intentionally skip bot-soul injection

The LLM layer is selected like this:
1. Scene selects an outer `llm_profiles.<name>`
2. The outer profile chooses provider / model / profile / reasoning / personality / prompt prefix
3. `llm.MultiBackend` dispatches to the correct provider adapter

Currently supported providers: `codex`, `claude`, `gemini`, `kimi`, `opencode`

## 8. Reply Dispatch

Alice distinguishes between:
- Immediate acknowledgement
- Streamed progress messages from the backend
- Final replies
- File/image follow-ups

Current behavior:
- Work-scene messages usually receive an immediate reaction or `收到！`
- Backend progress messages are sent as threaded replies when possible
- Final replies are posted via the reply dispatcher
- Thread replies fall back to direct replies when Feishu does not support threaded replies for that target

`internal/connector/card.go`, `internal/connector/outgoing_mentions.go`, `internal/connector/outgoing_plaintext.go`, and related files own:
- Message send / reply / patch-card operations
- Reactions
- Upload of images and files
- Attachment download
- Scoped resource-root resolution

## 9. Runtime API And Bundled Skills

Alice exposes a local authenticated runtime API intended for bundled skills and thin runtime scripts.

Current HTTP surface:
- `POST /api/v1/messages/image`
- `POST /api/v1/messages/file`
- `GET|POST|PATCH|DELETE /api/v1/automation/tasks`
- `GET|POST /api/v1/goal` + pause/resume/complete/delete

There is no standalone text-send endpoint. Plain text is normally returned through the main reply pipeline.

Current safeguards:
- Bearer token auth
- Request-body size limit (1 MB)
- In-process auth rate limiting (120 req/min)
- Local uploads require readable, non-empty regular files and remain subject to Feishu size limits

Runtime-facing shell entrypoints:
- `alice runtime message ...`
- `alice runtime automation ...`
- `alice runtime goal ...`

Bundled skills shipped in the current tree:
- `skills/alice-message`
- `skills/alice-scheduler`
- `skills/alice-goal`

Runtime context is injected through environment variables (see [Runtime API Design](../explanation/runtime-api-design.md)).

## 10. Automation Subsystem

`internal/automation` persists tasks in bbolt and executes them in-process.

Current task scopes: `user`, `chat`
Current task actions: `send_text`, `run_llm`, `run_workflow`

Execution model:
- Due tasks are claimed on a periodic tick
- Long-lived system tasks are scheduled separately
- Task env inherits the same conversation context bridge used for interactive runs
- Workflow tasks call the same LLM backend but with workflow-specific agent names, env vars, and workspace hints

Built-in system tasks registered during bootstrap:
- Periodic session/runtime state flush
- Periodic campaign-repo reconcile

## 11. Configuration Model

The config model is pure multi-bot.

Important keys:
- `bots.<id>`
- `llm_profiles`
- `group_scenes.chat`, `group_scenes.work`
- `private_scenes.chat`, `private_scenes.work`
- `permissions`
- `runtime_http_addr`
- `workspace_dir`, `prompt_dir`, `codex_home`

Behavior worth calling out:
- `RuntimeConfigs()` derives missing bot paths and increments default runtime API ports across bots
- Each outer `llm_profiles` key is a stable runtime selector
- Provider-specific profile selectors still live inside each profile via the inner `profile` field
- Runtime permissions gate bundled skills and runtime API surfaces independently

## 12. Observability And Debugging

Current observability surfaces:
- Structured logs via `zerolog`
- Rotating log files via `lumberjack`
- Session usage counters stored in `session_state.json`
- `/status` powered by `statusview`
- Per-run markdown debug traces when `log_level=debug`

Debug traces include, when the backend exposes them:
- Provider, agent name, thread/session id, model/profile
- Rendered input, observed tool activity, final output or error

## 13. Extension Boundaries

The supported extension surfaces:
- `llm` provider adapters
- Prompt templates under `prompts/`
- Bundled skills under `skills/`
- Runtime API handlers
