# 典型案例详解

本文是 [../draft.md](../draft.md) 的配套案例文档，用于人工核对实现是否符合顶层设计。

先固定术语：
- `Skill`：agent 可读的能力包，例如 Codex 的本地 skill。它属于 agent 运行时能力。
- `Workflow`：BUS 管理的受控流程定义，包含 step 图、gate、回退规则和 artifact 契约。
- `PromotionDecision`：`Reception`/模型产出的结构化升级判断，供策略层裁决是留在 `EphemeralRequest` 还是 promote 成 `DurableTask`。
- `MCP`：外部副作用适配层。

这四个概念必须分开：
- agent 只有在 `PromotionDecision` 命中只读 allowlist 后，才可以直接使用自己的 `Skill` 完成低风险、只读、一次性任务。
- 只有当任务需要外部写操作、长流程、多 agent 协作、审批或预算控制时，才升级成 `Workflow`。
- 一旦升级成 `Workflow`，所有外部副作用都必须走 `outbox + MCP`。
- 因此入口对象也应分成两类：短时只读请求先落到 `EphemeralRequest`，需要强控制时再 promote 成 `DurableTask`。

## 共通规则

1. 所有入口都先写成 `ExternalEvent`，再进入 BUS。
2. BUS 先按路由键命中已有 `EphemeralRequest` 或 `DurableTask`；如果都不命中，就创建新的 `EphemeralRequest`。
3. `Reception` 或辅助模型先抽取结构化事实并写入 `PromotionDecision`；是否 promote 由策略层裁决，不由单个 agent 主观拍板。
4. `Skill` 不等于 `Workflow`。前者给 agent 用，后者给 BUS 用。
5. 任何外部副作用都必须先写 `OutboxRecord`，再通过 MCP 执行。
6. 人类补充说明、打回、改目标、追加预算，本质上都是新的 `ExternalEvent`。
7. 只有 `DurableTask` 才进入 `NewTask`、`Active`、`WaitingHuman`、`Succeeded`、`Failed`、`Cancelled` 这些顶层状态；业务 step 由 workflow manifest 定义。
8. 对直接查询类请求，BUS 至少记录 `PromotionDecision`、必要的 `AgentDispatch`、`ToolCallRecord`、`ReplyRecord` 和 `TerminalResult`，不强制要求 workflow 字段。
9. 对“是否命中同一个正在运行的对象”这件事，必须使用固定优先级路由键，例如 `reply_to_event_id`、`repo_ref + issue_ref/pr_ref`、`scheduled_task_id/control_object_ref`、`workflow_object_ref`、`conversation_id + thread_id`、`coalescing_key`。

## 案例 1：查询天气

### 场景

人类发来一句自然语言消息：

`查询上海明天天气`

这个任务的特点是：
- 简单
- 一次性
- 只读
- 不需要代码仓库写权限
- 不需要审批

因此它通常会得到一个“留在 `EphemeralRequest` 内直接完成”的 `PromotionDecision`，随后由 `Reception` agent 使用自己已挂载的 `Skill` 完成。

### 详细流程

1. 入口层接收到消息后，生成一个新的 `ExternalEvent`。
2. 该事件至少记录：来源渠道、发送人、原始文本、接收时间、`conversation_id`、`thread_id`、验签结果或来源可信度。
3. BUS 接收事件后，先做去重、鉴权和基础风控。
4. 若没有命中已有活跃对象，BUS 创建一个新的 `EphemeralRequest`，只记录轻量事实，例如：
   - 来源是 IM 消息
   - 意图可能是信息查询
   - 不需要仓库写权限
   - 风险级别低
5. `Reception` agent 读取 request 上下文，并查看自己当前可用的 `Skill`。
6. `Reception` 先产出一个 `PromotionDecision`，至少表明 `effects.external_write=false`、`effects.create_persistent_object=false`、`execution.async=false`、`execution.multi_step=false`、`governance.approval_required=false`。
7. BUS 持久化该 `PromotionDecision`；因为命中了只读 allowlist，所以该请求保持在 `EphemeralRequest` 内执行，不升级成 workflow。
8. agent 使用自己的 `Skill` 解析查询条件，例如：
   - 城市：上海
   - 日期：明天
   - 输出要求：简洁天气摘要
9. 如果 agent 当前权限允许调用外部查询工具，则直接发起查询 toolcall。
10. BUS 记录这次 `ToolCallRecord` 的参数、状态和返回值摘要；若返回值很大，则保存引用而不是全文塞进 request。
11. agent 根据 toolcall 返回值生成最终回复文本。
12. BUS 写入 `ReplyRecord` 和 `TerminalResult`，随后通过回复通道发送给人类；该 request 进入 `Answered`，而不是创建 `DurableTask` 再走顶层状态机。

