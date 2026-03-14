# Alice Codebase Map

Target repository: `${ALICE_REPO:-$HOME/alice}`  
Language: Go  
Purpose: Feishu bot connector that forwards user messages to Codex and sends replies back.

## Entry and bootstrap

1. `cmd/connector/main.go`
- Minimal process entry that delegates to the Cobra root command.

2. `cmd/connector/root.go`
- Parse `-c/--config` path (default `${ALICE_HOME:-$HOME/.alice}/config.yaml`) and optional `--pid-file`.
- Load YAML config via `internal/config`.
- Materialize embedded bundled skills into `$CODEX_HOME/skills` (default `${ALICE_HOME:-$HOME/.alice}/.codex/skills`).
- Build runtime via `internal/bootstrap/connector_runtime.go`.
- Start long-connection app loop.

3. `cmd/connector/runtime_root.go`
- Expose `alice runtime ...` subcommands for bundled skills.
- Load runtime HTTP auth and stable session context from env.
- Reuse `internal/runtimeapi.Client` instead of shelling out curl or hand-written HTTP.

4. `internal/bootstrap/connector_runtime.go`
- Delegate assembly to `connectorRuntimeBuilder` (`internal/bootstrap/connector_runtime_builder.go`).
- Keep only stable bootstrap-facing APIs: provider factory and runtime build entry.

5. `internal/bootstrap/connector_runtime_builder.go`
- Create the shared prompt loader.
- Assemble sender, processor, app, automation engine, memory manager, and runtime HTTP server.
- Inject runtime API env plus the resolved runtime binary path.

## Runtime chain

1. Event intake:
- `internal/connector/app.go` creates WS client and dispatches `im.message.receive_v1`.
- Builtin slash commands such as `/help` and `/codearmy status [state_key]` are allowed through before normal group trigger rewriting.

2. Queue and steering:
- `internal/connector/app_queue.go`
- Jobs enter bounded queue (`queue_capacity`).
- Session key prioritizes chat/thread context.
- Per-session mutex guarantees serial processing.
- `internal/connector/media_window.go` caches recent group context and can synthesize an `@bot` follow-up job from templates.

3. Job processing:
- `internal/connector/processor.go`
- Short-circuit builtin commands before LLM.
- Build prompt/context (current user input + reply context + runtime skill hint + memory).
- Invoke backend (`internal/llm/codex/codex.go` for Codex provider).
- Delegate reply/send downgrade rules to `internal/connector/reply_dispatcher.go`.

4. Prompt loading:
- `internal/prompting/loader.go`
- `internal/prompting/default_loader.go`
- Prompt files live under `prompts/`.
- Connector/business prompts now come from template files rather than code literals.

5. Runtime/memory persistence:
- Runtime queue/session metadata in `${ALICE_HOME:-$HOME/.alice}/memory/runtime_state.json`.
- Session thread metadata in `${ALICE_HOME:-$HOME/.alice}/memory/session_state.json`.
- Long-term memory in `${ALICE_HOME:-$HOME/.alice}/memory/MEMORY.md`.
- Daily memory in `${ALICE_HOME:-$HOME/.alice}/memory/daily/YYYY-MM-DD.md`.

## Operationally important files

- `config.example.yaml`: baseline config template (includes `runtime_http_*`).
- `scripts/update-self-and-sync-skill.sh`: canonical self-update command.
- `skills/alice-codebase-onboarding/scripts/update-self-and-sync-skill.sh`: wrapper that delegates to the canonical updater.
- `skills/`: bundled skills that are auto-linked to `$CODEX_HOME/skills` on connector startup.
- `docs/architecture.md` / `docs/architecture.zh-CN.md`: architecture and refactor status.
- `docs/feishu-message-flow.zh-CN.md`: detailed runtime message path.
