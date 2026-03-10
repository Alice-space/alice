# 入口路由与 Promotion 概念设计

本文只细化入口阶段，承接 [draft.md](../draft.md) 和 [typical_cases.md](./typical_cases.md) 中的共识，不再使用旧模型里的 `RouteDecision`、`sync_direct`、`async_issue` 或“所有入口先建 Task”叙事。

本文止步于两个交接点：

- `EphemeralRequest` 在入口内直接得到最终回复
- request 被 promote 成 `DurableTask` 并完成 workflow 绑定

## 1. 目标与范围

入口阶段需要回答的不是“该不该马上开 issue”，而是更基础的三个问题：

1. 这条输入要不要进入 durable governance。
2. 如果不需要 durable governance，能否安全地留在 `EphemeralRequest` 内直接完成。
3. 如果需要 durable governance，应该 promote 成什么类型的 `DurableTask`，并绑定哪个 workflow。

因此，入口阶段的职责是：

- 接收并标准化外部输入
- 用固定路由键命中已有 request/task，或创建新的 `EphemeralRequest`
- 由 `Reception` 产出结构化 `PromotionDecision`
- 由策略层根据 `PromotionDecision` 决定直答、promote 或补充信息
- 在 promote 时把上下文和对象引用稳定地交给 durable workflow

本文不覆盖：

- durable workflow 内部的 plan/code/review/evaluate 细节
- MCP 协议报文
- 存储实现、快照格式和重放算法

## 2. 入口总模型

入口阶段的总模型应统一理解为：

```text
ExternalEvent
   -> route existing object or create EphemeralRequest
   -> Reception produces PromotionDecision
   -> policy decides:
        A. stay in EphemeralRequest and answer directly
        B. promote to DurableTask and bind workflow
        C. ask for more information, or promote and wait human
```

这个模型有两个关键约束：

- `Reception` 负责理解语义和抽取结构化事实，但不负责最终裁决。
- 真正的治理边界是 `PromotionDecision`，不是“模型觉得这个问题简单”。

## 3. 核心对象

入口阶段至少依赖以下对象：

| 对象 | 作用 | 说明 |
| --- | --- | --- |
| `ExternalEvent` | 统一入口事件 | 覆盖 IM 消息、webhook、评论、审批回流、定时触发 |
| `EphemeralRequest` | 短时请求容器 | 低风险、只读、短时完成；不绑定 workflow |
| `PromotionDecision` | 结构化升级判断 | 判断是否留在 request 内、是否 promote、建议哪个 workflow |
| `DurableTask` | durable 工作对象 | 命中 promote 硬规则后创建 |
| `WorkflowBinding` | workflow 版本绑定 | promote 后写入 task |
| `ContextPack` | 裁剪后的上下文快照 | direct answer 和 durable workflow 都可以使用 |
| `AgentDispatch` | 子 agent 调度契约 | request 内 helper 也必须显式审计 |
| `ToolCallRecord` | 工具或只读 MCP 调用审计 | direct answer 和 workflow 共用 |
| `ReplyRecord` | 回复审计 | 面向 request/task 的统一回复对象 |
| `TerminalResult` | 终态记录 | 记录 direct answer 或 task 终态摘要 |

需要特别强调的关系：

- `ExternalEvent` 先进入 BUS，再命中已有对象或创建 `EphemeralRequest`
- `EphemeralRequest` 不是“迷你 Task”；它有自己的审计，但不进入 durable 顶层状态机
- `DurableTask` 不是所有请求的默认容器；它只在命中 promote 规则时出现

## 4. 路由键与命中顺序

入口阶段必须优先使用确定性路由键，而不是把“是否命中同一对象”留给实现层自由发挥。

### 4.1 最小路由字段

`ExternalEvent` 在入口阶段至少要带这些字段：

- `task_id`
- `request_id`
- `reply_to_event_id`
- `conversation_id`
- `thread_id`
- `repo_ref`
- `issue_ref`
- `pr_ref`
- `comment_ref`
- `scheduled_task_id`
- `control_object_ref`
- `workflow_object_ref`
- `coalescing_key`