如果 `Reception` 想唤醒一个低成本 helper 来辅助解析条件，也必须显式传参，而不是只丢一句话过去。最小应包含：
- `agent_label`，例如 `weather-parser`
- `requested_role=helper`
- `goal`，例如“抽取城市、日期、输出格式”
- `ContextPack`，至少含原始用户消息和最近一轮对话切片
- `expected_outputs`，例如 `city/date/output_style`
- `allowed_tools`，通常只允许只读查询或不允许工具
- `write_scope=read_only`

BUS 还应把这次 helper 调用记录成 request 级 `AgentDispatch`，而不是让直接回答路径变成审计盲区。

### 人类中途修改请求

如果人类紧接着又发一句：

`改查北京后天`

系统应这样处理：

1. 第二条消息再次进入 BUS，形成新的 `ExternalEvent`。
2. BUS 用固定优先级命中：先看 `reply_to_event_id`，再看 `conversation_id + thread_id`，最后看 `coalescing_key` 是否仍在有效窗口内。
3. 如果第一个 request 仍处于 `Open`，则可以命中同一个 `EphemeralRequest`，由 `Reception` agent 把这条消息视为补充输入。
4. agent 重新使用自己的 `Skill` 解析新条件后重做查询。
5. 如果第一个 request 已经 `Answered`，则系统应创建新的 `EphemeralRequest`；不应任意复用已结束对象，更不应为了这类查询额外创建 `DurableTask`。

### 需要重点检查的实现点

- 主程序不应写死“天气查询专用状态机”。
- 这类任务应先走 `EphemeralRequest`，而不是先建 durable task。
- 这类任务可以不绑定 workflow，直接由 `Reception` agent 使用自身 `Skill` 完成，但前提是已经有结构化 `PromotionDecision` 证明它命中了只读 allowlist。
- 即使是最简单的查询，也应该留下 `ExternalEvent`、`PromotionDecision`、必要的 `AgentDispatch`、`ToolCallRecord`、`ReplyRecord` 和 `TerminalResult` 审计。
- 如果查询调用了外部服务，其权限边界仍应来自 agent 当前权限，而不是执行实例自行突破。

## 案例 2：查询 GPU 集群排队情况

### 场景

人类发来一句自然语言消息：

`帮我查一下 GPU 集群现在排队情况`

这个任务的特点是：
- 一次性
- 只读
- 不需要长流程
- 不需要审批
- 但数据来源不是公网搜索，而是受控的集群系统

因此它通常也不需要升级成 workflow，但不应像查天气那样使用开放式公网查询，而应先形成一个“留在 `EphemeralRequest` 内”的 `PromotionDecision`，再由 `Reception` agent 使用自身 `Skill`，并通过只读的 Cluster MCP 查询。

### 详细流程

1. 入口层接收到消息后，生成一个新的 `ExternalEvent`。
2. BUS 做去重、鉴权和基础风控，再按 `reply_to_event_id`、`conversation_id + thread_id`、`coalescing_key` 等路由键决定是命中已有对象还是创建新的 `EphemeralRequest`。
3. request 初始信息至少记录：
   - 来源是自然语言消息
   - 意图可能是集群状态查询
   - 不需要仓库写权限
   - 风险级别低
4. `Reception` agent 读取 request 上下文，并查看自己当前可用的 `Skill`。
5. `Reception` 先产出一个 `PromotionDecision`，至少表明 `external_write=false`、`create_persistent_object=false`、`async=false`，并且请求只需要只读 Cluster MCP。
6. BUS 持久化该 `PromotionDecision`；因为命中了只读 allowlist，所以请求保持在 `EphemeralRequest` 内执行。
7. agent 使用自己的 `Skill` 解析查询目标，例如：
   - 查询对象：GPU 集群
   - 查询维度：排队情况
   - 结果格式：简洁摘要，必要时附队列明细
8. 因为这个任务访问的是受控内部系统，agent 不应直接猜测结果，也不应改用公网搜索替代。
9. 正确路径应是调用只读的 Cluster MCP 接口，例如查询：
   - 各队列等待作业数
   - 用户自己的排队作业
   - 不同资源池的空闲/繁忙情况
   - 预计等待时间或最近调度状态
10. 这类查询是只读操作，因此通常不需要 `OutboxRecord`；但 toolcall 请求、状态和返回值摘要仍应留审计。
11. Cluster MCP 返回结果后，BUS 记录 `ToolCallRecord`；agent 使用自己的 `Skill` 把结果整理成人类可读的回复，例如：
   - 当前主队列排队 23 个作业
   - 你的作业有 2 个在排队
   - A100 队列拥堵，V100 队列较空
12. BUS 写入 `ReplyRecord` 和 `TerminalResult`，随后通过回复通道发送给人类；该 request 进入 `Answered`。

### 人类中途修改请求

如果人类继续发：

- `只看我自己的作业`
- `再告诉我 A100 队列情况`

系统应这样处理：

