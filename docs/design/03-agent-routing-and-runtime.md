# 03. AI 任务入口、模型路由与运行时

## 1. 总体目标

AI 层不只是“选个模型回复”。它负责四件事：

- 将自然语言解释为结构化意图。
- 入口分诊。
- 任务与模型能力匹配。
- 为不同模型族提供统一运行时封装。

## 2. IntentCompiler

`IntentCompiler` 是所有人类输入的第一层解释器。它负责把自然语言转换为结构化 `IntentSpec`。

### 2.1 目标

- 把“帮我设一个每天 9 点的邮件摘要”解释成 `setting_change`。
- 把“现在把项目 A 的预算调高一点”解释成设置变更并标记风险。
- 把“优化这个模型训练脚本”解释成 `task_request`。
- 把“停止当前这轮实验”解释成 `manual_signal`。
- 把“我批准这次合并”解释成 `approval_response`。

### 2.2 输出要求

- 输出必须结构化。
- 低置信度必须触发确认。
- 不允许自然语言解释器直接执行副作用。
- 对自然语言入口，意图识别必须由模型完成；关键词匹配、前缀匹配、正则规则不能作为主判定逻辑，也不能作为兜底分类器。

### 2.3 与 TaskClassifier 的关系

- `IntentCompiler` 负责“这句话想做什么”。
- `TaskClassifier` 负责“这个任务走哪条执行路径、需要哪些能力”。

### 2.4 首版实现约束

- `IntentCompiler` 必须是模型化组件，可以使用低成本模型，但不能退化成关键词表驱动分类器。
- 若模型输出低置信度，应回显候选解释并请求确认，而不是切回规则匹配。
- 关键词、前缀和正则只可用于结构化信号解析、槽位归一化或安全过滤，不得直接决定 `intent_kind`。

## 3. TaskClassifier

### 3.1 职责

`TaskClassifier` 是所有外部任务的第一跳。

输入：

- 用户原始请求或 webhook 负载。
- 最小上下文。
- 用户偏好和全局记忆摘要。

输出：

- `routing_decision`: `fast path` / `task path` / `reject`
- `required_capabilities`
- `needs_network`
- `needs_simple_execution`
- `needs_repo_context`
- `needs_memory_recall`
- `risk_level`
- `suggested_budget_tier`
- `suggested_runtime`
- `suggested_roles`

### 3.2 首版实现选择

首版使用单 agent、低成本、快速模型实现，例如低价档 Codex mini 或 Kimi Code 小模型档。原因：

- 分诊是高频动作。
- 分诊必须可容忍偶发误判，但不能太贵。
- 分诊允许轻量工具调用，但不应拉起复杂 agent 群。

### 3.3 分诊误判策略

- 误把复杂任务判成简单任务：允许中途升级为 `task path`。
- 误把简单任务判成复杂任务：代价是多走控制平面，但不破坏正确性。
- 系统更容忍“保守升级”，不容忍“静默低估风险”。

## 4. 角色与运行时

本设计中的 `Planner`、`Developer`、`Reviewer`、`Researcher`、`Experimenter` 都是逻辑角色，不是固定常驻进程。

### 4.1 映射规则

| 逻辑角色 | 主要职责 | 常见 runtime |
| --- | --- | --- |
| `Planner` | 拆解目标、收敛约束、决定下一步 | `LLMRuntime` 或 `AgenticRuntime` |
| `Developer` | 修改代码、执行命令、准备 PR | `AgenticRuntime` |
| `Experimenter` | 跑实验、收集产物、回传指标 | `AgenticRuntime` |
| `Reviewer` | 解释结果、比较方案、给出风险判断 | `LLMRuntime` |
| `Researcher` | 路线对比、长期探索、阶段总结 | `LLMRuntime` 或 `AgenticRuntime` |

规则：

- 角色是任务阶段上的职责标签，runtime 是实际执行载体。
- 同一个 runtime 在不同阶段可以扮演不同角色，但必须显式记录角色标签。
- 同一轮次不要同时让两个写入型角色对同一仓库分支工作。

## 5. ModelRouter

