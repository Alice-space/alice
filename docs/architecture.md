# Alice Runtime Architecture

[中文版本](./architecture.zh-CN.md)

This document describes the architecture that is actually implemented in the current repository. It is intentionally code-first: package names, runtime objects, and file paths match the live code under `cmd/connector`, `internal/`, `prompts/`, and `skills/`.

## 1. Process Model

Alice is a multi-bot runtime. One `alice` process can host multiple bots from one `config.yaml`.

At startup, the process:

1. Loads `config.yaml`
2. Expands `bots.*` into per-bot runtime configs
3. Verifies CLI auth where needed
4. Syncs embedded bundled skills into the local skill directories
5. Builds one `ConnectorRuntime` per bot
6. Runs all runtimes under one `RuntimeManager`

The main runtime object per bot is:

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
- `alice-headless`: runtime-only only; it may not start the Feishu connector

## 2. Bootstrap Path

The process entrypoint is `cmd/connector`.

Key bootstrap steps:

- `cmd/connector/root.go`
  Handles CLI flags, startup mode selection, config creation, PID locking, logging, auth preflight, bundled-skill sync, and runtime manager startup.
- `internal/config`
  Owns the pure multi-bot config model, path derivation, normalization, validation, and per-bot runtime expansion.
- `internal/bootstrap`
  Builds the per-bot runtime graph and wires cross-cutting features such as prompt loading, runtime API auth, campaign reconcile loops, and config hot reload.

`BuildRuntimeManager` expands `Config` into `[]Config` via `RuntimeConfigs()`, then builds one `ConnectorRuntime` for each bot.

Current hot-reload behavior:

- single-bot mode: partial config hot reload is supported
- multi-bot mode: hot reload is intentionally disabled; restart the process after config changes

## 3. Runtime Layout And Persisted State

Each bot gets its own runtime root under:

```text
${ALICE_HOME}/bots/<bot_id>/
```

Important per-bot paths:

- `workspace/`
  Bot workspace
- `prompts/`
  Optional prompt overrides for that bot
- `run/connector/automation.db`
  Persistent automation task store
- `run/connector/campaigns.db`
  Persistent lightweight campaign index
- `run/connector/session_state.json`
  Session aliases, provider thread ids, usage counters, work-thread metadata
- `run/connector/runtime_state.json`
  Mutable connector runtime state
- `run/connector/resources/scopes/<scope_type>/<scope_id>/`
  Downloaded inbound attachments and uploadable local artifacts scoped to the current conversation

The source tree also embeds:

- `prompts/`
- `skills/`
- `config.example.yaml`
- `prompts/SOUL.md.example`

Disk files override embedded prompt files when present; embedded assets are the fallback.

## 4. Package Map

Core packages:

- `cmd/connector`
  CLI entrypoint, `runtime` subcommands, and `skills sync`.
- `internal/bootstrap`
  Runtime construction, path resolution, auth checks, skill materialization, campaign reconcile bridging, and config reload.
- `internal/config`
  Config schema, validation, defaults, path derivation, and multi-bot expansion.
- `internal/connector`
  Feishu ingress, message normalization, scene routing, queueing, session serialization, interruption, prompt assembly, reply dispatch, attachment download, session persistence, and built-in commands.
- `internal/llm`
  Provider-agnostic backend contract plus provider adapters for `codex`, `claude`, `gemini`, and `kimi`.
- `internal/prompting`
  Template loader with disk-first / embedded-fallback behavior, `sprig` helpers, and compiled-template caching.
- `internal/runtimeapi`
  Local authenticated HTTP server and client used by bundled skills and runtime-facing shell scripts.
- `internal/automation`
  Task model, persistence, claiming, execution, system-task scheduling, and workflow dispatch.
- `internal/statusview`
  Aggregates usage and automation data for `/status`.
- `internal/platform/feishu`
  Feishu sender implementation, attachment I/O, bot self-info lookup, message lookup, and user-name resolution helpers.

Support packages:

- `internal/sessionctx`
  Session-context environment bridge for runtime API calls and bundled skills.