1. 新输入再次形成 `ExternalEvent`。
2. 如果原 request 仍在 `Open`，且 `reply_to_event_id` 或 `conversation_id + thread_id` 命中同一对象，则 `Reception` agent 可以把它视为同一查询任务的补充约束。
3. agent 重新使用自身 `Skill` 解析范围，然后再次通过只读 Cluster MCP 查询。
4. 如果原 request 已经结束，也应新建一个 `EphemeralRequest`；不需要为了复用而强行修改历史结果。

### 需要重点检查的实现点

- 这类任务通常不需要升级成 workflow，也不应先建 durable task；但是否允许留在 request 内，仍要经过结构化 `PromotionDecision`。
- 但它也不应像天气查询那样走开放式公网搜索，而应通过受控的只读 Cluster MCP。
- 只读查询通常不需要 `outbox`，因为没有外部副作用；但 `ExternalEvent`、`PromotionDecision`、必要的 `AgentDispatch`、`ToolCallRecord`、`ReplyRecord` 和 `TerminalResult` 仍应可审计。
- agent 可以使用自身 `Skill` 来组织查询和总结结果，但不能伪造集群状态。

## 案例 3：修改代码中的明确问题或增加新功能

### 场景

人类通过 GitHub issue、GitLab issue、issue 评论、PR 评论或消息提出代码需求，例如：

- `修复登录接口 500`
- `给列表页增加导出按钮`

这个任务的特点是：
- 明确涉及代码仓库
- 往往需要计划、实现、审阅，可能还需要 merge
- 会触发 GitHub/GitLab 外部副作用
- 可能需要多个 agent 分工

因此它不应由 `Reception` agent 直接回答，而应先形成一个命中 promote 硬规则的 `PromotionDecision`，再从入口 `EphemeralRequest` promote 成 `DurableTask`，并绑定某个不可变 revision 的 `issue-delivery` workflow，例如展示为 `issue-delivery@a1b2c3d`。

### 详细流程

#### A. 入口事件如何进入 BUS

1. 入口层接收到 issue、评论或消息后，先生成 `ExternalEvent`。
2. 该事件至少记录：
   - 来源渠道，例如 GitHub issue、GitLab issue、IM 消息
   - 外部对象引用，例如 `repo_ref`、`issue_ref`、`pr_ref`、评论 ID
   - 原始内容引用
   - `reply_to_event_id`、`conversation_id`、`thread_id`
   - 发送人身份
   - 验签结果或来源校验结果
3. BUS 对该事件做幂等检查，避免 webhook 重投造成重复建 task。
4. BUS 先按固定优先级路由：优先看显式 `task_id`、`reply_to_event_id`、`repo_ref + issue_ref/pr_ref`，最后才看对话线程键。
5. `Reception` agent 可以先利用自身 `Skill` 理解这是“代码修改/新功能”类请求，并抽取结构化事实。
6. 如果该事件已经命中某个既有 `DurableTask`，则直接路由给它；如果没有，就先创建一个轻量 `EphemeralRequest`，由后续 `PromotionDecision` 决定是否升级。

#### B. BUS 如何创建 task

1. `Reception` 先产出 `PromotionDecision`，至少表明 `effects.external_write=true`、`execution.multi_step=true`，并且通常伴随 `governance.recovery_required=true`。
2. 由于命中了 promote 硬规则，这类请求不能停留在 `EphemeralRequest`；因此 BUS 会把当前 request promote 成 `DurableTask`；`Task` 里只记录事实，不决定业务剧本。
3. task 初始信息通常包括：
   - 来源是代码仓库相关请求
   - 关联仓库和 issue/PR
   - 用户请求摘要
   - 初始风险级别
   - 是否需要仓库写权限
   - 是否可能需要人审

#### C. workflow 是怎么绑定的

1. `Reception` agent 基于 `PromotionDecision` 和自身 `Skill` 提交 workflow 建议，例如 `issue-delivery@a1b2c3d`，并附带它认为需要的能力和入口事实。
2. 该建议是候选项，而不是最终裁决；最终是否接受仍由 BUS 基于 manifest 字段校验。
3. BUS 校验这个 workflow 是否满足硬约束，例如：
   - `requires` 要求的 `repo_ref` 等上下文是否存在
   - `required_refs` 声明的对象引用是否齐备
   - `allowed_mcp` 是否允许后续使用 GitHub/GitLab MCP
   - 风险等级是否不超过 `max_risk`
   - `forbids` 中是否禁止当前场景
4. 校验通过后，BUS 接受这个建议，并绑定该 revision。
5. BUS 将该选择写入 `WorkflowBinding`，并把触发 promote 的 `PromotionDecision` 一并关联审计，持久化以下信息：
   - `workflow_id`
   - `workflow_source`
   - `workflow_rev`
   - `manifest_digest`
   - `workflow_ref`
   - manifest 快照
   - 绑定原因
   - `entry_step`
