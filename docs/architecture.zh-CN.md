# 架构设计与重构规划

[English](./architecture.md)

本文档定义 `alice` 的目标架构，并记录重构阶段成果。

## 设计目标

- 高内聚：每个包只负责单一核心职责。
- 低耦合：核心流程优先依赖接口，而非具体传输实现。
- 可恢复：重启后的状态恢复行为可预测、可重复。
- 可运维：重构期间不破坏既有部署与运行手册行为。

## 模块边界

- `cmd/connector`：仅负责启动编排（加载配置、组装依赖、运行主循环）。
- `internal/connector`：飞书事件接入、排队、按会话串行、回复编排。
- `internal/llm/codex`：Codex CLI 调用与流式事件解析。
- `internal/memory`：长期记忆与分日期记忆持久化。
- `internal/automation`：自动化任务调度、存储与执行引擎。
- `cmd/alice-mcp-server` + `internal/mcpserver`：MCP 服务入口与处理逻辑。

## 依赖约束

- `cmd/*` 可以依赖 `internal/*`；`internal/*` 不能反向依赖 `cmd/*`。
- `internal/connector` 通过接口使用 `internal/llm`、`internal/memory`、`internal/automation`。
- 飞书 SDK 调用集中在 connector/sender 适配层，避免外溢。
- 运行期可变状态集中在专门状态组件中管理。

## 运行链路

1. 飞书 WS 事件进入 `App`（`internal/connector/app.go`）。
2. 事件被标准化为 `Job`，按 session key 路由并入队。
3. Worker 通过 session 级互斥保证同会话串行处理。
4. `Processor` 构造上下文并调用后端，然后通过发送器回退链路输出进度与结果。
5. 会话/运行态与记忆模块异步落盘。

## 本次重构已完成

- 新增 `runtimeStore`（`internal/connector/runtime_store.go`）集中管理运行态：
  - `latest` 会话版本
  - `pending` 任务
  - 群聊 `mediaWindow`
  - 会话互斥锁映射
  - 运行态持久化版本信息
- `App` 与运行态/上下文窗口相关逻辑已统一改为使用 `runtimeStore`。
- 删除废弃的交互卡片增量更新链路：移除 `Sender` 接口及实现中的 `PatchCard`。

## 后续重构切片

1. 抽离消息发送回退策略（卡片/富文本/纯文本）为独立传输策略组件。
2. 将 `Processor` 拆分为 `上下文构建`、`后端调用`、`回复渲染` 三段流水线，提升可测性。
3. 把 `cmd/connector/main.go` 的装配逻辑拆成可组合初始化器/构建器。
