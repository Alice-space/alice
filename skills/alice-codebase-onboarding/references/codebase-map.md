# Alice Codebase Map

Target repository: `${ALICE_REPO:-$HOME/alice}`  
Language: Go  
Purpose: Feishu bot connector that forwards user messages to Codex and sends replies back.

## Entry and bootstrap

1. `cmd/connector/main.go`
- Parse `-c/--config` path.
- Load YAML config via `internal/config`.
- Auto-link bundled repo skills into `$CODEX_HOME/skills`.
- Optional MCP registration for `alice-mcp-server`.
- Build runtime via `internal/bootstrap/connector_runtime.go`.
- Start long-connection app loop.

2. `internal/bootstrap/connector_runtime.go`
- Delegate assembly to `connectorRuntimeBuilder` (`internal/bootstrap/connector_runtime_builder.go`).
- Keep only stable bootstrap-facing APIs: provider factory, MCP registration, runtime build entry.

3. `cmd/alice-mcp-server/main.go`
- Load same config.
- Reuse bootstrap path helpers.
- Start MCP stdio server (`internal/mcpserver`).

## Runtime chain

1. Event intake:
- `internal/connector/app.go` creates WS client and dispatches `im.message.receive_v1`.

2. Queue and steering:
- `internal/connector/app_queue.go`
- Jobs enter bounded queue (`queue_capacity`).
- Session key prioritizes chat/thread context.
- Per-session mutex guarantees serial processing.

3. Job processing:
- `internal/connector/processor.go`
- Build prompt/context (reply context + memory + mention context).
- Invoke backend (`internal/llm/codex/codex.go` for Codex provider).
- Delegate reply/send downgrade rules to `internal/connector/reply_dispatcher.go`.

4. Runtime/memory persistence:
- Runtime queue/session metadata in `.memory/runtime_state.json`.
- Session thread metadata in `.memory/session_state.json`.
- Long-term memory in `.memory/MEMORY.md`.
- Daily memory in `.memory/daily/YYYY-MM-DD.md`.

## Operationally important files

- `config.example.yaml`: baseline config template (includes `codex_mcp_*`).
- `scripts/update-self-and-sync-skill.sh`: canonical self-update command.
- `skills/`: bundled skills that are auto-linked to `$CODEX_HOME/skills` on connector startup.
- `docs/architecture.md` / `docs/architecture.zh-CN.md`: architecture and refactor status.