6. 从这一刻开始，这个 task 使用绑定时的 workflow 快照执行；即使 `issue-delivery` 后来继续演进，旧 task 也不会自动漂移。

#### D. 进入 `plan` step

1. workflow manifest 声明 `plan` step 需要哪些能力槽位，例如 leader。
2. 策略层按能力约束，从 `AgentRegistry` 选择 leader。
3. leader 可以继续使用自己挂载的 `Skill` 去读代码、拆解问题、生成计划。
4. leader 产出一个计划 artifact，内容通常包括：
   - 修改目标
   - 涉及模块
   - 风险点
   - 测试点
   - 是否需要新增或更新 PR
   - 是否需要评测或额外验证
5. 该 artifact 回写 BUS，并与本 step 的 `StepExecution` 关联。

#### E. `plan` 后是否审批

1. 如果 workflow manifest 规定 `plan` 后需要人工批准，则 BUS 创建 `ApprovalRequest`。
2. 审批需要几席 reviewer、截止时间多久，都来自 workflow manifest 或策略配置。
3. BUS 只负责创建 gate、跟踪状态、收集结果和记录审计。
4. BUS 不负责在主程序里写死“所有代码任务都必须 plan audit”。
5. 若需要 reviewer 意见，应通过后续 `review` step 或结构化 `review_result` 实现，而不是新增平台级 gate 类型。
6. 若 gate 通过，task 进入下一个 step；若不通过，则按 manifest 回退到 `plan`，或者进入 `WaitingHuman`。

#### F. 进入 `code` step

1. `code` step 开始后，leader 可以单兵执行，也可以按 workflow 规则组建子团队。
2. 例如：
   - 只读分析 worker 负责读代码和定位修改点
   - 实现 worker 负责生成候选 patch
   - 测试 worker 负责补测试或修测试
3. leader 唤醒这些 worker 时，必须提交显式的 `AgentDispatch`，至少带上：
   - `agent_label`，例如 `repo-reader`、`patch-writer`、`test-fixer`
   - `requested_role=worker`
   - `goal`，例如“定位登录接口 500 根因”或“补齐导出按钮测试”
   - `ContextPack`，至少含计划 artifact、相关代码路径、仓库引用、当前工作状态摘要
   - `expected_outputs`，例如 `analysis_notes`、`candidate_patch`、`test_notes`
   - `allowed_tools`、`allowed_mcp`、`sandbox_template`
   - `budget_cap` 和 `deadline`
   - `write_scope`
4. 这些 worker 在执行各自 step 时，也可以使用自己挂载的 `Skill`，但它们的权限上界由 BUS、当前 `AgentDispatch` 和 workflow manifest 共同限制。
5. 默认只有主写实例可以提交最终 patch、分支引用或 PR 引用；worker 返回的是候选结果，是否采纳由 leader 决定。
6. 如果需要创建分支、推送提交、创建 PR、更新 PR 评论，这些都不是执行实例直接写外部系统。
7. 正确路径必须是：
   - BUS 先写 `OutboxRecord`
   - BUS 调用 GitHub MCP 或 GitLab MCP
   - MCP 返回结果
   - BUS 回写外部对象引用，例如 `pr_ref`
8. 如果 MCP 成功但 BUS 回写失败，恢复逻辑应依赖 `outbox` 对账，而不是假设“反正外面已经成功了就算完成”。

#### G. 进入 `review` step

1. `review` step 由 workflow manifest 声明。
2. 策略层从 `AgentRegistry` 选择 reviewer。
3. leader 或 BUS 唤醒 reviewer 时，也应提交显式 `AgentDispatch`，至少声明：
   - `agent_label`，例如 `security-reviewer`
   - `requested_role=reviewer`
   - `goal`，例如“基于 patch 和测试证据给出通过/不通过”
   - `ContextPack`，至少含 patch、`pr_ref`、计划摘要、测试结果
   - `expected_outputs=review_result`
   - `write_scope=read_only`
4. reviewer 读取当前 patch、PR 或相关 artifact。
5. reviewer 在执行审核时也可以使用自身 `Skill`，但最终只允许回写结构化审核结果。
6. 审核结果至少应表达：
   - 通过 / 不通过
   - 不通过的主要原因
   - 需要修改的点
   - 证据或引用
7. 若通过，task 可以达成停止条件，进入 `Succeeded`；或继续进入 manifest 声明的 `merge` step。
8. 若不通过，则按 manifest 回退到 `code` 或 `plan`。

#### H. 如果 workflow 定义了 `merge` step

1. merge 也只是 workflow 的一个普通 step。
2. BUS 不能默认把“review 通过”等价成“自动合并”。
3. 如果仓库策略要求额外审批，merge step 前还应出现新的 gate。
4. 真正的合并动作仍必须走 `outbox + MCP`。

### 人类中途改需求

如果人类在执行过程中追加一句：

