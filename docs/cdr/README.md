# Conceptual Design Report

`docs/cdr` 用来承接 [draft.md](../draft.md) 的系统草案。可以把它理解为“根据 `draft.md` 写出的概念设计文档”：它把第一版 Alice 的关键工作流拆成可评审的概念设计，并把近期新增的审核租约、预算熔断、评测恢复、取消传播和人工操作面统一下沉。

## 目标

这组文档主要回答四件事：

- BUS、Issue、PR、评论分别是什么角色
- 各 Agent 的职责边界和交接点是什么
- 第一版状态机应该如何命名，避免实现时各模块各说各话
- 人类应通过飞书、Web、issue、PR 在哪些节点介入系统

这些文档仍是概念设计，不下沉到 Prompt、具体存储格式或 MCP 接口报文字段。

## 文档目录

- [entry_processing_detail.md](./entry_processing_detail.md)：入口接待、分流策略、同步与异步边界，以及从用户请求进入 BUS 到 `issue_created` 的交接过程
- [issue_workflow_detail.md](./issue_workflow_detail.md)：Issue 驱动的计划、审核、编码、可选评测、代码审核、预算恢复、合并与关闭工作流

## 推荐阅读顺序

1. 先读 [draft.md](../draft.md)，理解 BUS 真源、MCP 独立进程、审核租约、预算硬熔断这些总前提。
2. 再读 [entry_processing_detail.md](./entry_processing_detail.md)，确认入口怎样受理请求、建 Task、分流到同步或 issue 通道。
3. 最后读 [issue_workflow_detail.md](./issue_workflow_detail.md)，看 `issue_created` 之后如何进入 Planner、审核、编码、评测、代码审核和合并流程。

## 共享前提

两份细化文档共享以下原则：

- `BUS` 统一指 Alice 的核心运行时，而不是狭义消息队列。
- BUS 中的任务状态是系统真实状态；Issue、PR、评论是协作界面和外部事件来源，不是状态真源。
- BUS 在概念上分为 `Event Bus` 与 `State Store`；真实状态写入必须经过 `State Store`。
- `task_version` 用于单任务乐观锁和并发写保护，`global_hlc` 预留给跨任务因果追踪与后续分布式扩展。
- 外部事件按“至少一次投递”处理，必须验签、幂等去重并持久化。
- 所有外部副作用都先写入本地持久化 `outbox`，再通过 MCP 执行，完成后再回写 BUS。
- 外部副作用必须携带幂等键，并把幂等键作为 MCP 协议契约透传到底层执行器。
- BUS 在高负载下会进入背压，对低优先级事件执行拒绝或延迟入队，并持续告警直到积压缓解。
- 所有涉及仓库、凭据、系统设置的动作都通过受限环境中的 Agent 和 MCP 完成。
- 审核必须绑定明确版本：计划审核绑定 `plan_version`，代码审核绑定 `pr_head_sha`。
- 每一轮审核都必须固定审核 Agent 集合、截止时间和心跳租约；只有在收齐 verdict，或未完成席位被判定缺席后，BUS 才聚合本轮结论。
- 审核冲突默认按不通过处理，连续 3 轮仍不一致再升级人工仲裁。
- `waiting_human` 是共享挂起状态，但必须携带 `waiting_reason`，至少区分信息补充、风险确认、预算决策和恢复处理。
- 对需要实验验证的任务，`evaluation` 是主流程的一部分，不并入代码审核。
- `UsageLedger` 负责预算跟踪；预算剩余小于 0 时，BUS 必须触发硬熔断，停止新的 `coding` / `evaluation`，并尝试终止已有评测作业。
- `OpsReadModel` 负责把任务状态、预算、MCP 健康和队列积压导出为只读展示区视图。
- 状态回退、人工取消或熔断触发时，`CancellationToken` 必须传播到 Agent 与 MCP 层。
- 人工审核、确认、驳回与仲裁优先通过飞书卡片加按钮完成，Web 管理页作为补充后台；所有按钮都必须带 `confirmation_token` 或等价幂等键。
- 第一版优先追求可解释、可恢复、可观测，而不是高度动态编排。

## 共享状态约定

为避免状态机歧义，`docs/cdr` 统一使用以下核心状态名：

- `task_created`：BUS 已正式建任务
- `waiting_human`：等待人类补充信息、风险确认、预算决策或恢复指令
- `issue_sync_pending`：任务已建，但仅在缺少现成外部 issue 时，系统仍在创建或补建 issue 镜像
- `issue_created`：任务已绑定外部 issue 载体，进入下游 issue 驱动工作流
- `planning` / `plan_audit` / `coding` / `evaluation` / `code_audit`：计划、审核、实现、评测和代码审核阶段
- `budget_exceeded`：预算或资源命中硬上限，系统暂停自动推进
- `pending_arbitration`：连续 3 轮审核仍不一致，等待人工仲裁
- `merge_approval_pending` / `merging` / `merged`：等待合并审批、实际合并执行中、外部已完成合并
- `closed`：任务生命周期正常结束
- `failed` / `cancelled`：异常终止或人工取消

