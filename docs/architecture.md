# Architecture and Refactor Plan

[中文版本](./architecture.zh-CN.md)

This document defines the target architecture for `alice` and tracks ongoing refactor slices.

## Design goals

- High cohesion: each package should own one clear responsibility.
- Low coupling: business flow should depend on interfaces, not concrete transport details.
- Recoverability: restart and runtime-state restoration must stay deterministic.
- Operability: deployment and runbook behavior must stay stable during refactors.

## Bounded modules

- `cmd/connector`: process bootstrap only (config load, dependency wiring, run loop).
- `internal/connector`: Feishu event intake, queueing, per-session sequencing, reply orchestration.
- `internal/llm/codex`: Codex CLI invocation and stream parsing.
- `internal/memory`: long-term and daily memory persistence.
- `internal/automation`: task scheduling, persistence, and execution engine.
- `cmd/alice-mcp-server` + `internal/mcpserver`: MCP server entry and handlers.

## Dependency rules

- `cmd/*` may depend on `internal/*`; `internal/*` must not depend on `cmd/*`.
- `internal/connector` may call `internal/llm`, `internal/memory`, `internal/automation` via interfaces.
- Feishu SDK usage should stay in connector/sender-facing adapters.
- Runtime mutable state should be centralized in dedicated state components.

## Runtime flow

1. Feishu WS event enters `App` (`internal/connector/app.go`).
2. Queue/session steering is handled by dedicated runtime helpers (`internal/connector/app_queue.go`).
3. Worker serializes processing by session-level mutex.
4. `Processor` builds prompt/context, invokes backend, and delegates reply downgrade policy to `replyDispatcher` (`internal/connector/reply_dispatcher.go`).
5. Session/runtime state and memory are flushed asynchronously.

## Refactor status (this iteration)

- Introduced `runtimeStore` (`internal/connector/runtime_store.go`) to centralize mutable runtime state:
  - `latest` session versions
  - `pending` jobs
  - group `mediaWindow`
  - per-session mutex map
  - runtime-state persistence metadata
- Updated `App` and related runtime/media-window paths to use the centralized store.
- Split connector orchestration by responsibility:
  - `internal/connector/app.go`: websocket lifecycle and worker loop
  - `internal/connector/app_queue.go`: session routing, queueing, and active-run steering
- Extracted `replyDispatcher` (`internal/connector/reply_dispatcher.go`) so transport fallback policy is no longer embedded in `Processor`.
- Refactored connector bootstrap into staged builder steps in `internal/bootstrap/connector_runtime_builder.go`.
- Removed deprecated interactive card patch path (`PatchCard`) from `Sender` abstractions and concrete sender implementation.

## Next slices

1. Split `Processor` into pipeline stages (`context build`, `backend invoke`, `reply render`) for better test isolation.
2. Clarify memory ownership by introducing a coordinator boundary between prompt assembly, idle summaries, and persistence hooks.
3. Continue shrinking `cmd/connector/main.go` into thinner startup wiring as more builder slices stabilize.