- `顺手把错误提示文案也改掉`
- `这个按钮还要支持批量导出`

系统应这样处理：

1. 新输入再次写成 `ExternalEvent`。
2. BUS 按固定优先级命中当前 `task_id`；若没有显式 `task_id`，则优先使用 `reply_to_event_id`，其次才是 `repo_ref + issue_ref/pr_ref` 或其他上下文键。
3. 这条事件不能由执行实例私下处理后直接改结果；必须先进入 BUS。
4. BUS 把该事件交给当前 workflow 的回退规则处理。
5. 如果只是小范围补充，workflow 可以让 task 回到 `code` step。
6. 如果改了需求边界、验收标准或风险级别，workflow 可以要求回到 `plan` step，甚至重新审批。
7. 如果新需求和原任务差异太大，策略层可以拒绝继续漂移，拆出新 task。

### 需要重点检查的实现点

- `issue-delivery` 的绑定应来自 agent 建议 + BUS 基于 manifest 字段的校验，不应写成主程序里的 `if issue then issue-delivery`。
- BUS 负责 task、binding、gate、审计和副作用协调；不负责自己发明 plan/code/review 的具体细节。
- 新请求不应一上来就强建 task；先记录入口 request，再在 promotion 时创建 durable task。
- leader 唤醒 worker/reviewer 时，必须带结构化 `AgentDispatch` 和 `ContextPack`，不能只靠 prompt 里临时描述角色。
- 计划、patch、review、PR 引用都应是 artifact 或外部对象引用。
- 所有 GitHub/GitLab 写操作都必须走 `outbox + MCP`。
- 中途改需求应由确定性路由 + workflow 的回退规则决定回到哪一步，而不是 Go 代码写死。

## 案例 4：科研探索与评测

### 场景

人类提出一个带指标和实验要求的任务，例如：

`在指定数据集上把某个指标提升到阈值以上，允许使用 GPU 反复实验`

这个任务的特点是：
- 不是一次编码后就结束
- 通常会在实现和评测之间多轮循环
- 强依赖 GPU/CPU、数据集、实验配置和预算
- 可能最终产出 PR，也可能只产出报告

因此它通常会先得到一个命中 promote 硬规则的 `PromotionDecision`，再从入口 `EphemeralRequest` promote 成 `DurableTask`，并升级成某个不可变 revision 的 `research-exploration` workflow。

### 详细流程

#### A. 入口与建 task

1. 入口层把用户请求写成 `ExternalEvent`。
2. `ExternalEvent` 至少应带上 `conversation_id`、`thread_id`，若来自代码托管系统还应带上 `repo_ref`、`issue_ref` 或 `pr_ref`。
3. BUS 校验来源、去重、鉴权后，先按路由规则命中已有对象；若没有命中，就创建 `EphemeralRequest`。
4. `Reception` 先产出 `PromotionDecision`，至少表明 `execution.async=true`、`execution.multi_step=true`、`governance.budget_required=true` 或 `governance.recovery_required=true`；BUS 据此触发 promotion，再创建 `DurableTask`。
5. task 初始信息通常包括：
   - 来源
   - 请求摘要
   - 预算初值
   - 是否允许集群资源
   - 风险级别
   - 目标仓库或实验工程引用

#### B. 绑定 `research-exploration` workflow

1. `Reception` agent 基于 `PromotionDecision` 和自身 `Skill` 识别这是研究/实验型任务，不能直接回答。
2. agent 向 BUS 建议绑定某个具体 revision，例如展示为 `research-exploration@b4c5d6e`。
3. BUS 校验该 workflow 所需约束，例如：
   - `requires` 与 `required_refs` 中声明的实验工程和对象上下文是否存在
   - `allowed_mcp` 是否允许 Cluster MCP、代码托管 MCP
   - `max_risk` 是否覆盖当前风险等级
   - `forbids` 是否排除了当前场景
4. BUS 将绑定结果持久化为 `WorkflowBinding`，至少包含 `workflow_source`、`workflow_rev`、`manifest_digest` 和 manifest 快照。
5. 此后 task 的实验循环、是否 review、是否 report，全部由该 workflow manifest 决定。

#### C. `plan` step 产出实验计划

1. leader 读取需求和已有上下文。
2. 如果 leader 需要辅助分析数据集或基线，也应通过 `AgentDispatch` 唤醒 helper，例如：
   - `agent_label=dataset-reader`
   - `requested_role=helper`
   - `goal=总结数据集版本、标签分布和评测注意事项`
   - `ContextPack`，至少含需求摘要、数据集引用、已有实验上下文
   - `expected_outputs=dataset_notes`
   - `write_scope=read_only`
3. leader 在规划时也可以使用自身 `Skill` 做数据集理解、指标解释和实验设计。
4. `plan` step 产出计划 artifact，内容通常包括：
   - 主指标和阈值
   - 数据集版本
   - 基线版本
   - 资源预算
   - 停止条件
   - 是否需要代码交付或只要报告
