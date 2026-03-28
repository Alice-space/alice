# CodeArmy Isolated Debug Runbook

See the Chinese guide for the full workflow and caveats:

- `docs/codearmy-isolated-debug.zh-CN.md`

Recommended pattern:

1. Build temporary `alice` and `alice-headless` binaries from the latest source.
2. Use a fresh `ALICE_HOME` with its own config, runtime port, token, automation db, and campaign db.
3. Connector startup mode is explicit: use `--feishu-websocket` for the real Feishu connector, or `--runtime-only` for local runtime/API-only execution.
4. Start isolated runtimes with `alice-headless --runtime-only`.
5. Never let temporary or separate `ALICE_HOME` runtimes connect to the real Feishu WebSocket. If startup logs say `feishu-codex connector started (long connection mode)`, stop immediately.
6. Bootstrap a small local repo into a real CodeArmy campaign.
7. Inspect all three surfaces together:
   - campaign repo artifacts
   - runtime campaign / automation API state
   - runtime log
8. If you changed embedded skills, run `alice skills sync` before treating a new run as representative.