`ModelRouter` 接收任务类型、能力需求、预算和运行条件，返回模型选择结果。

### 5.1 路由维度

- 任务类型：问答 / 代码 / 评审 / 研究 / 摘要 / 定时观察 / 评测解释。
- 能力类型：agentic / language-only / fast-single-agent。
- 风险等级：低 / 中 / 高。
- 是否需要联网。
- 是否需要简单执行。
- 是否需要仓库写权限。
- 是否需要中立评审。
- 预算剩余。

### 5.2 能力分类

| 类型 | 适合任务 | 典型模型族 |
| --- | --- | --- |
| `FastSingleAgentRuntime` | 快速问答、轻量摘要、轻量工具调用 | 低成本 Codex mini / Kimi Code 小模型档 |
| `AgenticRuntime` | 代码修改、修复、命令执行、PR 迭代 | Codex、Kimi Code |
| `LLMRuntime` | review、比较、总结、计划、判定 | DeepSeek、GLM、其他纯语言模型 |

### 5.3 路由原则

- 能用低成本完成的任务，不升级高成本模型。
- 需要写代码和执行命令的任务优先选择 `AgenticRuntime`。
- 需要中立评审的任务优先选择 `LLMRuntime`。
- 涉及审批或高风险副作用时，必须返回显式的审批建议，而不是直接继续动作。

## 6. RuntimeSession 接口

统一接口：

- `Plan`
- `Act`
- `Review`
- `Summarize`
- `ClassifyCapability`

每次运行时调用都应返回统一结果结构，至少包含：

- `role`
- `summary`
- `proposed_actions`
- `artifacts`
- `memory_candidates`
- `followup_hint`
- `requires_escalation`

### 6.1 FastSingleAgentRuntime

适合：

- 天气、新闻、网页查询。
- 邮件摘要。
- 学校官网定时检查。
- 单次轻量工具调用。

约束：

- 默认只允许只读型工具和非常简单的执行动作。
- 不允许持有仓库写锁。
- 超过时长、动作数或上下文阈值必须升级。

### 6.2 AgenticRuntime

适合：

- 编码与修复。
- 测试与本地执行。
- 评测前准备。
- 仓库操作。

约束：

- 必须运行在受控执行器或受控工具边界内。
- 对同一仓库分支的写操作必须串行。
- 任何超出 `workspace_policy` 的写入请求都必须被拒绝或升级审批。

### 6.3 LLMRuntime

适合：

- 代码 review。
- 评测结果解释。
- 科研路线对比。
- 阶段性报告摘要。

约束：

- 默认不直接执行写入型命令。
- 对结果只给建议，不直接推进终态。

## 7. Context Packet

所有运行时都接收统一的上下文包：

- `Goal`
- `Constraints`
- `CurrentState`
- `RelevantArtifacts`
- `MemoryContext`
- `NextAction`
- `Role`

### 7.1 上下文压缩规则

- 只附最近必要日志摘要。
- 只附相关文件和差异片段。
- 只附检索出来的高相关记忆。
- 上下文超过阈值时必须二次摘要，不允许无上限堆积。

## 8. 预算与升级

### 8.1 Fast path 预算

- 更低 token 上限。
- 更低工具调用上限。
- 更低总时长上限。

### 8.2 Task path 预算

- 分阶段预算。
- 可因评测失败或环境不稳定触发预算扣减。
- 可因人工批准提升预算。

### 8.3 升级条件

- 上下文过大。
- 需要仓库写入。
- 需要多轮修复。
- 需要远端执行。
- 需要正式评测。
- 需要切换到不同逻辑角色接力。

## 9. 运行时失败处理

- `FastSingleAgentRuntime` 失败：优先重试一次，仍失败则升级或返回失败摘要。
- `AgenticRuntime` 失败：记录失败动作、命令和产物，再交给 `Reviewer` 角色做失败归因，或回到 `Planning`。
- `LLMRuntime` 失败：可退回较低成本模型或跳过评审，但不能伪造通过结论。

规则：

- 失败摘要必须携带角色、模型选择和关键上下文来源。
- 运行时的结论只是建议，真正的工作流推进仍由控制平面裁决。