- `internal/runtimecfg`
  Helpers for scene-derived profile selection and thread-reply preference.
- `internal/sessionkey`
  Canonical session-key and visibility-key helpers.
- `internal/messaging`
  Narrow sender/uploader interfaces shared across connector and runtime API layers.
- `internal/storeutil`
  Shared bbolt helpers and string utilities.
- `internal/logging`
  Zerolog plus rotating file output configuration.
- `internal/buildinfo`
  Version reporting.

## 5. Inbound Message Pipeline

`internal/connector.App` owns the live Feishu connection and the per-bot job queue.

High-level flow:

1. Feishu delivers `im.message.receive_v1` over WebSocket.
2. `App` normalizes the event into a `Job`.
3. `routeIncomingJob` decides whether the message should be ignored, treated as a built-in command, handled as `chat`, or handled as `work`.
4. The job is queued and serialized by session.
5. If a newer job supersedes the current one for the same session, the in-flight run is interrupted.
6. `Processor` executes the accepted job.

Scene routing rules:

- group/topic-group chats can use `group_scenes.chat` and `group_scenes.work`
- work threads are identified by a trigger plus a stable work-scene session key
- if both scenes are disabled, Alice falls back to legacy `trigger_mode` / `trigger_prefix`
- built-in commands such as `/help`, `/status`, `/clear`, and `/stop` bypass the LLM path

## 6. Session Keys, Aliases, And Serialization

Alice routes and resumes work through canonical session keys plus aliases.

Common formats:

- `{receive_id_type}:{receive_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}`
- `{receive_id_type}:{receive_id}|scene:{scene}|thread:{thread_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}|message:{message_id}`

Special cases:

- work-scene seed key: `{receive_id_type}:{receive_id}|scene:work|seed:{source_message_id}`
- chat reset alias: `{chat_key}|reset:{message_id}`

What is persisted in `session_state.json`:

- provider thread id
- work-thread id alias
- session aliases
- usage counters
- last-message timestamp
- scope key for status aggregation

`internal/connector/runtime_store.go` keeps the live in-memory coordination state:

- latest version per session
- pending job per session
- active run cancellation handle
- per-session mutex for serialization
- superseded-version tracking

## 7. Prompt Assembly And LLM Execution

`internal/connector.Processor` is the execution core for one accepted job.

Before an LLM call it:

- loads and parses `SOUL.md` if needed
- downloads inbound attachments into the scoped resource directory
- derives runtime env vars for the current conversation
- prepares prompt text

Current prompt assets:

- `prompts/llm/initial_prompt.md.tmpl`
- `prompts/connector/bot_soul.md.tmpl`
- `prompts/connector/current_user_input.md.tmpl`
- `prompts/connector/reply_context.md.tmpl`
- `prompts/connector/runtime_skill_hint.md.tmpl`
- `prompts/connector/synthetic_mention.md.tmpl`

Important prompt behavior:

- first-turn or non-resumed runs render the current-user-input template and may append reply context, bot soul, and runtime-skill hints
- resumed provider threads send only the current user input; Alice relies on the provider-side thread/session to hold prior context
- `chat` runs can prepend `SOUL.md`; `work` runs intentionally skip bot-soul injection

The LLM layer is selected like this:

1. scene selects an outer `llm_profiles.<name>`
2. the outer profile chooses provider/model/profile/reasoning/personality/prompt prefix
3. `llm.MultiBackend` dispatches to the correct provider adapter

Currently supported providers:

- `codex`
- `claude`
- `gemini`
- `kimi`

## 8. Reply Dispatch

Alice distinguishes between:

- immediate acknowledgement
- streamed progress messages from the backend
- final replies
- file/image follow-ups

Current behavior:

- work-scene messages usually receive an immediate reaction or `收到！`
- backend progress messages are sent as threaded replies when possible
- final replies are posted via the reply dispatcher
- thread replies fall back to direct replies when Feishu does not support threaded replies for that target

`internal/connector/sender.go` and related files own:

