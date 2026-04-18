# Alice Docs

This directory now keeps only the current operator and architecture docs in the top level. Historical design notes and stale flow walkthroughs were moved under [`docs/legacy/`](./legacy/README.md).

## Current Docs

- [Architecture](./architecture.md)
- [架构文档](./architecture.zh-CN.md)
- [Usage Guide](./usage.md)
- [使用说明](./usage.zh-CN.md)

## Why Some Docs Were Archived

The archived documents described older internals that no longer match the codebase, especially:

- the removed memory subsystem
- the old Feishu message-flow walkthrough built around that subsystem
- one-off design/review artifacts that are no longer the canonical architecture source

## 当前文档

- [Architecture](./architecture.md)
- [架构文档](./architecture.zh-CN.md)
- [Usage Guide](./usage.md)
- [使用说明](./usage.zh-CN.md)

## 为什么归档旧文档

已经归档的文档主要描述了不再存在或已明显变更的实现，尤其是：

- 已移除的 memory 子系统
- 建立在那套 memory 设计之上的旧版飞书消息流说明
- 不再适合作为当前架构事实源的一次性设计稿和审阅报告