5. 如果 workflow 要求 plan 审批，则 BUS 创建对应 gate。

#### D. `code` step 产出候选实现

1. leader 或 worker 对训练脚本、算法实现、参数配置或实验编排代码做修改。
2. leader 唤醒实现 worker 时，推荐显式给出：
   - `agent_label`，例如 `trainer-writer`、`sweep-editor`
   - `requested_role=worker`
   - `goal`，例如“实现新的损失函数并保留旧 baseline 开关”
   - `ContextPack`，至少含实验计划、相关代码路径、当前代码版本、已有实验结果摘要
   - `expected_outputs`，例如 `candidate_patch`、`experiment_notes`
   - `allowed_tools`、`allowed_mcp`、`sandbox_template`
   - `budget_cap`、`deadline`
   - `write_scope`
3. 如果涉及仓库写操作，仍然必须经过 `outbox + MCP`。
4. `code` step 完成后，BUS 至少应拿到：
   - patch 或 commit 引用
   - 可追踪的代码版本引用
   - 对应的 artifact

#### E. `evaluate` step 通过 MCP 发起实验

1. 进入 `evaluate` step 后，执行实例不能直接操作集群。
2. 若 leader 或 evaluator 需要拉起实验 helper，也应通过 `AgentDispatch` 明确声明：
   - `agent_label`，例如 `run-evaluator`
   - `requested_role=evaluator`
   - `goal=按指定配置发起实验并汇总指标`
   - `ContextPack`，至少含代码版本、数据集版本、评测配置、预算状态
   - `expected_outputs=evaluation_result`
   - `allowed_mcp=[cluster]`
   - `write_scope=cluster_job_submit`
3. 正确路径是：
   - BUS 先写 `OutboxRecord`
   - BUS 通过 Cluster MCP 创建实验作业
   - 外部作业返回作业 ID 或引用
   - BUS 持久化外部对象引用
4. 实验运行期间，系统需要持续记录：
   - 当前代码版本
   - 数据集版本
   - 评测配置
   - 资源用量
   - 指标结果
5. 这些结果最终以 artifact 形式回写 BUS。

#### F. 在 `code` 和 `evaluate` 之间循环

1. 如果指标未达标，workflow manifest 可以规定从 `evaluate` 回退到 `code`。
2. 如果指标已达标，则 workflow 可以进入 `review` 或 `report`。
3. 这个循环是 workflow 定义的，不应被系统核心写成“凡是代码任务都必须 evaluation”。

#### G. 预算和人工干预

1. 如果预算接近耗尽，BUS 可能触发 budget gate 或进入 `WaitingHuman`。
2. 如果人类追加预算，这同样是新的 `ExternalEvent`。
3. 这些新事件要先通过 `repo_ref/pr_ref` 或 `conversation_id + thread_id` 命中当前 task，再由当前 workflow 的回退规则决定：
   - 回到 `plan`
   - 回到 `code`
   - 重新审批
4. 如果任务被取消或预算硬熔断，BUS 必须传播取消信号，并通过 Cluster MCP 清理无用作业。

### 需要重点检查的实现点

- `evaluate` 是 workflow step，不应成为系统级固定阶段。
- 入口阶段应先是 `EphemeralRequest`，只有在 `PromotionDecision` 明确命中长流程、资源控制或恢复要求时才 promote 成 durable task。
- leader 唤醒 helper/worker/evaluator 时，必须把角色、名称、上下文快照、预算和返回契约显式写入 `AgentDispatch`。
- GPU/CPU 作业必须经由 MCP，不能让执行实例直接控制集群。
- 指标、预算、数据集版本、作业引用都应可追溯。
- 预算耗尽和方向变化应体现在确定性路由、gate、回退规则或 `WaitingHuman`，而不是靠临时分支逻辑拼接。

## 案例 5：设置定时任务

### 场景

人类发来一个系统配置类请求，例如：

`每天早上 9 点同步一次某仓库 issue 摘要`

这个任务的特点是：
- 它不是一次性回答，而是创建一个持久化计划
- 会影响系统未来行为
- 可能需要更高权限或二次确认

因此它通常会先得到一个命中 promote 硬规则的 `PromotionDecision`，再从入口 `EphemeralRequest` promote 成 `DurableTask`，并升级成某个不可变 revision 的 `schedule-management` workflow。

### 详细流程

#### A. 入口与建 task

1. 人类消息进入入口层。
2. 入口层生成 `ExternalEvent`，记录原始文本、发送人、来源渠道、时间、`conversation_id`、`thread_id`，并附上某个调度注册表类 `control_object_ref`；若是修改既有定时任务，还应带 `scheduled_task_id`。
3. BUS 做去重、鉴权和风险识别；若未命中已有对象，则先创建 `EphemeralRequest`。
4. `Reception` 先产出 `PromotionDecision`，至少表明 `effects.create_persistent_object=true` 或 `effects.external_write=true`；因此该请求不能停留在只读 request 内，BUS 创建 `DurableTask`，标记这是可能会修改调度对象的持久化任务。