- message send / reply / patch-card operations
- reactions
- upload of images and files
- attachment download
- scoped resource-root resolution

## 9. Runtime API And Bundled Skills

Alice exposes a local authenticated runtime API intended for bundled skills and thin runtime scripts.

Current HTTP surface:

- `POST /api/v1/messages/image`
- `POST /api/v1/messages/file`
- `GET|POST|PATCH|DELETE /api/v1/automation/tasks`
- `GET|POST|PATCH|DELETE /api/v1/campaigns`

There is no standalone text-send endpoint in the current runtime API. Plain text is normally returned through the main reply pipeline; the message API is for attachments and attachment captions.

Current safeguards:

- bearer token auth
- request-body size limit
- in-process auth rate limiting
- local uploads still require readable, non-empty regular files and remain subject to Feishu size limits

Runtime-facing shell entrypoints:

- `alice runtime message ...`
- `alice runtime automation ...`
- `alice runtime campaigns ...`

Bundled skills shipped in the current tree:

- `skills/alice-message`
- `skills/alice-scheduler`

Runtime context is injected through:

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`
- `ALICE_RUNTIME_BIN`
- `ALICE_RECEIVE_ID_TYPE`
- `ALICE_RECEIVE_ID`
- `ALICE_SOURCE_MESSAGE_ID`
- `ALICE_ACTOR_USER_ID`
- `ALICE_ACTOR_OPEN_ID`
- `ALICE_CHAT_TYPE`
- `ALICE_SESSION_KEY`

## 10. Automation Subsystem

`internal/automation` persists tasks in bbolt and executes them in-process.

Current task scopes:

- `user`
- `chat`

Current task actions:

- `send_text`
- `run_llm`
- `run_workflow`

Execution model:

- due tasks are claimed on a periodic tick
- long-lived system tasks are scheduled separately
- task env inherits the same conversation context bridge used for interactive runs
- workflow tasks call the same LLM backend but with workflow-specific agent names, env vars, and workspace hints

Built-in system tasks registered during bootstrap:

- periodic session/runtime state flush
- periodic campaign-repo reconcile

## 11. Removed Campaign Layer

The former in-repo campaign / code-army orchestration layer is no longer part of this repository.
Current Alice focuses on scene routing, backend execution, runtime API, and generic automation primitives.

## 12. Configuration Model

The config model is pure multi-bot.

Important keys:

- `bots.<id>`
- `llm_profiles`
- `group_scenes.chat`
- `group_scenes.work`
- `permissions`
- `runtime_http_addr`
- `workspace_dir`
- `prompt_dir`
- `codex_home`

Behavior worth calling out:

- `RuntimeConfigs()` derives missing bot paths and increments default runtime API ports across bots
- each outer `llm_profiles` key is a stable runtime selector
- provider-specific profile selectors still live inside each profile via the inner `profile` field
- runtime permissions gate bundled skills and runtime API surfaces independently

Default execution posture for work-capable profiles is intentionally permissive. For example, Claude and Kimi defaults reflect their current non-interactive CLI behavior, and work-oriented Codex profiles commonly run with `danger-full-access` plus `never`. Operators who need a stricter posture should override the per-profile permissions explicitly.

## 13. Observability And Debugging

Current observability surfaces:

- structured logs via `zerolog`
- rotating log files via `lumberjack`
- session usage counters stored in `session_state.json`
- `/status` powered by `statusview`
- per-run markdown debug traces when `log_level=debug`

Debug traces include, when the backend exposes them:

- provider
- agent name
- thread/session id
- model/profile
- rendered input
- observed tool activity
- final output or error

## 14. Extension Boundaries

The supported extension surfaces in the current codebase are:

- `llm` provider adapters
- prompt templates under `prompts/`
- bundled skills under `skills/`
- runtime API handlers

Notably absent from the current implementation:

- no active `internal/memory` package
- no runtime memory API
- no in-repo code-army / campaign orchestration layer
- no business-logic MCP server; only backward-compatible session env naming remains
