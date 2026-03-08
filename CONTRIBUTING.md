# Contributing

本仓库当前处于早期设计阶段，贡献以“先定边界，再写代码”为默认顺序。

## 基本原则

- 先读 [`README.md`](./README.md) 和 [`docs/draft.md`](./docs/draft.md)
- 涉及架构边界变化时，先更新设计文档或新增 ADR
- 小步提交，避免把目录初始化、文档改动和业务实现混在同一个提交里
- 不提交本地构建产物、临时文件或 IDE 噪音

## 分支与提交

- 分支命名建议：`feat/...`、`fix/...`、`docs/...`、`chore/...`
- 提交信息建议使用简洁前缀，例如：
  - `docs: refine task state model`
  - `chore: bootstrap go project skeleton`
  - `feat: add webhook signature verification`

## 目录约束

- `cmd/` 只放程序入口和装配代码
- `internal/` 放仓库私有实现，默认优先落这里
- `pkg/` 只在确认需要复用且 API 可长期维护时再使用
- `api/` 放协议、schema、OpenAPI、protobuf 或 MCP 契约草案
- `configs/` 放模板与样例，不提交真实密钥
- `scripts/` 放可重复执行的自动化脚本
- `test/` 放集成测试夹具、测试数据与端到端测试资产

## Pull Request 要求

- 说明改动目的、影响范围和验证方式
- 如果修改系统边界，附上相关设计文档或 ADR 链接
- 如果引入新目录或新模块，说明它为什么属于该层
- 如果暂时没有测试，需要明确写出原因和后续补测计划

## 文档优先的场景

以下变更默认先写文档再实现：

- 状态机或任务生命周期调整
- BUS、Store、Outbox、Scheduler 的职责变更
- MCP 协议字段或交互语义调整
- 审核、预算、恢复、人工干预相关流程变化

## 本地约定

- 默认使用 LF 行尾
- 默认使用 UTF-8 编码
- 默认使用空格缩进；`Makefile` 等特殊文件除外