其中，第一版统一要求所有入口请求在进入执行、等待或异步分发前，都先由 BUS 创建或绑定正式 `Task` 并拿到 `task_id`。同步请求和 `waiting_human` 请求因此也是短生命周期 `Task`。

## 案例细化

下面三个案例不是实现脚本，而是给评审者看的“人怎么操作系统”的概念样例。

### 案例 1：查询今日天气

适用文档：

- 主要落在 [entry_processing_detail.md](./entry_processing_detail.md)

人如何发起：

1. 人类在飞书里直接发消息，例如“查询今日天气”。
2. 如果使用 Web 管理页，也可以在输入框中提交同样的请求。

系统如何响应：

1. 接入层把这条消息包装成 `UserRequest`，先写入 BUS。
2. `ReceptionAgent` 识别这是无持久副作用的轻量查询，建议走 `sync_direct`。
3. BUS 创建短生命周期 `Task`，进入 `task_created -> sync_running`。
4. 查询结果回写 BUS，并经回复链路发送回飞书或 Web。
5. 任务进入 `closed`。

人可以怎么继续操作：

1. 如果结果返回前，人类又在同一线程发送“改查上海明天”，入口层可以按 `coalescing_key` 合并成同一轮查询，避免重复执行旧请求。
2. 如果结果已经返回，人类再问一个新城市或新日期，系统通常创建一个新的短生命周期任务，而不是污染旧任务。
3. 如果请求范围过大，例如“给我做一份全国未来两周天气分析报告”，系统不会强行执行，而是回到 `waiting_human`，要求人类缩小范围。

人看到的交互面：

1. 飞书或 Web 中能看到“已接收”“处理中”“已完成”这类简短反馈。
2. 一般不需要 issue、PR、审批卡片。
3. 如果入口触发背压，用户会看到“稍后重试”的反馈，而不是静默超时。

### 案例 2：修改明确 bug 或增加新功能

适用文档：

- 入口与任务建立落在 [entry_processing_detail.md](./entry_processing_detail.md)
- 计划、编码、审核和合并落在 [issue_workflow_detail.md](./issue_workflow_detail.md)

人如何发起：

1. 最直接的方式是在 GitHub / GitLab 新建 issue，写清楚 bug、期望行为、影响范围和仓库位置。
2. 也可以在飞书或 Web 中提交“修复某仓库某模块的 bug”，系统再为其创建内部任务并补建 issue 镜像。

系统如何响应：

1. 如果请求本身来自 GitHub / GitLab 现成 issue，BUS 创建内部 `Task` 后直接绑定该 issue，进入 `task_created -> issue_created`。
2. 如果请求来自飞书或 Web，BUS 先创建内部 `Task`，进入 `task_created -> issue_sync_pending`；Git 平台镜像建立成功后再进入 `issue_created`。
3. `PlannerAgent` 阅读 issue 和代码上下文，产出 `PlanArtifact` 并回帖到 issue。
4. `PlanAuditAgent` 对当前 `plan_version` 审核；BUS 只有在该轮 verdict 收齐，或未完成席位因租约/截止时间被判定缺席后，才聚合结论。
5. 审核通过后，`CodingAgent` 创建或更新唯一活动 PR。
6. 如果任务不需要评测，直接进入 `code_audit`；需要评测则先进入 `evaluation`。
7. 代码审核通过且 CI、分支保护满足后，任务进入 `merge_approval_pending` 或 `merging`。

人可以怎么操作：

1. 在计划阶段：
   - 直接回复 issue 评论，补充需求、改范围、加约束。
   - 通过飞书卡片或 Web 后台点击“批准计划”“打回重做”“拆成新任务”。
   - 如果改动改变了需求边界，系统回到 `planning`，旧 `plan_version` 结论自动失效。
2. 在编码阶段：
   - 在 issue 或 PR 评论里补充“小改动”“顺手改掉某个接口名”这类请求。
   - 如果只是实现细节微调，系统通常回到 `coding` 继续演进同一个活动 PR。
   - 如果人类要求大改目标，例如从“修 bug”变成“重构整套模块”，系统应回到 `planning`，必要时拆成新任务。
3. 在代码审核阶段：
   - 人类可以阅读 PR、留评论、要求继续修改。
   - 多个审核结论冲突时，前 2 轮默认先打回；连续 3 轮还冲突，人类会收到仲裁卡片。
4. 在合并阶段：
   - 如果策略要求人工审批，人类通过飞书或 Web 点击“批准合并”。
   - 该动作必须带 `confirmation_token`；重复点击只会得到“已处理/已失效”。
