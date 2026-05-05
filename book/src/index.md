# Alice

Alice is a **Feishu long-connection connector** that turns CLI-based LLM agents (Codex, Claude, Gemini, Kimi, OpenCode) into interactive bots inside your Feishu workspaces.

## What problem does Alice solve?

You have an LLM agent CLI installed — `codex`, `claude`, `opencode` — and it works great in your terminal. But your team lives in Feishu. You want the same agent available in group chats and direct messages, without building a custom bot from scratch.

Alice bridges that gap. It runs as a local service that:

- Connects to Feishu's WebSocket for real-time message delivery
- Routes incoming messages into `chat` (casual) or `work` (task-oriented) scenes
- Calls your configured LLM CLI backend with the right prompt, model, and permissions
- Sends progress updates, final replies, files, and images back to Feishu
- Exposes a local HTTP API for bundled skills and automation tasks

## Key Features

- **Multi-bot**: One `alice` process, one `config.yaml`, multiple independent bots
- **Scene routing**: Separate `chat` and `work` modes with per-scene LLM profiles
- **Six backends**: OpenCode, Codex, Claude, Gemini, Kimi — switch per scene
- **Session persistence**: Resumable threads, session aliases, usage counters
- **Live status cards**: Real-time heartbeat showing backend activity and file changes
- **Automation**: Cron-like scheduled tasks with `send_text`, `run_llm`, and `run_workflow` actions
- **Bundled skills**: Extendable skill scripts that call the runtime API
- **Subprocess delegation**: `alice delegate` lets OpenCode agents send subtasks to other backends
- **Zero cloud dependency**: Everything runs on your machine

## Who is Alice for?

- **Teams using Feishu** that want LLM agent access without building custom integrations
- **Developers** who already use CLI agents and want them accessible in group chats
- **Operators** who need scheduled automation along with interactive LLM capabilities

## Navigation

| Section | For |
|---------|-----|
| [Tutorials](tutorials/quick-start.md) | New users — get Alice running in 5 minutes |
| [How-To Guides](how-to/install.md) | Task-focused recipes for specific goals |
| [Explanation](explanation/system-model.md) | Deep dives into concepts and design |
| [Reference](reference/configuration.md) | Comprehensive config, API, and CLI docs |
| [Development](development/architecture.md) | Contributor-oriented architecture and guides |

## Quick Start

```bash
npm install -g @alice_space/alice
alice setup
# edit ~/.alice/config.yaml with your Feishu credentials
alice --feishu-websocket
```

See the [Quick Start tutorial](tutorials/quick-start.md) for detailed steps.

---

[中文版](zh-CN/index.md) · [GitHub](https://github.com/Alice-space/alice)
