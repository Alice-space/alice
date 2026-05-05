# Message Pipeline

This page walks through the full lifecycle of an incoming Feishu message — from WebSocket delivery to the final reply. Understanding this pipeline helps debug routing issues and tune behavior.

## Overview

```
Feishu WebSocket
  └─ App (job queue)
      └─ Processor (execution)
          └─ LLM Backend (subprocess)
              └─ Reply Dispatcher (back to Feishu)
```

## 1. WebSocket Reception

Alice establishes a long connection to Feishu's WebSocket endpoint. When a user sends a message the bot can see, Feishu delivers an `im.message.receive_v1` event over this connection.

The event contains:
- Sender identity (open_id, user_id, name)
- Message content (text, attachments, mentions)
- Chat context (chat_id, chat_type, thread_id if in a thread)
- Bot identity (which bot received this)

## 2. Job Creation

The raw event is normalized into a `Job` struct. This step:
- Extracts mentioned users
- Resolves the receive ID type (`chat_id`, `open_id`, etc.)
- Sets the bot's configured LLM profile, scene, and reply preferences
- Generates a session key and resource scope key
- Attaches a monotonic version number

## 3. Routing

`routeIncomingJob` decides what to do with the job:

### Built-in Commands
If the message starts with `/help`, `/status`, `/clear`, `/stop`, `/session`, `/cd`, `/ls`, or `/pwd`, it's handled by the connector directly — no LLM call. See [Use Built-in Commands](../how-to/use-builtin-commands.md).

### Work Scene
If `group_scenes.work.enabled` and the message contains the `trigger_tag` (e.g., `#work`) after the @bot mention, the job is routed to the work scene. Work jobs use the work-scoped session key and LLM profile.

### Chat Scene
If `group_scenes.chat.enabled`, all other messages are routed to chat. Chat jobs use the chat-scoped session key and LLM profile.

### Legacy Fallback
If both scenes are disabled, Alice falls back to matching `trigger_mode` and `trigger_prefix`.

## 4. Queue and Serialization

Each session has a mutex that serializes execution:

- **Active run exists** → Try provider-native steer first (inject new input into running session)
- **Native steer unavailable** → Queue the new job. A newer job supersedes an older queued job.
- **No active run** → Accept the job and dispatch to the Processor.

The runtime store (`runtime_store.go`) keeps in-memory coordination state:
- Latest version per session
- Pending queued job
- Active run cancellation handle
- Per-session mutex

## 5. Pre-LLM Processing

Before the LLM call, the Processor:

1. Loads and parses `SOUL.md` (chat only) — separates YAML frontmatter from Markdown body
2. Downloads inbound attachments into the scoped resource directory
3. Derives runtime environment variables for the conversation
4. Prepares the rendered prompt text

### Session State Check

Alice checks `session_state.json`:
- If a provider thread ID exists, the backend call resumes that thread
- If the session was recently active, context from the last turn is available

## 6. LLM Execution

The Processor builds a `RunRequest` and dispatches it to the LLM backend:

```
RunRequest {
    ThreadID       → from session state (empty = new session)
    UserText       → rendered prompt
    Provider       → from llm_profile
    Model          → from llm_profile
    ReasoningEffort → from llm_profile
    WorkspaceDir   → per-bot workspace
    ExecPolicy     → sandbox + approval settings
    Env            → per-bot + process env
    OnProgress     → stream progress updates to Feishu
}
```

The backend spawns the provider CLI as a subprocess and streams output. Progress updates are sent to Feishu as status card patches.

## 7. Reply Dispatch

When the LLM finishes, Alice processes the reply:

### Content Processing
- If the reply matches `no_reply_token`, stay silent
- If `output_contract` is configured in SOUL.md, strip hidden tags
- Apply formatting for Feishu (rich text, @mentions)

### Threading
- **Work scene with `create_feishu_thread: true`**: Reply is posted in a Feishu thread
- **Chat scene with `create_feishu_thread: false`**: Reply is posted as a top-level message
- **Thread replies**: When Feishu supports it, reply is threaded. Falls back to direct reply otherwise.

### Immediate Feedback
Before the LLM starts, Alice sends immediate acknowledgement:
- `immediate_feedback_mode: "reaction"` → Adds a reaction emoji to the source message
- `immediate_feedback_mode: "reply"` → Sends explicit `收到！` reply

## 8. Post-Run

- Session state is persisted to `session_state.json` (thread ID, usage counters, timestamp)
- Downloaded attachments remain in the scoped resource directory
- Runtime state is flushed periodically

## Key Invariants

1. **At most one LLM run per session at a time** — enforced by per-session mutex
2. **Newer messages supersede queued, not active** — `/stop` is the only way to interrupt a running LLM
3. **Session state is disk-backed** — survives process restart
4. **Attachments are scoped** — each conversation has its own resource directory