#### B. 绑定 workflow

1. `Reception` agent 先利用自身 `Skill` 和 `PromotionDecision` 解析出：这是一个系统配置类请求，不应直接在对话里偷偷执行。
2. agent 向 BUS 建议绑定某个具体 revision，例如展示为 `schedule-management@c7d8e9f`。
3. BUS 校验该 workflow 是否允许用于调度对象变更，例如检查 `requires`、`required_refs`、`allowed_mcp`、`max_risk` 和 `forbids`。
4. BUS 将绑定结果持久化到 `WorkflowBinding`，至少包含 `workflow_source`、`workflow_rev`、`manifest_digest` 和 manifest 快照。

#### C. 解析并形成结构化调度请求

1. workflow 的第一个 step 先把自然语言解析成结构化 artifact，例如：
   - 频率：每天
   - 时间：09:00
   - 行为：同步 issue 摘要
   - 目标仓库：某仓库
   - 触发后执行的 workflow：某个摘要 workflow
2. 如果信息不全，例如用户没说明是哪个仓库，workflow 可以要求补充输入，此时 task 进入 `WaitingHuman`。

#### D. 风险确认

1. 如果策略认为“创建新定时任务”属于高风险系统配置，则 BUS 创建确认 gate。
2. 人类确认通过后，task 才能继续。
3. 这里的确认对象应该绑定本次结构化调度请求，而不是只绑定自然语言原文。

#### E. 写入 `ScheduledTask`

1. workflow 完成结构化请求后，BUS 准备执行外部副作用。
2. BUS 先写 `OutboxRecord`。
3. BUS 调用 Control MCP，请求创建 `ScheduledTask`。
4. Control MCP 返回调度对象引用后，BUS 持久化该 `ScheduledTask` 信息。
5. task 达成停止条件，进入 `Succeeded`。

#### F. 后续触发

1. 到达调度时间后，scheduler 不应直接“替某个 agent 想起这件事”。
2. 正确做法是：
   - scheduler 读取 `ScheduledTask`
   - 生成新的 `ScheduleTriggered` `ExternalEvent`
   - 由 BUS 按统一入口规则形成可审计的 `PromotionDecision`，并立即 promote 成新的 `DurableTask`
   - 为这个新 task 绑定对应 workflow
   - 按普通任务流程执行
3. 这样定时任务只是“任务来源的一种”，而不是藏在某个 agent 的私有记忆中。

#### G. 修改、暂停、删除定时任务

1. 这三类操作也应走同样路径：
   - 新的 `ExternalEvent`
   - 新的 `DurableTask` 或命中已有 `DurableTask`
   - workflow 解析结构化变更请求
   - 必要时确认
   - `outbox + Control MCP`
2. 对修改、暂停、删除，路由键应优先使用 `scheduled_task_id`，其次才是 `control_object_ref` 或对话上下文。
3. 主程序不应提供一个绕开 BUS 的“后台直接改定时任务表”捷径。

### 需要重点检查的实现点

- 设置定时任务不应由对话执行实例直接改本地配置文件或内存状态。
- `ScheduledTask` 必须是持久化对象，并有独立审计。
- 定时触发后生成的是新的 `DurableTask`，而不是隐式调用某个 agent。
- 修改、暂停、删除应优先通过 `scheduled_task_id` 或 `control_object_ref` 路由到控制面 task。
- 修改、暂停、删除也必须保留同样的事件、审批和副作用路径。

## 案例 6：使用自然语言修改 workflow 定义

### 场景

人类直接发来一句系统设计/配置请求，例如：

`把 issue-delivery 的 plan 审批去掉，改成 merge 前必须人工审批`

或者：

`给 research-exploration 增加一个 report step，在 evaluate 达标后先产出报告再决定要不要 merge`

这个任务的特点是：
- 它修改的不是普通业务对象，而是 workflow 定义本身
- 会影响后续大量新任务的行为
- 需要把自然语言变成结构化的 workflow 变更请求
- 通常需要 schema 校验、策略校验、diff 审阅和发布审批

因此它不应由 `Reception` agent 直接在后台改文件，而应先得到一个命中 promote 硬规则的 `PromotionDecision`，再从入口 `EphemeralRequest` promote 成 `DurableTask`，并绑定专门的 `workflow-management` workflow。

### 详细流程

#### A. 入口与建 task

