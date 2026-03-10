# Durable Workflow 概念设计

文件名沿用历史命名，但本文现在只讨论第一版与代码和研究直接相关的 durable workflow，重点覆盖：

- `issue-delivery`
- `research-exploration`

控制面 workflow 另见 [control_plane_workflow_detail.md](./control_plane_workflow_detail.md)。

本文承接 [entry_processing_detail.md](./entry_processing_detail.md) 中“request 已经 promote 并完成 workflow 绑定”的止步点。

## 1. 目标与范围

本文回答六个问题：

- `DurableTask` 绑定 workflow 后，哪些边界由 BUS 负责，哪些边界由 workflow manifest 负责
- leader / reviewer / worker / evaluator 这些槽位如何被策略层选型
- `issue-delivery` 和 `research-exploration` 的参考 step 图是什么
- 人类补充信息、审批、打回、预算和取消如何通过 `ExternalEvent` 回流
- durable workflow 的最小审计和恢复语义是什么

本文不覆盖：

- prompt 模板
- 模型选型策略细节
- MCP 接口报文
- 存储实现

## 2. durable workflow 的共通原则

### 2.1 `DurableTask` 只承载顶层生命周期

`DurableTask` 的顶层状态只保留：

- `NewTask`
- `Active`
- `WaitingHuman`
- `Succeeded`
- `Failed`
- `Cancelled`

这意味着：

- `plan`、`code`、`review`、`evaluate`、`merge`、`report` 都是 workflow step，不是 BUS 顶层状态
- BUS 不负责把每类 durable task 写成一套新的全局状态机

### 2.2 workflow 才是业务剧本

每个 `DurableTask` 在创建时都绑定一个不可变 `WorkflowBinding`，至少包含：

- `workflow_id`
- `workflow_source`
- `workflow_rev`
- `manifest_digest`
- manifest 快照

BUS 只负责三件事：

- 校验某个 workflow 是否可接受
- 固化绑定结果
- 按 manifest 管理 step 执行、gate 和回退

这里的“不可变”有两个明确含义：

- 已写入的单条 `WorkflowBinding` 记录不得原地改写。
- 如果策略允许在审批点显式重规划，只能为同一 `task_id` 追加新的 binding 记录，并把旧 binding 标记为 superseded；旧 step history、gate 和 outbox 记录仍归属于旧 binding。

BUS 不负责：

- 发明某类任务的固定业务阶段
- 写死某个阶段必须用哪个模型
- 默认把 review 通过等价成自动 merge

### 2.3 agent 通过槽位选型，不通过硬编码命名

workflow manifest 只声明能力槽位，例如：

- `leader`
- `worker`
- `reviewer`
- `evaluator`

策略层再根据 `AgentRegistry` 选择满足约束的 agent profile。本文不再使用旧模型里那种固定 `PlannerAgent`、`CodeAuditAgent` 名字来表达系统边界，因为真正稳定的是槽位和约束，而不是某个具体实例名。

### 2.4 子 agent 必须通过 `AgentDispatch`

leader 不得用一段自由文本私下叫人干活。任何 helper / worker / reviewer / evaluator 都必须通过 `AgentDispatch` 被唤醒，并绑定：

- `context_pack_id`
- `goal`
- `expected_outputs`
- `allowed_tools`
- `allowed_mcp`
- `write_scope`
- `budget_cap`
- `return_to`

默认只有 workflow 中的主写实例可以提交 step 最终结果；子 agent 返回的是候选结果和证据。

### 2.5 所有外部副作用都走 `outbox + MCP`

durable workflow 内部的外部动作一律遵循：

1. BUS 写入 `OutboxRecord`
2. MCP 执行外部动作
3. BUS 回写结果和外部对象引用

适用范围包括：

- 创建或更新 Issue / PR / 评论 / merge
- 提交、取消、清理集群作业
- 创建、修改、暂停、删除定时任务
- 发布 workflow 新 revision

## 3. 关键对象

| 对象 | 含义 |
| --- | --- |
| `DurableTask` | 顶层 durable 工作对象 |
| `WorkflowBinding` | task 与某个具体 workflow revision 的绑定 |
| `StepExecution` | 某个 step 的执行记录 |
| `Artifact` | workflow 内部 step 的结构化产物 |
| `AgentDispatch` | 一次子 agent 调度 |
| `ContextPack` | 调度时传递的上下文快照 |
| `ApprovalRequest` | approval / confirmation / budget gate |
| `HumanWaitRecord` | `WaitingInput` / `WaitingRecovery` 的持久等待锚点 |
| `OutboxRecord` | 外部副作用动作 |
| `UsageLedger` | 预算、token 和资源账本 |

## 4. `issue-delivery` 参考模型

`issue-delivery` 用于处理明确代码需求、修 bug、新功能交付和 PR 收口。

### 4.1 绑定条件

这类任务通常具备以下特征：

- 需要仓库写权限
- 会产生 PR、评论、merge 等外部副作用
- 需要多 step 协作
- 经常伴随 review 或人工确认