### 4.2 命中优先级

入口阶段的推荐顺序应与 `draft.md` 保持一致：

1. 显式指定的 `task_id` 或 `request_id`
2. `reply_to_event_id`
3. `repo_ref + issue_ref` 或 `repo_ref + pr_ref`
4. `scheduled_task_id` 或 `control_object_ref`
5. `workflow_object_ref`
6. `conversation_id + thread_id`
7. `coalescing_key`
8. 若都不命中，则创建新的 `EphemeralRequest`

补充约束：

- `reply_to_event_id` 的优先级高于控制面对象键和对话线程键
- 若 `reply_to_event_id` 命中的对象与显式 `scheduled_task_id` / `workflow_object_ref` 指向不同治理域，BUS 不得继续复用被回复对象；应拆出控制面 request/task，或先进入澄清路径
- 只有活跃对象才允许被命中和复用
- 已 `Answered` 或 `Expired` 的 `EphemeralRequest` 默认不复用
- 已 `Succeeded`、`Failed`、`Cancelled` 的 `DurableTask` 默认不复用，除非 workflow 明确允许
- 同优先级多候选时，选择最近更新时间最新的活跃对象，并记录命中依据

### 4.3 为什么控制面对象要单独路由

这是第一版最容易被旧模型误导的点。

- 修改定时任务不应靠普通对话线程路由，而应优先命中 `scheduled_task_id`
- 修改 workflow 不应靠“最近聊到哪个 topic”猜，而应优先命中 `workflow_object_ref`
- 高风险控制面请求一旦退化成对话线程匹配，就会把治理边界重新交还给模型

## 5. PromotionDecision 契约

`PromotionDecision` 是入口阶段最重要的结构化产物。它至少应表达：

- `intent_kind`
- `required_refs`
- `risk_level`
- `effects.external_write`
- `effects.create_persistent_object`
- `execution.async`
- `execution.multi_step`
- `execution.multi_agent`
- `governance.approval_required`
- `governance.budget_required`
- `governance.recovery_required`
- `proposed_workflow_id`
- `decision`
- `reason_codes`
- `confidence`

### 5.1 决策原则

策略层至少执行以下硬规则：

- 只要需要外部写操作或创建持久对象，就必须 promote
- 只要请求是长流程、异步、多 agent 协作，就必须 promote
- 只要请求需要审批、预算控制或恢复能力，就必须 promote
- 只有当上述条件全部为 `false`，并且命中只读 allowlist，才允许停留在 `EphemeralRequest`
- 信息不全或置信度低时，默认保守 promote，或先进入补充信息路径

### 5.2 只读 allowlist 的最小边界

第一版至少应把这两类请求视为 allowlist 候选：

- 公开信息查询，例如天气、公开资料
- 受控内部系统的只读查询，例如集群排队、资源池占用

它们共同满足：

- 没有外部写操作
- 没有持久对象创建
- 不需要审批或预算 gate
- 结果可以在当前回复内收口

### 5.3 workflow 归属裁决矩阵

`PromotionDecision` 命中 promote 硬规则后，BUS 还必须解决“到底绑定哪个 workflow”。这一步不能退化成模型一句话拍板，至少要满足下面的裁决矩阵：

| 信号或命中事实 | 候选 workflow | 裁决原则 |
| --- | --- | --- |
| 命中活跃 `scheduled_task_id` 或调度类 `control_object_ref` | `schedule-management` | 控制面对象路由优先于普通对话语义；不得降级成代码类 task |
| 命中活跃 `workflow_object_ref` | `workflow-management` | workflow 定义变更优先绑定控制面 workflow；不得被“顺手改代码”意图覆盖 |
| `external_write=true`、`multi_step=true`，且主结果是代码修改 / PR / repo 交付 | `issue-delivery` | 以 repo 交付为主目标时优先 |
| `async=true`、`budget_required=true`、`recovery_required=true`，且主结果依赖实验循环或指标达标 | `research-exploration` | 以评测闭环和预算恢复为主目标时优先 |
| 同时命中代码交付和研究探索信号 | `issue-delivery` 或 `research-exploration` | 若主成功条件是“交付 patch / PR”，选 `issue-delivery`；若主成功条件是“跑实验并看指标是否达标”，选 `research-exploration` |
| `Reception` 建议 0 个候选 workflow，或 manifest 校验后 0 个可接受候选 | 无 | 不得硬绑 workflow；进入补充信息或人工路由路径 |
| `Reception` 建议多个可接受候选，且无法靠路由键或主成功条件唯一化 | 多个 | 不得让模型隐式拍板；进入补充信息或人工路由路径 |
| `confidence` 低于策略阈值 | 任意 | 不得绑定业务 workflow；先补充信息或保守转人工 |