5. 在任何阶段：
   - 人类都可以点击“取消任务”“转人工处理”“要求阶段报告”。

人看到的交互面：

1. issue 是需求与计划讨论面。
2. PR 是代码与审核讨论面。
3. 飞书卡片和 Web 管理页负责高风险确认、仲裁、合并审批和异常恢复。
4. 展示区应显示当前 `task_id`、状态、当前 `plan_version` / `pr_head_sha`、阻塞原因和最近一次状态变化。

实现上应特别注意的人机边界：

1. 人类在 `code_audit`、`merge_approval_pending`、`merging` 阶段追加新需求时，系统要先冻结自动推进，再决定回退到 `coding` 还是 `planning`。
2. 如果 PR 更新时遇到远端非快进变化，系统只允许自动 rebase 或回退到 `coding` 解决冲突，不允许 force push 覆盖历史。

### 案例 3：科研探索任务，目标是反复实验直到达到指标

适用文档：

- 入口与任务建立落在 [entry_processing_detail.md](./entry_processing_detail.md)
- 评测、预算和人工恢复落在 [issue_workflow_detail.md](./issue_workflow_detail.md)

人如何发起：

1. 在 issue、飞书或 Web 中给出研究目标，例如“在指定数据集上把 F1 提升到 0.92 以上，预算 200 GPU 小时”。
2. 人类应尽量一次写清楚：
   - 数据集或数据来源
   - 目标指标和通过阈值
   - 可接受的预算和资源上限
   - 是否最终需要合入主干，还是只要阶段报告

系统如何响应：

1. 如果研究目标直接在 GitHub / GitLab issue 中提出，BUS 创建 `Task` 后直接绑定该 issue；如果目标来自飞书或 Web，则先建 `Task` 再补建 issue 镜像。
2. `PlannerAgent` 产出探索计划和 `EvalSpec`，明确数据集版本、评测配置、通过阈值、预算上限、资源规格、随机种子策略和停止条件。
3. `PlanAuditAgent` 审核探索计划。
4. `CodingAgent` 修改算法、脚本或实验编排代码，更新活动 PR。
5. BUS 根据 `EvalSpec` 进入 `evaluation`，由 `EvaluationAgent` 通过 Cluster MCP 发起作业。
6. 评测通过则进入 `code_audit`；不通过则回到 `coding`；预算触顶则进入 `budget_exceeded -> waiting_human`。

人可以怎么操作：

1. 在计划 / `EvalSpec` 阶段：
   - 回复 issue，修改目标指标、数据集、预算或停止条件。
   - 通过卡片或 Web 后台确认“允许使用哪些资源”和“预算上限是多少”。
2. 在评测阶段：
   - 在展示区查看当前实验的 `pr_head_sha`、数据集版本、指标摘要、GPU/CPU 消耗和预算剩余。
   - 收到预算告警后，点击“追加预算”“暂停实验”“输出阶段报告”“停止任务”。
   - 如果代码未变，只是恢复资源或加预算，系统可以从 `waiting_human` 直接回到 `evaluation`，不必重走 `coding`。
3. 在探索方向变化时：
   - 人类可以说“把主指标从 AUC 改成 F1”“换基线”“别再搜这个方向”。
   - 这类操作通常回到 `planning`，旧 `EvalSpec`、旧评测结果和旧审核结论都只保留审计价值。
4. 在结果收口时：
   - 如果任务目标是“产出报告”，人类可以直接要求结束自动实验并生成阶段总结。
   - 如果目标是“合入主干”，仍需经过代码审核和必要的合并审批。

人看到的交互面：

1. issue 负责记录目标、计划和方向变更。
2. PR 负责记录当前实现版本。
3. 展示区负责显示评测进度、预算、资源使用、最近一次结论和阻塞原因。
4. 飞书卡片 / Web 后台负责预算追加、恢复评测、停止探索和合并审批。

实现上应特别注意的人机边界：

1. 预算不是提示字段，而是真正的状态机约束；预算剩余小于 0 时，BUS 必须停止新的 `evaluation`，并尝试终止已运行的作业。
2. 人类点击“继续评测”时，系统应判断代码和 `EvalSpec` 是否变化；没变化才允许直接恢复到 `evaluation`。
3. 如果人类要求“先出报告再说”，系统应撤销活动 `CancellationToken`，停止新的实验投递并清理已有集群作业。

## 文档边界

- [entry_processing_detail.md](./entry_processing_detail.md) 只负责入口阶段，止步于 `issue_created`
- [issue_workflow_detail.md](./issue_workflow_detail.md) 从 `issue_created` 开始，负责后续代码与探索工作流

如果后面新增文档，建议继续保持这个边界写法：先说明承接自哪个状态，再说明本文件止步于哪个状态。
