# Conceptual Design Report

`docs/cdr` 承接 [draft.md](../draft.md) 和 [typical_cases.md](./typical_cases.md)，目标不是重复顶层设计，而是把第一版 Alice 的核心概念翻译成可评审的 CDR 叙事。

这版 CDR 明确建立在新模型上，而不是旧的“所有入口先建 `Task`、再靠若干固定 Agent 往前推”的模型上。当前版本以以下边界为准：

- 入口对象先分成 `EphemeralRequest` 和 `DurableTask`
- `Reception` 不直接裁决，只产出 `PromotionDecision`
- 是否 promote 由策略层依据结构化字段和只读 allowlist 决定
- `Skill` 是 agent 可读能力包，`Workflow` 是 BUS 管理的受控流程契约
- 外部副作用统一走 `outbox + MCP`
- 控制面对象必须有确定性路由键，而不是退化成普通对话线程

## CDR 要回答的问题

这组文档主要回答五件事：

- 用户请求进入 BUS 后，如何在直接回答路径和 durable workflow 路径之间分流
- `EphemeralRequest`、`PromotionDecision`、`DurableTask`、`WorkflowBinding` 的职责边界是什么
- 第一版四类 durable workflow 各自的入口条件、step 图、gate 和人工介入点是什么
- 人类如何通过 IM、Web、issue、PR、审批卡片与系统协作
- 恢复、审计、预算、取消和控制面变更怎样被统一纳入 BUS 治理

这组文档仍然是概念设计，不下沉到 Prompt 文本、存储表结构或 MCP 报文字段。

## 文档目录

- [entry_processing_detail.md](./entry_processing_detail.md)：入口路由、`PromotionDecision`、`EphemeralRequest` 直答路径，以及 promote 到 durable workflow 的交接
- [issue_workflow_detail.md](./issue_workflow_detail.md)：代码交付与研究探索两类 durable workflow 的细化
- [control_plane_workflow_detail.md](./control_plane_workflow_detail.md)：控制面 durable workflow 的细化，覆盖 `schedule-management` 与 `workflow-management`
- [typical_cases.md](./typical_cases.md)：六个典型案例，用于人工核对实现是否符合顶层设计

## 推荐阅读顺序

1. 先读 [draft.md](../draft.md)，理解 BUS 边界、对象模型和全局约束。
2. 再读 [typical_cases.md](./typical_cases.md)，建立对六个案例的直觉。
3. 然后读 [entry_processing_detail.md](./entry_processing_detail.md)，看入口如何产生 `PromotionDecision` 并分流。
4. 接着读 [issue_workflow_detail.md](./issue_workflow_detail.md)，看代码交付与研究探索如何进入 durable workflow。
5. 最后读 [control_plane_workflow_detail.md](./control_plane_workflow_detail.md)，看控制面变更如何被受控发布。

## 共享前提

评审时建议先看下面这张统一规则表；后续三篇细化文档默认继承这些规则，只补充本文件特有约束。

| Rule ID | 强制规则 | 由谁裁决/执行 | 主要展开位置 | 评审时重点看什么 |
| --- | --- | --- | --- | --- |
| `R1` | 所有输入先成为 `ExternalEvent`；先命中已有对象，未命中才创建新的 `EphemeralRequest` | BUS | [entry_processing_detail.md](./entry_processing_detail.md) | 有没有把所有事件错误地理解成“默认新建 request” |
| `R2` | `Reception` 只能产出 `PromotionDecision`，不能自己最终裁决 promote | `Reception` + 策略层 | [entry_processing_detail.md](./entry_processing_detail.md) | 有没有让模型主观决定治理边界 |
| `R3` | 只有命中只读 allowlist 的请求才能留在 direct answer path | 策略层 | [entry_processing_detail.md](./entry_processing_detail.md) | 有没有把高风险或异步请求留在 request 内执行 |
| `R4` | `DurableTask` 一旦创建，就必须绑定不可变 `WorkflowBinding`；若允许重规划，也只能追加新 binding 记录，不能原地覆盖旧 binding | BUS | [issue_workflow_detail.md](./issue_workflow_detail.md) | 有没有把 workflow 绑定写成可漂移的文本选择 |
| `R5` | 所有外部副作用都必须走 `outbox + MCP` | BUS + MCP | [issue_workflow_detail.md](./issue_workflow_detail.md), [control_plane_workflow_detail.md](./control_plane_workflow_detail.md) | 有没有出现执行实例直接改外部系统的捷径 |
| `R6` | 控制面对象必须使用专属路由键，不能退化成普通对话线程 | BUS | [entry_processing_detail.md](./entry_processing_detail.md), [control_plane_workflow_detail.md](./control_plane_workflow_detail.md) | 有没有遗漏 `scheduled_task_id` / `control_object_ref` / `workflow_object_ref` |
| `R7` | 人类补充、审批、预算恢复、取消都是新的 `ExternalEvent` | BUS | [issue_workflow_detail.md](./issue_workflow_detail.md), [control_plane_workflow_detail.md](./control_plane_workflow_detail.md) | 有没有偷偷在内存里直接改状态 |
| `R8` | 旧 workflow revision 绑定中的 task 不会自动漂移到新 revision | BUS | [control_plane_workflow_detail.md](./control_plane_workflow_detail.md) | 有没有把控制面发布写成影响正在运行 task 的热更新 |

