# Alice

> Turn DeepSeek V4 into a Feishu group chat bot in 5 minutes. No API key — just your existing OpenCode CLI.

> **Goal mode + DeepSeek's low cost** = fire off dozens of coding tasks in parallel for pennies.
> **Feishu on your phone** = start tasks from anywhere, get notified when done. You don't need to be at your desk.
> **Native CLI, not cloud API** = start a task in your terminal, continue from Feishu on your phone. One `/session resume` and your context is there — no lock-in, no cloud dependency.

[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Alice-space/alice)](https://goreportcard.com/report/github.com/Alice-space/alice)
[![Go Reference](https://pkg.go.dev/badge/github.com/Alice-space/alice.svg)](https://pkg.go.dev/github.com/Alice-space/alice)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Feishu long-connection connector for CLI-based LLM agents — OpenCode (DeepSeek V4), Codex, Claude, Gemini, Kimi.

Runs as a local multi-bot runtime: receives Feishu messages over WebSocket, routes them into `chat` or `work` scenes, calls the configured LLM CLI, and sends replies, files, and images back. Zero cloud dependency — everything runs on your machine.

## Documentation

Full documentation is at **[alice-space.github.io/alice](https://alice-space.github.io/alice/)**.

| | |
|--|--|
| [Tutorials](https://alice-space.github.io/alice/en/tutorials/quick-start.html) | Get Alice running in 5 minutes |
| [How-To Guides](https://alice-space.github.io/alice/en/how-to/install.html) | Task-focused recipes |
| [Configuration Reference](https://alice-space.github.io/alice/en/reference/configuration.html) | Every config key documented |
| [Architecture](https://alice-space.github.io/alice/en/development/architecture.html) | Code-level architecture |

[中文文档 »](https://alice-space.github.io/alice/zh/)

## Quick Start

```bash
npm install -g @alice_space/alice
alice setup
# edit ~/.alice/config.yaml
alice --feishu-websocket
```

## Development

```bash
make check   # fmt, vet, test, race
make build
make run
```

Contribution guide: [CONTRIBUTING.md](./CONTRIBUTING.md)

## License

[MIT](./LICENSE)
