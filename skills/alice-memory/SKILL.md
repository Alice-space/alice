---
name: alice-memory
description: Inspect or update Alice memory for the current chat through Alice's local runtime HTTP API. Use when the user wants to view current session memory, edit long-term memory, append a daily note/summary, or confirm which memory files are active for the conversation.
---

# Alice Memory

Use `scripts/alice-memory.sh` instead of editing `.memory` paths by hand. The script reads the current session context and runtime auth from environment variables injected by Alice.

## Commands

- Inspect current memory context:
  `scripts/alice-memory.sh context`
- Overwrite current session long-term memory:
  `scripts/alice-memory.sh write-session '偏好中文回复；关注稳定性。'`
- Overwrite global long-term memory:
  `scripts/alice-memory.sh write-global '所有会话默认先给结论。'`
- Append a daily summary entry for the current session:
  `scripts/alice-memory.sh daily-summary '今天确认了新的部署窗口和风险项。'`

## Workflow

1. Run `context` first when you need to understand which files are in play.
2. Prefer `write-session` for chat-specific preferences or constraints.
3. Use `write-global` only for stable cross-chat rules.
4. Use `daily-summary` for time-bound notes that should stay in the per-day log.

## Reply Pattern

- Mention whether you inspected, rewrote, or appended memory.
- Include the affected file path from the API response when relevant.
- Summarize the updated memory content briefly instead of dumping the whole file unless the user asked for it.
