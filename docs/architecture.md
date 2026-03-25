# Alice Runtime Architecture

[中文版本](./architecture.zh-CN.md)

This document describes the current target architecture after the runtime/skills refactor shipped on March 13, 2026.

## Design goals

- Keep the connector process focused on orchestration, not prompt literals or tool-specific business logic.
- Treat LLM backends as interchangeable adapters.
- Move chat-scoped operational capabilities into reusable skills that talk to Alice through a local HTTP API.
- Keep runtime skills and the local HTTP API as the only supported expansion surface.
- Make debug traces auditable: every agent call should record markdown input/output/tool activity.

## Component map

- `cmd/connector`
  Starts the Feishu connector, automation engine, local runtime HTTP API, and the `runtime` skill-facing subcommands in one binary.
- `internal/connector`
  Handles Feishu websocket intake, queueing, session serialization, interruption, reply dispatch, and per-run env injection.
- `internal/llm`
  Backend factory plus provider adapters for `codex`, `claude`, `gemini`, and `kimi`.
- `internal/prompting`
  File-backed prompt/template renderer using Go templates plus `sprig`.
- `internal/memory`
  Scoped memory storage and prompt assembly, plus HTTP-friendly snapshot/update helpers.
- `internal/automation`
  Task persistence/execution for `send_text` and `run_llm`.
- `internal/runtimeapi`
  Local authenticated HTTP server and client used by skills.
- `internal/statusview`
  Aggregates scoped usage, automation, and campaign snapshots for `/status` style views.
- `internal/imagegen`
  Image generation and editing adapters plus output safety guards.
- `internal/messaging`
  Narrow sender/uploader port definitions shared by runtime and connector layers.
- `internal/storeutil`
  Shared helpers for bbolt open/version/parent-dir handling.
- `skills/`
  Runtime-facing operational skills such as `alice-memory`, `alice-message`, `alice-scheduler`, and `alice-code-army`.

## Prompt system

Prompts are no longer embedded as large string literals in code paths.

- Prompt root: `prompts/`
- LLM initial prompt template: `prompts/llm/initial_prompt.md.tmpl`
- Memory prompt template: `prompts/memory/prompt.md.tmpl`
- Connector context templates:
  - `prompts/connector/current_user_input.md.tmpl`
  - `prompts/connector/reply_context.md.tmpl`
  - `prompts/connector/runtime_skill_hint.md.tmpl`
  - `prompts/connector/idle_summary.md.tmpl`
  - `prompts/connector/synthetic_mention.md.tmpl`
- Campaign repo workflow templates:
  - `prompts/campaignrepo/executor_dispatch.md.tmpl`
  - `prompts/campaignrepo/reviewer_dispatch.md.tmpl`

`internal/prompting` loads templates from disk, caches compiled templates with `xxhash`, and exposes `sprig` helpers.

Current behavior:

- `App`, `Processor`, and LLM runners accept an injected loader when bootstrap provides one.
- If no loader is injected, they fall back to `internal/prompting.DefaultLoader()`, which searches upward for the repo `prompts/` directory.
- Non-test business prompts now live in template files only; string-literal fallbacks have been removed.

## Backend abstraction

The backend factory now supports:

- `codex`
- `claude`
- `gemini`
- `kimi`

Key behaviors:

- Shared high-level `llm.Backend` contract remains stable.
- `kimi` uses the local `kimi` CLI in print/stream-json mode and reuses Alice session ids as Kimi session ids.

## Runtime HTTP API

The connector now exposes a local authenticated HTTP API, intended for skills and thin proxies.

Current API groups:

- `/api/v1/messages/*`
  Send text/image/file into the current chat context.
- `/api/v1/memory/*`
  Inspect memory context, rewrite long-term memory, append daily summaries.
- `/api/v1/automation/*`
  Create/list/get/patch/delete scheduled tasks.
- `/api/v1/campaigns/*`
  Manage code-army campaigns, trials, guidance, reviews, and pitfalls in the current conversation scope.

Configuration:

- `runtime_http_addr`
- `runtime_http_token`

If `runtime_http_token` is omitted, the connector generates a per-process token and injects it into agent environments.

Current runtime hardening:

- Request bodies are capped to a small JSON-sized limit because runtime API endpoints pass paths/metadata, not bulk file data.
- Bearer-authenticated requests are rate-limited in-process to reduce brute-force pressure on the local token.
- Attachment payloads are uploaded from local paths instead of posting raw file bytes through the API.

## Session key routing