补充约束：

- 只有当“恰好一个候选 workflow 通过路由优先级和 manifest 校验”时，BUS 才能写入 `WorkflowBinding`。
- 如果控制面对象键和普通代码/研究意图同时出现，控制面对象键优先。
- 第一版不允许“先创建可执行 task，等跑到一半再决定它属于哪个 workflow”。

## 6. 入口执行路径

### 6.1 Direct Answer Path

当 `PromotionDecision` 命中 allowlist 时，入口阶段可以在 `EphemeralRequest` 内直接完成：

1. `Reception` 使用自身 `Skill` 解析请求
2. 如果需要 helper，也必须提交显式 `AgentDispatch`
3. 所有工具或只读 MCP 调用记录为 `ToolCallRecord`
4. 输出统一写成 `ReplyRecord`
5. 请求以 `TerminalResult` 收口

这条路径不要求 workflow，不要求 `DurableTask`，但仍然必须可审计。

### 6.2 Promote to DurableTask

当 `PromotionDecision` 命中硬规则时，入口阶段应做以下动作：

1. 写入本次 `PromotionDecision`
2. 由 `Reception`/模型给出 workflow 建议或候选集合
3. BUS 依据路由键优先级、主成功条件和 manifest 约束裁决是否存在唯一可接受 workflow
4. 只有在存在唯一可接受 workflow 时，BUS 才执行单个原子动作：把 request 标成 `Promoted`，并同时创建 `DurableTask`、active `WorkflowBinding` 和必要的首个 `StepExecution`
5. 把 route keys、外部对象引用、上下文摘要和必要的 artifact 交给 durable workflow

第一版不允许出现“request 已 promote，但 task 还没绑定 workflow”的 durable 中间态。

### 6.3 补充信息与等待人类

入口阶段要区分两类“不足以继续”的情况：

- 低风险只读请求只差少量条件时，可以保持 `EphemeralRequest=Open` 并发起追问
- 已命中 promote 硬规则、但缺少执行输入或 workflow 无法唯一归属时，不得靠模型硬猜；必须先进入补充信息或人工路由路径

也就是说，“需要追问”不自动等于“需要 Task”，而“需要 durable governance”也不等于“可以在 workflow 归属不明时先开跑”。第一版要求先解决唯一绑定，再进入业务 workflow；在这之前只能补充信息、转人工，或停留在入口审计边界内。

## 7. Request 级最小审计

旧 CDR 最大的问题之一，是把 direct answer path 写成几乎没有治理的同步小任务。新模型下，request 级最小审计必须落成可以逐项核对的矩阵：

