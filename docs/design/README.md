# 个人科研 AI 系统设计库

本目录是面向实现的系统架构设计库。[plan.md](./plan.md) 仍然保留为概念设计报告；本目录负责把概念收敛为统一 spec、统一边界、统一接口和统一并发/异常模型。若 `plan.md` 与本目录在对象定义、状态命名或事件语义上存在差异，以本目录为准。

## 阅读顺序

1. [00-system-overview.md](./00-system-overview.md)
2. [01-domain-spec.md](./01-domain-spec.md)
3. [02-control-plane-and-workflow.md](./02-control-plane-and-workflow.md)
4. [03-agent-routing-and-runtime.md](./03-agent-routing-and-runtime.md)
5. [04-memory-system.md](./04-memory-system.md)
6. [05-execution-and-integrations.md](./05-execution-and-integrations.md)
7. [06-evaluation-and-pr-loop.md](./06-evaluation-and-pr-loop.md)
8. [07-concurrency-failure-and-safety.md](./07-concurrency-failure-and-safety.md)
9. [08-operations-and-observability.md](./08-operations-and-observability.md)
10. [09-scenarios.md](./09-scenarios.md)

## 文档约定

- 术语采用本目录定义，不再以历史对话为准。
- 所有状态名、事件名、接口名保持英文，正文说明使用中文。
- 对人类而言，自然语言是唯一操作入口；CLI、飞书等只是自然语言承载通道。
- 自然语言意图识别必须由模型完成，不允许用关键词匹配替代。
- 所有写入型动作必须可追踪到 `ProjectSpec`、`TaskSpec`、`RunRecord`、`RuntimeSetting` 或 `ConfigChangeProposal` 之一。
- 所有异步触发都必须通过事件或调度任务显式建模，不允许“隐式后台动作”。
- 所有外部能力默认优先通过 MCP 或适配器边界接入，不直接把平台细节写进核心控制平面。
- 配置文件是静态基线与安全边界，不是日常任务交互入口。
- `Planner`、`Developer`、`Reviewer` 等名称在本目录中是逻辑角色，不是固定进程名。

## 规范分层

- 概念层：[plan.md](./plan.md)
- 技术总览：`00-system-overview.md`
- 统一领域模型与 spec：`01-domain-spec.md`
- 控制平面与状态流转：`02-control-plane-and-workflow.md`
- AI 任务入口、模型路由、运行时：`03-agent-routing-and-runtime.md`
- 记忆系统：`04-memory-system.md`
- 执行、集成、MCP 边界：`05-execution-and-integrations.md`
- PR 评测与回写闭环：`06-evaluation-and-pr-loop.md`
- 并发、故障与安全：`07-concurrency-failure-and-safety.md`
- 运维、部署、观测：`08-operations-and-observability.md`
- 端到端场景：`09-scenarios.md`

## 设计基线

当前设计坚持以下基线：

- 首版以 Go 单主控为中心。
- 首版部署目标是单二进制优先。
- 外部能力优先 MCP 化。
- 快速任务、长期任务、定时任务统一进入一套控制平面，但走不同处理路径。
- 记忆系统是一级能力，不是附属插件。
- 评测闭环和人工介入是科研闭环的硬组成部分。

## 后续扩展规则

- 新文档若引入新对象类型，必须回写到 [01-domain-spec.md](./01-domain-spec.md)。
- 新文档若引入新状态、事件或锁粒度，必须同步更新 [02-control-plane-and-workflow.md](./02-control-plane-and-workflow.md) 和 [07-concurrency-failure-and-safety.md](./07-concurrency-failure-and-safety.md)。
- 新的执行器或外部系统接入，必须先定义边界，再谈实现。