Alice routes work by a canonical session key with optional aliases.

Common format:

- `{receive_id_type}:{receive_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}`
- `{receive_id_type}:{receive_id}|scene:{scene}|thread:{thread_id}`
- `{receive_id_type}:{receive_id}|scene:{scene}|message:{message_id}`

Special cases:

- work-scene seed key: `{receive_id_type}:{receive_id}|scene:work|seed:{source_message_id}`
- chat reset alias: append `|reset:{message_id}` when rotating a chat-scoped session

These aliases let Alice resume the same logical session when later Feishu events arrive as root messages, replies, or thread replies.

## Codex execution policy defaults

The bundled default for Codex work-scene runs is intentionally powerful:

- sandbox: `danger-full-access`
- approval mode: `never`

This is a deliberate tradeoff so work-scene agents can edit the workspace and run local tooling without interactive approval loops. It should only be enabled for trusted local workspaces and trusted bundled skills; operators who need a stricter posture should override the Codex scene policy in config.

## Skills model

Operational modules are now exposed as skills instead of being reachable only through MCP tools:

- `skills/alice-memory`
  Inspect/update current chat memory through `alice runtime memory ...`.
- `skills/alice-message`
  Send image/file attachments through `alice runtime message ...`; plain text is forwarded by the main reply pipeline.
- `skills/alice-scheduler`
  Manage automation tasks through `alice runtime automation ...`.
- `skills/alice-code-army`
  Acts as the long-running optimization orchestration skill, using `alice runtime campaigns ...` as a lightweight campaign index while the campaign repository's markdown/frontmatter structure serves as the primary source of truth; Alice backend periodically reconciles the campaign repo to refresh live-report and wake tasks, while GitLab/cluster integrations remain optional mirrors and execution surfaces.

These skills rely on:

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`
- `ALICE_RUNTIME_BIN`
- existing session env such as `ALICE_MCP_RECEIVE_ID`, `ALICE_MCP_SESSION_KEY`, and related actor metadata

Bundled runtime skill scripts resolve the runtime binary in this order:

1. `ALICE_RUNTIME_BIN`
2. `${ALICE_HOME:-$HOME/.alice}/bin/alice`
3. `alice` from `PATH`

## MCP strategy

Alice no longer exposes business operations through MCP.

Current posture:

- Skills + runtime HTTP are the primary path for memory/scheduling/campaign/message operations.
- Bundled skills call the same `alice` binary with `runtime ...` arguments.
- The remaining `mcp` naming is limited to session-context env keys such as `ALICE_MCP_RECEIVE_ID`, which are still used as stable runtime context variables.

This preserves stable session env keys while avoiding duplicated business handlers across skills and core runtime code.

## Debug traces

When `log_level=debug`, every agent call emits a markdown trace containing:

- provider
- agent name
- thread/session id
- model/profile
- rendered input
- observed tool calls
- final output or error

This applies to:

- normal assistant runs
- scheduler-triggered `run_llm`
- backend adapters that can surface tool activity (`codex`, `kimi`, and partial `claude`)

## Library adoption in this refactor

Actively used:

- `github.com/Masterminds/sprig/v3`
- `github.com/cespare/xxhash/v2`
- `github.com/evanphx/json-patch/v5`
- `github.com/go-co-op/gocron/v2`
- `github.com/go-playground/validator/v10`
- `github.com/gin-gonic/gin`
- `github.com/go-resty/resty/v2`
- `github.com/oklog/run`
- `github.com/oklog/ulid/v2`
- `github.com/rs/zerolog`
- `github.com/spf13/cobra`
- `github.com/cenkalti/backoff/v4`
- `github.com/cyphar/filepath-securejoin`
- `go.etcd.io/bbolt`
- `gopkg.in/natefinch/lumberjack.v2`
- `gopkg.in/yaml.v3`

## End-to-end flow

1. Feishu event enters `internal/connector`.
2. Connector serializes work per session and builds per-run env.
3. Env includes current chat context plus runtime HTTP auth.
4. LLM backend renders prompt templates from disk and runs `codex`/`claude`/`kimi`.
5. Skills invoked by the agent call `alice runtime ...`, which then talks to the runtime HTTP API.
6. Runtime HTTP API operates memory, automation, campaign state, and message sending using the same session context.
7. Automation tasks are persisted in `automation.db` through `bbolt`.
8. Runtime logs are emitted through `zerolog`, with optional file rotation handled by `lumberjack`.
9. Debug traces record each agent call in markdown for replay/audit.