对 `R1` 需要补一条容易被误读的说明：

- 定时触发同样先成为 `ExternalEvent`，不会绕开统一入口模型。
- scheduler 只负责产生可信系统事件和必要路由键；后续仍由 BUS 按统一命中规则处理。
- 对“命中后必然 promote”的系统事件，BUS 可以在同一事务里创建短生命周期 request 包络并立即写出 `PromotionDecision=promote`，但这不构成调度专用旁路。

两份细化文档共享以下前提：

- `BUS` 是真实状态与治理边界，外部平台只提供协作表面和事件来源。
- 第一版以“所有输入先成为 `ExternalEvent`，先命中已有 `DurableTask`/`EphemeralRequest`，未命中才创建新的 `EphemeralRequest`”为总入口模型。
- `EphemeralRequest` 只承载低风险、只读、短时请求；它有自己的审计，但不进入 durable task 顶层状态机。
- `DurableTask` 才进入 `NewTask / Active / WaitingHuman / Succeeded / Failed / Cancelled` 顶层状态。
- `PromotionDecision` 是结构化对象，不是模型一句“我觉得这是简单查询”的主观结论。
- 只有命中只读 allowlist 的请求才能留在 `EphemeralRequest` 内；信息不全或高风险请求应保守 promote，或进入补充信息路径。
- `Workflow` 绑定是不可变 revision 绑定；旧 task 不会因为 workflow 发布新版本而自动漂移。
- 若策略允许重规划，也只能在显式审批点追加新的 `WorkflowBinding` 记录并 supersede 旧 binding；旧 binding、旧 step 和旧 outbox 审计都必须保留。
- `Skill` 不等于 `Workflow`，也不等于运行时 agent 实例名。
- 所有外部副作用都先写 `outbox`，再经由 MCP 执行。
- 直接回答路径也必须留下 `PromotionDecision`、必要的 `AgentDispatch`、`ToolCallRecord`、`ReplyRecord` 和 `TerminalResult` 审计。
- 控制面请求必须优先使用 `scheduled_task_id`、`control_object_ref`、`workflow_object_ref` 等路由键，而不是普通对话线程。
- 如果同一事件同时带 `reply_to_event_id` 和显式控制面对象键，且两者指向不同域，BUS 不得把控制面动作吞进被回复的普通 task；应拆出控制面 request/task，或先追问澄清。
- 人工补充、审批、预算恢复、取消，本质上都是新的 `ExternalEvent`。

## CDR 统一命名约定

为避免 CDR 再次引入一套与 `draft.md` 冲突的影子术语，本目录统一遵守下面的命名约定：

- 不再把所有入口对象都称为 `Task`；入口默认先是 `EphemeralRequest`。
- 不再引入 `sync_direct`、`sync_mcp`、`async_issue` 这类旧路由名；统一改用 `PromotionDecision` + workflow 名称。
- 不再把 `task_created`、`issue_created`、`issue_sync_pending` 当作核心状态名；这些如果需要出现，只能作为局部交接事实，而不是全局状态机。
- 运行时角色使用 `reception`、`leader`、`worker`、`reviewer`、`evaluator`；必要时可以举 `repo-reader`、`patch-writer` 这类 `agent_label` 例子，但它们不是系统级角色。
- `issue-delivery`、`research-exploration`、`schedule-management`、`workflow-management` 是 workflow 名称，不是主程序里的固定阶段枚举。

## 核心对象视角

从 CDR 视角，第一版最重要的对象只有这些：

- `ExternalEvent`：外部消息、webhook、评论、审批和定时触发的统一入口
- `EphemeralRequest`：低风险、只读请求的短生命周期容器
- `PromotionDecision`：是否 promote 的结构化判断
- `DurableTask`：需要治理、恢复和 workflow 的长期对象
- `WorkflowBinding`：不可变 workflow revision 绑定
- `ContextPack` / `AgentDispatch`：显式的子 agent 调度契约
- `ToolCallRecord` / `ReplyRecord` / `TerminalResult`：直接回答路径和 workflow 路径共享的审计骨架
- `OutboxRecord`：所有外部副作用的出站记录

## 文档边界

- [entry_processing_detail.md](./entry_processing_detail.md) 只讨论入口阶段，止步于两种交接点：
  - `EphemeralRequest` 直接得到终态回复
  - request 被 promote 成 `DurableTask` 并完成 workflow 绑定
- [issue_workflow_detail.md](./issue_workflow_detail.md) 讨论 promote 之后的代码交付与研究探索 workflow，不再回退去重写入口路由
- [control_plane_workflow_detail.md](./control_plane_workflow_detail.md) 讨论调度对象与 workflow 定义这两类控制面 workflow

如果后续继续细化，实现上更好的做法是继续按“先说明承接自哪个对象，再说明止步于哪个对象”的方式扩写，而不是重新引入第二套状态机词汇。
