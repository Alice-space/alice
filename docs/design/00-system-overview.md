# 00. 系统总览

## 1. 目标

本系统是一个面向个人科研和小规模协作的 AI 科研控制平面。它统一处理四类工作：

- 快速问答与轻量执行。
- 代码修改、实验运行和 PR 评测。
- 没有固定范式的长期科研探索。
- 定时观察、摘要、通知类自动化任务。

系统不是简单的聊天壳，也不是一开始就拆成微服务的大平台。首版目标是用单个 Go 控制平面把任务、状态、日志、通知、预算、记忆和外部执行串起来，并通过稳定接口为后续拆分留下余地。

## 2. 设计模型

系统设计同时使用五种模型：

1. 有界上下文模型
   将系统拆为控制平面、工作流、AI 运行时、记忆、执行、集成、治理几个上下文。
2. 状态机模型
   所有长期任务都要经过可恢复的显式状态流转。
3. 事件驱动模型
   外部触发、内部异步动作和回写都通过事件建模。
4. 能力边界模型
   SSH、Git、SLURM、邮件、飞书等能力不直接嵌入核心，而通过 MCP 或适配器暴露。
5. 自然语言意图模型
   所有人类操作都先转成结构化 `IntentSpec`，再进入任务流、设置流、审批流或配置提案流。

## 3. 总体架构

```text
Human Natural Language / Webhook / Cron
                  |
         Intent Compiler + Control Plane
                  |
  +---------------+------------------+
  |               |                  |
Workflow      Agent Runtime      Governance
  |               |                  |
  +----- Memory ---+---- Integrations ----+
                           |              |
                        Model APIs      MCP Clients
                                          |
                 +------------------------+----------------------+
                 |            |            |          |          |
               Git MCP      SSH MCP    SLURM MCP   Mail MCP   Custom MCP
```

## 4. 子系统边界

| 子系统 | 负责 | 不负责 |
| --- | --- | --- |
| Control Plane | 接收请求、分发命令、治理、对外 API、事件协调 | 不直接实现 SSH/Git/模型细节 |
| Workflow Engine | 长任务状态、恢复、人工信号、调度接力 | 不做模型推理 |
| Agent Routing & Runtime | 分诊、模型选择、上下文打包、运行时封装 | 不保长期状态真相 |
| Memory System | 复用性知识沉淀、检索、晋升、衰减 | 不替代原始日志和主数据库 |
| Execution & Integrations | 命令执行、作业提交、Git/邮件/通知调用 | 不裁决业务流程 |
| Governance | 预算、锁、配额、异常等级、审计 | 不直接完成任务 |

## 5. 核心不变量

- 任意写入型任务都必须属于某个 `ProjectSpec` 或“全局助手项目”。
- 任意人类输入都必须先被解释为 `IntentSpec`，不得直接驱动副作用。
- 任意可执行请求都必须生成 `TaskSpec`，任意任务运行都必须生成 `RunRecord`。
- 任意设置变更都必须落到 `RuntimeSetting` 或 `ConfigChangeProposal`。
- 任意长期任务都必须拥有可恢复状态和终止条件。
- 任意运行都必须区分 `run_status` 和 `workflow_phase`。
- 任意外部副作用都必须经过适配器或 MCP 边界。
- 任意不可安全重复的外部副作用都必须先落持久化 action intent 或等价 outbox。
- 任意自动评测都必须对应一个明确的 `AcceptancePolicy`。
- 任意记忆写回都必须带来源、置信度和作用域。

## 6. 配置边界

系统配置分三层：

1. 静态基线配置
   由配置文件、环境变量和密钥存储组成。负责凭据、MCP 地址、根目录、硬安全策略、默认资源上限。该层不是日常自然语言操作对象。
2. 可变运行时设置
   由数据库持久化，负责项目预算、通知策略、定时任务、观察目标、项目偏好等。该层可以通过自然语言变更。
3. 会话派生状态
   由运行时临时生成，例如当前锁、当前游标、当前重试次数。这一层不能直接由用户配置。

自然语言可以调节第二层，不能绕过审批直接改第一层。若用户提出“修改静态基线配置”的自然语言请求，系统应生成配置变更提案，而不是直接改文件。

## 7. 首版与后续演进

### 7.1 首版

- 单二进制 Go 主控。
- 本地嵌入式状态存储。
- `TaskClassifier + ModelRouter + AgentRuntime` 三段式 AI 入口。
- Git/SSH/SLURM/邮件/飞书以 MCP 或适配器方式接入。
- 统一日志、预算、锁、租约和异常分类。

### 7.2 后续

- 将事件总线外置。
- 将工作流引擎替换为独立组件。
- 记忆向量索引和知识库增强。
- 多主控或多租户不是首版目标，但接口设计需预留唯一 ID、租户字段和外部存储替换位。

## 8. 设计阅读重点

如果目的是实现：

- 先读 [01-domain-spec.md](./01-domain-spec.md) 和 [02-control-plane-and-workflow.md](./02-control-plane-and-workflow.md)。
- 然后读 [03-agent-routing-and-runtime.md](./03-agent-routing-and-runtime.md) 和 [05-execution-and-integrations.md](./05-execution-and-integrations.md)。
- 评测闭环看 [06-evaluation-and-pr-loop.md](./06-evaluation-and-pr-loop.md)。
- 并发和边界条件看 [07-concurrency-failure-and-safety.md](./07-concurrency-failure-and-safety.md)。