因此入口阶段通常已经通过 `PromotionDecision` 判定必须 promote。

### 4.2 参考 step 图

`issue-delivery` 的一个最小参考 step 图可以是：

1. `triage`
2. `plan`
3. `code`
4. `review`
5. 可选 `merge`
6. 可选 `report`

可能的 gate 挂点：

- `plan` 之后可挂 `approval`
- 高风险外部写之前可挂 `confirmation`
- `merge` 之前可再挂 `approval`

具体是否存在这些 gate，以及挂在哪个 step 之间，由 manifest 决定，而不是由 BUS 顶层逻辑写死。

### 4.3 各 step 的概念职责

`triage`

- leader 汇总入口请求、仓库引用、issue/PR 引用和风险摘要
- 输出 `task_brief`

`plan`

- leader 结合代码上下文形成 `plan`
- 输出修改目标、涉及模块、风险点、测试点、验收点
- 如 manifest 要求，后续进入 `approval` gate；若需要 reviewer 意见，应在后续 `review` step 中产出结构化 `review_result`，而不是新增 gate 类型

`code`

- leader 单兵执行，或通过 `AgentDispatch` 拉起只读分析 worker、patch worker、测试 worker
- 输出 `candidate_patch`、`test_notes`、PR 引用或等价代码版本引用

`review`

- reviewer 基于 patch、PR、测试证据给出结构化 `review_result`
- 不通过时按 manifest 回退到 `code` 或 `plan`

`merge`

- 只有在 workflow 明确声明、且所有 gate 满足时才存在
- merge 本身仍然只是普通 step，不能被系统核心默认触发

`report`

- 将最终 artifact、review 结论和外部对象引用整理成人类可读输出

### 4.4 人类事件如何作用到 `issue-delivery`

人类补充需求、打回、改目标、要求拆任务，本质上都是新的 `ExternalEvent`：

- 若是小范围补充，workflow 可以回退到 `code`
- 若是需求边界变化，workflow 应回退到 `plan`
- 若变化已经超出原任务目标，策略层可以拒绝继续漂移并要求拆新 task

如果人类在 `review` 后追加新需求，BUS 不得让系统继续自动 merge，而应先冻结自动推进，再根据回退规则回到 `code` 或 `plan`。

## 5. `research-exploration` 参考模型

`research-exploration` 用于处理带实验循环、预算约束和指标目标的研究任务。

### 5.1 绑定条件

这类任务通常具备以下特征：

- 明确依赖 GPU/CPU 作业
- 不是一次编码后就结束，而是 `code -> evaluate -> code` 循环
- 必须跟踪预算、资源和恢复
- 可能最终输出 PR，也可能只输出报告

因此入口阶段通常会把它 promote 成 `DurableTask` 并绑定 `research-exploration`。

### 5.2 参考 step 图

最小参考 step 图可以是：

1. `plan`
2. `code`
3. `evaluate`
4. 若未达标，回到 `code`
5. 若达标，进入 `review` 或 `report`

### 5.3 各 step 的概念职责

`plan`

- leader 输出实验计划
- 明确主指标、阈值、数据集版本、基线版本、预算、停止条件

`code`

- 修改训练脚本、模型实现、参数配置或实验编排代码
- 输出候选 patch 和代码版本引用

`evaluate`

- evaluator 或 leader 通过 `outbox + Cluster MCP` 发起受控实验
- 回写 `evaluation_result`
- 必须记录代码版本、数据集版本、评测配置、资源用量和指标结果

`review` / `report`

- 若目标是交付代码，则进入 review
- 若目标是阶段研究结论，则可直接进入 report

### 5.4 预算与恢复

`research-exploration` 与 `issue-delivery` 的最大区别在于：

- 预算不是附加展示字段，而是状态推进约束
- 当预算接近耗尽或命中硬上限时，task 会进入 `WaitingHuman`
- 人类追加预算、恢复评测或改变目标时，本质上都是新的 `ExternalEvent`

恢复规则必须显式：

- 如果代码版本和 `EvalSpec` 都未变，只是恢复资源或追加预算，可以回到 `evaluate`
- 如果主指标、数据集、预算上限或停止条件发生变化，通常要回到 `plan`

## 6. gate 与 `WaitingHuman`

对 durable workflow 而言，`WaitingHuman` 是顶层挂起状态，具体原因体现在 `waiting_reason`：

- `WaitingInput`
- `WaitingConfirmation`
- `WaitingBudget`
- `WaitingRecovery`

进入 `WaitingHuman` 的常见原因：

- 需求不清，需要补充信息
- 高风险动作需要人工确认
- 预算耗尽，需要追加或终止
- 系统需要人工决定是恢复还是回退

离开 `WaitingHuman` 的方式也是新的 `ExternalEvent`，而不是 workflow 内部偷偷恢复。

### 6.1 人类 gate 回流事件契约

