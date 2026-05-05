# Alice

> **A**I **L**ocal **I**nteractive **C**ross-device **E**ngine
>
> Same AI session, anywhere. Terminal ↔ Feishu. No cloud lock-in. Works with OpenCode (DeepSeek V4), Codex, Claude, Gemini, Kimi.

[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Alice-space/alice)](https://goreportcard.com/report/github.com/Alice-space/alice)
[![Go Reference](https://pkg.go.dev/badge/github.com/Alice-space/alice.svg)](https://pkg.go.dev/github.com/Alice-space/alice)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[中文](./README.zh-CN.md)

- **Access your agent from anywhere.**
  Terminal at your desk. Feishu on your phone. Same session, same context — just `/session resume`.
- **Pick your AI.**
  OpenCode / DeepSeek V4, Codex, Claude, Gemini, Kimi. Mix and match per scene.
- **Zero cloud dependency.**
  The agent CLI runs on your machine. No API keys, no vendor lock-in.
- **Goal mode × DeepSeek = low cost.**
  Fire off dozens of tasks for pennies. Get notified on your phone when done.

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

Then in Feishu: `@Alice #work deploy the staging environment` — Alice creates a task thread, runs your LLM backend, and streams progress back. Use `/session` anytime to resume the task from your terminal.

## Development

```bash
make check   # fmt, vet, test, race
make build
make run
```

Contribution guide: [CONTRIBUTING.md](./CONTRIBUTING.md)

## License

[MIT](./LICENSE)
