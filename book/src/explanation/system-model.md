# System Model

This page explains the fundamental concepts behind Alice: multi-bot architecture, scene routing, sessions, and startup modes. Understanding these helps you configure and troubleshoot effectively.

## What Alice Is (And Isn't)

Alice is a **connector**, not a bot framework. It doesn't implement chat logic, NLU, or custom integrations directly. Instead, it:

1. Receives messages from Feishu
2. Decides which LLM backend to call and how
3. Calls the LLM CLI as a subprocess
4. Sends the response back to Feishu

The "intelligence" lives in the LLM backend (Codex, Claude, etc.). Alice handles the plumbing: routing, queuing, session management, attachment I/O, and progress display.

## Multi-Bot Model

One `alice` process can host multiple independent bots from a single `config.yaml`:

```yaml
bots:
  engineering_bot:
    feishu_app_id: "cli_11111"
    # ...
  support_bot:
    feishu_app_id: "cli_22222"
    # ...
```

Each bot has its own:
- **Runtime directory** (`~/.alice/bots/<bot_id>/`)
- **Workspace**, prompts, and SOUL.md
- **Feishu credentials** (App ID, App Secret)
- **LLM profiles** — can use different providers and models
- **Scene configuration** — independent chat/work routing
- **Runtime API port** — auto-incremented (7331, 7332, ...)

Bots share:
- The same process and worker pool
- `CODEX_HOME` by default (can be overridden per bot)

### Bot Directory Layout

```
~/.alice/bots/<bot_id>/
├── workspace/                        # Agent workspace
├── prompts/                          # Prompt template overrides
├── SOUL.md                           # Bot persona
└── run/connector/
    ├── automation.db                 # Persistent task store (bbolt)
    ├── campaigns.db                  # Campaign index (bbolt)
    ├── session_state.json            # Session aliases, usage counters
    ├── runtime_state.json            # Mutable runtime state
    └── resources/scopes/             # Downloaded attachments, artifacts
```

## Scene Routing

Every incoming group message goes through a decision tree:

```
Incoming Message
  │
  ├─ Is it a built-in command? (/help, /status, /stop, /clear, /session)
  │   └─ Yes → Handle directly, no LLM involved
  │
  ├─ Does it match the work trigger? (@Bot #work ...)
  │   └─ Yes → Route to work scene
  │
  ├─ Is the chat scene enabled?
  │   └─ Yes → Route to chat scene
  │
  └─ Both scenes disabled?
      └─ Fall back to legacy trigger_mode (at / prefix / all)
```

### Scenes vs Legacy Triggers

The legacy `trigger_mode` (at/prefix/all) is a simple gate: it decides whether to accept a message or ignore it. If accepted, there's one LLM pipeline.

Scenes go further: they assign different LLM profiles, session scopes, thread behaviors, and SOUL.md treatment per scene. New deployments should always use scenes.

## Session Management

A **session** is the LLM's context window. Alice decides when to start a new session vs. when to continue an existing one.

### Session Keys

Alice identifies sessions with canonical keys:

| Format | Example |
|--------|---------|
| `{receive_id_type}:{receive_id}` | `chat_id:oc_123` |
| `{key}|scene:{scene}` | `chat_id:oc_123|scene:chat` |
| `{key}|scene:{scene}|thread:{thread_id}` | `chat_id:oc_123|scene:work|thread:om_456` |

### Session Scope

`session_scope` controls when sessions are created and reused:

| Scope | Behavior |
|-------|----------|
| `per_chat` | One session for the entire group/DM |
| `per_thread` | One session per Feishu thread |
| `per_user` | (DM only) One session per user |
| `per_message` | (DM only) New session for every message |

### Session Persistence

Alice persists session metadata to `session_state.json`:
- Provider thread ID (for resuming with the backend)
- Session aliases
- Usage counters
- Last-message timestamp
- Work-thread ID aliases

When a job comes in, Alice checks if an active session exists. If yes:
- **Provider-native steer**: Some backends (Codex, Claude) allow injecting new input into a running session. Alice tries this first.
- **Queuing**: If native steer fails and an LLM run is active, the new job is queued. A newer job supersedes an older queued job.
- **New run**: If no run is active, a new RunRequest is dispatched to the LLM backend.

### Cancellation and Interruption

- `/stop` immediately cancels the active run via context cancellation
- A newer user message supersedes queued jobs but does not interrupt the active run
- Automation tasks can also be interrupted by user messages that acquire the session gate

## Startup Modes

Alice supports two explicit startup modes:

### `--feishu-websocket`
Full mode. Connects to Feishu WebSocket, processes live messages, runs automation, and exposes the runtime API.

### `--runtime-only`
Local-only mode. The runtime API and automation engine run, but the Feishu connector does not start. Use for:
- Debugging and development
- Running only the automation scheduler
- Headless environments (use `alice-headless --runtime-only`)

> `alice-headless` is a dedicated binary that *cannot* start the Feishu connector. Attempting `alice-headless --feishu-websocket` will error.

## Config Hot Reload

- **Single-bot mode**: Limited partial hot reload is supported. Some config keys are watched for changes.
- **Multi-bot mode**: Hot reload is intentionally disabled. Always restart Alice after config changes.

## Runtime Home

| Build Channel | Default Home |
|---------------|-------------|
| Release (npm / installer) | `~/.alice` |
| Dev (source build) | `~/.alice-dev` |

Override with `--alice-home` flag or `ALICE_HOME` environment variable.
