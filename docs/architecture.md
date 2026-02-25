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
2. Event is normalized into a `Job`, routed by session key, and queued.
3. Worker serializes processing by session-level mutex.
4. `Processor` builds prompt/context, invokes backend, emits progress/final reply via sender fallback chain.
5. Session/runtime state and memory are flushed asynchronously.

## Refactor status (this iteration)

- Introduced `runtimeStore` (`internal/connector/runtime_store.go`) to centralize mutable runtime state:
  - `latest` session versions
  - `pending` jobs
  - group `mediaWindow`
  - per-session mutex map
  - runtime-state persistence metadata
- Updated `App` and related runtime/media-window paths to use the centralized store.
- Removed deprecated interactive card patch path (`PatchCard`) from `Sender` abstractions and concrete sender implementation.

## Next slices

1. Extract message send/reply fallback policies into a dedicated transport policy component.
2. Split `Processor` into pipeline stages (`context build`, `backend invoke`, `reply render`) for better test isolation.
3. Refactor `cmd/connector/main.go` wiring into composable initializers/builders.