1. 人类消息先写成 `ExternalEvent`。
2. 该事件至少记录原始文本、发送人、`conversation_id`、`thread_id`，以及它引用的 workflow 对象，例如 `workflow_object_ref=issue-delivery` 或 `workflow_source=...`。
3. BUS 做去重、鉴权和风险识别；若未命中已有对象，则先创建 `EphemeralRequest`。
4. `Reception` 先产出 `PromotionDecision`，至少表明 `effects.external_write=true` 或 `effects.create_persistent_object=true`，并且目标是高风险控制面对象；因此 BUS 创建 `DurableTask`。

#### B. 绑定 `workflow-management` workflow

1. `Reception` agent 基于 `PromotionDecision` 向 BUS 建议绑定 `workflow-management` 的某个不可变 revision。
2. BUS 校验该 workflow 是否允许用于 workflow 定义变更，例如：
   - `required_refs` 是否要求给出 `workflow_id`、`workflow_source` 或 workflow 仓库引用
   - `allowed_mcp` 是否允许代码托管 MCP 或 workflow registry MCP
   - `max_risk` 是否覆盖这类系统配置变更
   - `forbids` 是否禁止未授权用户修改生产 workflow
3. BUS 将绑定结果写入 `WorkflowBinding`，并冻结本次管理流程所依据的 manifest 快照。

#### C. 把自然语言变成结构化变更请求

1. workflow 的第一个 step 不应直接改文件，而应先产出结构化 `workflow_change_request` artifact。
2. 这个 artifact 至少应表达：
   - 目标 `workflow_id`
   - 目标 `workflow_source`
   - 要修改的是 manifest、prompt/template，还是二者都改
   - 变更前规则
   - 期望变更后规则
   - 不允许被改动的边界
   - 风险说明和影响范围
3. 如果自然语言不够精确，例如用户没说清楚“去掉的是 plan 审批还是 review 审批”，workflow 应进入 `WaitingHuman` 补充信息，而不是擅自猜。

#### D. leader 如何唤醒修改 helper

1. leader 若需要 helper 来分析现有 workflow 文件、生成 diff 或解释影响，也应通过 `AgentDispatch` 显式唤醒。
2. 例如可以唤醒：
   - `agent_label=workflow-reader`
   - `requested_role=helper`
   - `goal=读取现有 issue-delivery manifest 并定位 approval gate`
   - `ContextPack`，至少含 `workflow_change_request`、当前 workflow 文件引用、已有约束
   - `expected_outputs=workflow_analysis`
   - `write_scope=read_only`
3. 若需要真正产出候选修改，也可以再唤醒：
   - `agent_label=workflow-editor`
   - `requested_role=worker`
   - `goal=基于变更请求生成候选 manifest diff`
   - `ContextPack`，至少含当前 workflow 内容、结构化变更请求、schema 约束
   - `expected_outputs=candidate_workflow_patch`
   - `write_scope`，只允许修改 workflow 仓库中的目标文件路径

#### E. 生成 diff 并做机器校验

1. leader 汇总 helper/worker 结果后，产出候选 workflow diff 或 PR。
2. 在任何发布前，BUS 至少应要求：
   - workflow schema 校验通过
   - manifest 约束字段仍可机器判定
   - 不突破既有权限边界
   - 引用的 step、gate、artifact schema 保持一致或有兼容迁移说明
3. 这些校验结果应作为 artifact 挂回 BUS，而不是只存在临时日志里。

#### F. 审批与发布

1. workflow 变更通常是高风险配置，因此发布前应出现确认或审批 gate。
2. 审批对象不应是原始自然语言，而应是：
   - 结构化变更请求
   - 候选 diff/PR
   - 校验结果摘要
   - 影响范围摘要
3. 若审批通过，BUS 再通过代码托管 MCP 或 registry MCP 执行发布动作。
4. 成功后，BUS 应持久化新的：
   - `workflow_source`
   - `workflow_rev`
   - `manifest_digest`
   - 发布时间和审批记录

#### G. 对现有 task 的影响

1. 已经在跑的 `DurableTask` 不应因为 workflow 发布了新 revision 就自动漂移。
2. 它们继续使用各自 `WorkflowBinding` 中冻结的旧 revision。
3. 只有后续新建 task，或在允许重绑定的审批点显式重规划，才会使用新 revision。

### 需要重点检查的实现点

- 系统应支持用自然语言提出 workflow 变更请求，但不能直接把用户文本当成“线上立刻生效的 workflow 编辑脚本”。
- 这类请求必须走受控的 `workflow-management` workflow，而不是后台直接改配置文件。
- 自然语言必须先变成结构化 `workflow_change_request`，再生成 diff、做校验、走审批。
- workflow 发布后，旧 task 仍绑定旧 revision；不能把已运行任务悄悄切到新 workflow。
- 对 workflow 控制面请求，事件路由应优先使用 `workflow_object_ref`，而不是退化成普通对话线程匹配。
- leader 唤醒 `workflow-reader`、`workflow-editor` 等子 agent 时，也必须显式给出 `AgentDispatch` 和 `ContextPack`。