人类点击审批卡、补充信息、追加预算或决定恢复时，回流事件至少要满足下面的最小契约：

| 场景 | 必填关联键 | 最小字段 | 生效前置条件 | 不满足时处理 |
| --- | --- | --- | --- | --- |
| `approval` / `confirmation` | `approval_request_id`、`task_id`、`step_execution_id` | `decision`、操作者、时间戳、可选备注 | gate 仍处于活跃状态，task 未终态，目标 step 未 superseded | 只记审计，不恢复执行 |
| `budget` 恢复 | `approval_request_id`、`task_id`、`step_execution_id`、`waiting_reason=WaitingBudget` | `decision`、预算变更或继续/终止指令、操作者 | task 仍在 `WaitingHuman`，预算 gate 未过期 | 只记审计，不恢复执行 |
| 补充输入 | `task_id` 或 `reply_to_event_id`、当前 `waiting_reason`、durable 路径下的 `human_wait_id` | 字段补丁或补充文本、操作者、时间戳 | 当前等待原因仍是 `WaitingInput`，且 task 未被新事件 supersede | 只记审计，并要求重新路由 |
| 恢复 / 回退决定 | `human_wait_id`、`task_id`、`step_execution_id`、`waiting_reason=WaitingRecovery` | `decision`、恢复点或回退点、操作者 | 当前恢复点仍有效，task 未终态 | 只记审计，不改变执行状态 |

补充约束：

- 过期审批、已 superseded 的 gate、或已经进入终态的 task，只能留下审计，不得恢复执行。
- `decision` 必须是结构化枚举，而不是自由文本“差不多继续”。
- 人类回流首先是新的 `ExternalEvent`，其次才是 gate 结果；没有事件审计，就不允许改变 task 状态。

## 7. 一致性与恢复

durable workflow 必须明确以下恢复语义：

- 外部事件按至少一次投递处理，必须幂等
- `OutboxRecord` 必须支持对账恢复
- MCP 成功但 BUS 回写失败时，重启后必须扫描并补写状态
- 旧 workflow revision 的 task 不得自动迁移到新 revision
- 已 superseded 的外部对象回流事件只能留审计，不能污染当前主路径

## 8. 反模式

本文明确拒绝以下做法：

- 把 `plan`、`code`、`review` 写成 BUS 平台级固定状态
- 用固定 Agent 名字代替 manifest 槽位和策略选型
- 让 leader 通过隐式 prompt 拉起看不见的 helper
- 在 `review` 通过后默认自动 merge
- 在预算耗尽后继续隐式提交实验作业
- 在 workflow 发布后让旧 task 自动切换 revision

## 9. 评审时最该看什么

把 durable workflow 的评审口径固定成统一评审卡，避免不同评审人各看各的：

| 检查维度 | 必查证据 | 通过标准 | 常见反模式 | 阻塞级别 | 评审结论 |
| --- | --- | --- | --- | --- | --- |
| workflow 边界归属 | `WorkflowBinding`、manifest step 图、BUS 顶层状态定义 | business step 来自 workflow manifest；BUS 顶层只保留 `NewTask/Active/WaitingHuman/Succeeded/Failed/Cancelled` | 把 `plan/code/review/merge` 写成平台级固定状态 | 阻塞 | 待填写 |
| agent 协作治理 | 槽位定义、`AgentDispatch` 字段、`write_scope`、预算约束 | helper / worker / reviewer / evaluator 均通过槽位 + `AgentDispatch` 受控协作 | 用隐式 prompt 私下拉 helper，或默认子 agent 可直接提交最终结果 | 阻塞 | 待填写 |
| 外部副作用路径 | `OutboxRecord`、MCP 执行记录、外部对象引用回写 | 所有写操作都走 `outbox + MCP`，且 BUS 能对账恢复 | step 内直接写仓库/集群/控制面，不留 outbox 记录 | 阻塞 | 待填写 |
| 人类 gate 与恢复 | `WaitingHuman` 原因、回流 `ExternalEvent`、回退规则 | `WaitingHuman` 是顶层挂起状态；恢复、补充信息、预算追加都通过新 `ExternalEvent` 回流 | 在 workflow 内部偷偷恢复，或把人工 gate 写成普通 step 旁注 | 高 | 待填写 |
| 版本与漂移控制 | `workflow_rev`、manifest digest、变更发布规则 | 旧 task 不自动漂移到新 revision；需要新绑定才切换 | workflow 发布后让进行中 task 自动吃到新逻辑 | 高 | 待填写 |
| 跨 workflow 一致性 | `issue-delivery`、`research-exploration`、控制面 workflow 的对照 | 三类 workflow 共用同一套 binding / gate / outbox / audit 原则，只在业务 step 上不同 | 代码交付守治理边界，控制面或研究流程另起一套特例 | 高 | 待填写 |

只要这张评审卡能逐项过关，具体 step 命名和局部图形可以继续演化；如果这些检查不过，CDR 仍然没有从旧模型迁移过来。
