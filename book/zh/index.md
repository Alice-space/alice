# Alice

> **A**I **L**ocal **I**nteractive **C**ross-device **E**ngine
>
> 同一个 AI 会话，随时随地。终端 ↔ 飞书，没有云锁定。

Alice 是一个**飞书长连接连接器**，把 CLI 型 LLM agent 变成飞书里的交互式 bot。支持 OpenCode（DeepSeek V4）、Codex、Claude、Gemini、Kimi。

## 核心思路

你的终端 agent 和飞书 bot 是**同一个会话**。在 IDE 里开始重构，手机上查进度，飞书里发下一条指令。Alice 把你的本地 CLI agent 桥接到飞书 WebSocket，让你不被任何设备束缚。

## Alice 解决什么问题？

你已经装了 LLM agent CLI，终端里用得很好。但是：

- 通过 WebSocket 连接飞书，实时接收消息
- 把收到的消息路由到 `chat`（闲聊）或 `work`（任务执行）场景
- 调用配置好的 LLM CLI，传入合适的 prompt、模型和权限设置
- 把进度更新、最终回复、文件、图片发回飞书
- 暴露本地 HTTP API 给 bundled skill 和自动化任务使用

## 主要特性

- **多 bot**：一个进程、一份 `config.yaml`、多个独立的 bot
- **场景路由**：`chat` 和 `work` 两种模式，各自使用不同的 LLM profile
- **六种后端**：OpenCode、Codex、Claude、Gemini、Kimi — 可以按场景切换
- **会话持久化**：可恢复的 thread、session 别名、用量计数器
- **实时状态卡片**：显示后端活动和文件变更的心跳卡片
- **自动化**：类似 cron 的定时任务，支持 `send_text`、`run_llm`、`run_workflow`
- **Bundled Skill**：可扩展的脚本 skill，通过 runtime API 交互
- **子任务委托**：`alice delegate` 让 OpenCode agent 把子任务发给其他后端
- **零云依赖**：所有东西都在你的机器上运行

## 谁适合用 Alice？

- **使用飞书的团队**，想把 LLM agent 放进群聊而不必自己写集成
- **开发者**，已经在用 CLI agent，想在群里也能用
- **运维人员**，需要定时自动化 + 交互式 LLM 能力

## 导航

| 区域 | 适合 |
|------|------|
| [教程](tutorials/quick-start.md) | 新用户 — 5 分钟跑起来 |
| [操作指南](how-to/install.md) | 针对具体目标的操作步骤 |
| [讲解](explanation/system-model.md) | 深入理解概念和设计 |
| [参考](reference/configuration.md) | 完整的配置、API 和 CLI 文档 |
| [开发](development/architecture.md) | 面向贡献者的架构和开发指南 |

## 快速开始

```bash
npm install -g @alice_space/alice
alice setup
# 编辑 ~/.alice/config.yaml 填入飞书凭据
alice --feishu-websocket
```

详见[快速开始教程](tutorials/quick-start.md)。

---

[English](../en/) · [GitHub](https://github.com/Alice-space/alice)
