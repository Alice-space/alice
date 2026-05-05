# Alice

> **同一个 AI 会话，随时随地。** 终端里写代码，手机上用飞书接着跑，再切回来。没有云锁定 — agent 跑在*你自己*的机器上。支持 OpenCode（DeepSeek V4）、Codex、Claude、Gemini、Kimi。

[![Dev CI](https://github.com/Alice-space/alice/actions/workflows/ci.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/ci.yml)
[![Main Release](https://github.com/Alice-space/alice/actions/workflows/main-release.yml/badge.svg)](https://github.com/Alice-space/alice/actions/workflows/main-release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Alice-space/alice)](https://goreportcard.com/report/github.com/Alice-space/alice)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[English](./README.md)

- **随时随地访问你的 agent。**
  电脑前用终端，路上用飞书。同一个会话、同一份上下文 — 一条 `/session resume` 就切过去。
- **任选 AI。**
  OpenCode / DeepSeek V4、Codex、Claude、Gemini、Kimi。不同场景可以混用。
- **零云依赖。**
  agent CLI 跑在你的机器上。不需要 API Key，没有厂商锁定。
- **Goal 模式 × DeepSeek = 低价。**
  几毛钱并行跑几十个任务，手机上收通知。

面向飞书的长连接连接器，把 CLI 型 LLM agent — OpenCode (DeepSeek V4)、Codex、Claude、Gemini、Kimi 接入飞书聊天。

Alice 以本地多 bot runtime 运行 — 通过 WebSocket 接收消息，路由到 `chat` 或 `work` 场景，调用 LLM CLI，返回文本、文件和图片。

## 文档

完整文档在 **[alice-space.github.io/alice](https://alice-space.github.io/alice/zh/)**。

| | |
|--|--|
| [教程](https://alice-space.github.io/alice/zh/tutorials/quick-start.html) | 5 分钟跑起来 |
| [操作指南](https://alice-space.github.io/alice/zh/how-to/install.html) | 按目标查找的操作步骤 |
| [配置参考](https://alice-space.github.io/alice/zh/reference/configuration.html) | 所有配置项详解 |
| [架构文档](https://alice-space.github.io/alice/zh/development/architecture.html) | 代码级架构 |

[English »](https://alice-space.github.io/alice/en/)

## 快速开始

```bash
npm install -g @alice_space/alice
alice setup
# 编辑 ~/.alice/config.yaml
alice --feishu-websocket
```

## 开发

```bash
make check   # 格式、vet、测试、race
make build
make run
```

贡献指南：[CONTRIBUTING.md](./CONTRIBUTING.md)

## 许可证

[MIT](./LICENSE)
