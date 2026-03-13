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
  Backend factory plus provider adapters for `codex`, `claude`, and `kimi`.
- `internal/prompting`
  File-backed prompt/template renderer using Go templates plus `sprig`.
- `internal/memory`
  Scoped memory storage and prompt assembly, plus HTTP-friendly snapshot/update helpers.
- `internal/automation`
  Task persistence/execution for `send_text`, `run_llm`, and `run_workflow`.
- `internal/runtimeapi`
  Local authenticated HTTP server and client used by skills.
- `skills/`
  Runtime-facing operational skills such as `alice-memory`, `alice-message`, `alice-scheduler`, and `alice-code-army`.

## Prompt system

Prompts are no longer embedded as large string literals in code paths.

- Prompt root: `prompts/`
- LLM initial prompt template: `prompts/llm/initial_prompt.md.tmpl`
- Memory prompt template: `prompts/memory/prompt.md.tmpl`
- Code Army phase templates:
  - `prompts/code_army/manager.md.tmpl`
  - `prompts/code_army/worker.md.tmpl`
  - `prompts/code_army/reviewer.md.tmpl`

`internal/prompting` loads templates from disk, caches compiled templates with `xxhash`, and exposes `sprig` helpers for richer prompt logic.

## Backend abstraction

The backend factory now supports:

- `codex`
- `claude`
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
- `/api/v1/workflows/code-army/status`
  Inspect `code_army` workflow state for the current conversation.

Configuration:

- `runtime_http_addr`
- `runtime_http_token`

If `runtime_http_token` is omitted, the connector generates a per-process token and injects it into agent environments.

## Skills model

Operational modules are now exposed as skills instead of being reachable only through MCP tools:

- `skills/alice-memory`
  Inspect/update current chat memory through `alice-connector runtime memory ...`.
- `skills/alice-message`
  Send text/image/file through `alice-connector runtime message ...`.
- `skills/alice-scheduler`
  Manage automation tasks and workflow status through `alice-connector runtime automation ...` and `workflow ...`.
- `skills/alice-code-army`
  Now composes with `alice-scheduler` instead of invoking MCP automation tools directly.

These skills rely on:

- `ALICE_RUNTIME_API_BASE_URL`
- `ALICE_RUNTIME_API_TOKEN`
- `ALICE_RUNTIME_BIN`
- existing session env such as `ALICE_MCP_RECEIVE_ID`, `ALICE_MCP_SESSION_KEY`, and related actor metadata

## MCP strategy

Alice no longer exposes business operations through MCP.

Current posture:

- Skills + runtime HTTP are the primary path for memory/scheduling/workflow/message operations.
- Bundled skills call the same `alice-connector` binary with `runtime ...` arguments.
- The remaining `mcp` naming is limited to session-context env keys such as `ALICE_MCP_RECEIVE_ID`, which are still used as stable runtime context variables.

This keeps legacy Codex MCP behavior alive while reducing duplication between skills and MCP handlers.

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
- `code_army` phase agents (`manager`, `worker`, `reviewer`)
- backend adapters that can surface tool activity (`codex`, `kimi`, and partial `claude`)

## Library adoption in this refactor

Actively used:

- `github.com/Masterminds/sprig/v3`
- `github.com/cespare/xxhash/v2`
- `github.com/evanphx/json-patch/v5`
- `github.com/gin-gonic/gin`
- `github.com/go-resty/resty/v2`
- `github.com/oklog/run`
- `github.com/oklog/ulid/v2`
- `github.com/rs/zerolog`
- `github.com/spf13/cobra`
- `go.etcd.io/bbolt`
- `gopkg.in/natefinch/lumberjack.v2`
- `gopkg.in/yaml.v3`

## End-to-end flow

1. Feishu event enters `internal/connector`.
2. Connector serializes work per session and builds per-run env.
3. Env includes current chat context plus runtime HTTP auth.
4. LLM backend renders prompt templates from disk and runs `codex`/`claude`/`kimi`.
5. Skills invoked by the agent call `alice-connector runtime ...`, which then talks to the runtime HTTP API.
6. Runtime HTTP API operates memory, automation, and message sending using the same session context.
7. Automation tasks are persisted in `automation.db` through `bbolt`, migrating legacy JSON snapshots on first open.
8. Runtime logs are emitted through `zerolog`, with optional file rotation handled by `lumberjack`.
9. Debug traces record each agent call in markdown for replay/audit.