| 审计记录类型 | 何时必须出现 | 生成者 | 最小字段 | 关联对象 | 缺失后的处理 |
| --- | --- | --- | --- | --- | --- |
| `ExternalEvent` | 每次外部输入进入 BUS 时 | BUS | `event_id`、`event_type`、route keys、原始来源引用、接收时间 | `EphemeralRequest` 或既有对象 | 入口事件不得继续处理；标记为 ingest 异常 |
| `PromotionDecision` | 每个新建或被重新评估的 `EphemeralRequest` | `Reception` 提议，策略层裁决，BUS 落库 | `intent_kind`、effects、execution、governance、`decision`、`reason_codes`、`confidence` | `EphemeralRequest` | 不允许进入直答或 promote；必须重新评估 |
| `ContextPack` | 发生上下文裁剪、转交 helper 或准备 promote 时 | `Reception` 或主执行实例 | `context_pack_id`、输入来源、裁剪摘要、对象引用、版本/时间戳 | `EphemeralRequest`、`AgentDispatch`、`DurableTask` | 仅允许在无需上下文转交的最简单直答场景缺省；否则阻断执行 |
| `AgentDispatch` | request 内唤起 helper / reviewer / read-only worker 时 | 主执行实例 | `dispatch_id`、`goal`、`expected_outputs`、`allowed_tools`、`allowed_mcp`、`return_to` | `EphemeralRequest`、可选 `ContextPack` | helper 输出不得采信；视为审计缺口 |
| `ToolCallRecord` | 每次工具调用或只读 MCP 调用时 | 工具运行器 / BUS | 工具名、参数摘要、结果摘要、开始/结束时间、调用方 | `EphemeralRequest`、可选 `AgentDispatch` | 结果不得作为最终答复依据 |
| `ReplyRecord` | 对用户或外部会话产生可见回复时 | 主执行实例 | `reply_id`、目标会话/对象、回复摘要、引用的证据或 artifact | `EphemeralRequest` | 不允许把回复视为已完成 |
| `TerminalResult` | request 收口为 `Answered` 或 `Expired` 时 | BUS | 终态、终态原因、关键输出摘要、完成时间 | `EphemeralRequest` | request 不得进入终态 |

补充约束：

- 只要允许 `Reception` 在 request 内拉起 helper，就必须留下 `AgentDispatch`，否则 direct answer path 会变成审计盲区。
- `ReplyRecord` 和 `TerminalResult` 不能互相替代；前者记录对外可见输出，后者记录 request 如何收口。
- `PromotionDecision` 缺失时，不能通过“这个请求看起来很简单”来豁免治理。

## 8. 第一版入口映射表

下面这张表把四类典型请求映射到入口动作：

| 请求类型 | 关键 Promotion 信号 | 入口结果 | 候选 workflow 建议 |
| --- | --- | --- | --- |
| 公开信息查询 | 全部只读、无持久对象、无治理 gate | 留在 `EphemeralRequest` | 无 |
| 集群只读查询 | 只读 MCP、无持久对象、无预算 gate | 留在 `EphemeralRequest` | 无 |
| 代码修改 / 新功能 | `external_write=true`、`multi_step=true`、通常 `recovery_required=true` | promote 成 `DurableTask` | `issue-delivery` |
| 科研探索 / 评测 | `async=true`、`budget_required=true`、`recovery_required=true` | promote 成 `DurableTask` | `research-exploration` |
| 定时任务创建/修改 | `create_persistent_object=true` 或 `external_write=true` | promote 成 `DurableTask` | `schedule-management` |
| workflow 变更 | 高风险控制面对象、通常 `external_write=true` | promote 成 `DurableTask` | `workflow-management` |

这里的“候选 workflow 建议”不是静态分派表。真正的绑定仍然来自：

1. `PromotionDecision` 命中 promote 硬规则
2. `Reception`/模型提出 workflow 建议
3. BUS 按 manifest 机器字段校验并接受该建议

## 9. 入口阶段的人机边界

入口阶段的人机边界应遵守三个原则：

- 人类自然语言只是候选意图来源，不是执行授权
- `Reception` 可以建议，但不能绕过策略层做最终裁决
- 控制面对象不能走隐藏捷径，必须通过正式 workflow 和 MCP 改动

因此，下面这些行为都应该被明确禁止：

- `Reception` 直接在对话里偷偷改系统配置
- 只因为“看起来像简单请求”就跳过 `PromotionDecision`
- 直接把 workflow 或定时任务修改绑定到普通对话线程，而不是对象引用
- 在 request 内唤醒看不见的 helper 且不留审计

## 10. 本文交接点

本文只关心入口阶段，最终只会产出两类结果：

- direct answer path：`EphemeralRequest` 进入 `Answered`
- durable path：request 被 promote，`DurableTask` 进入 `NewTask -> Active` 并完成 workflow 绑定

从这一点开始，后续 durable workflow 的细节进入 [issue_workflow_detail.md](./issue_workflow_detail.md)。
