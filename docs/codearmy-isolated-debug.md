# CodeArmy Isolated Debug Runbook

See the Chinese guide for the full workflow and caveats:

- `docs/codearmy-isolated-debug.zh-CN.md`

Recommended pattern:

1. Build temporary `alice` and `alice-headless` binaries from the latest source.
2. Use a fresh `ALICE_HOME` with its own config, runtime port, token, automation db, and campaign db.
3. Bootstrap a small local repo into a real CodeArmy campaign.
4. Inspect all three surfaces together:
   - campaign repo artifacts
   - runtime campaign / automation API state
   - runtime log
5. If you changed embedded skills, run `alice skills sync` before treating a new run as representative.
