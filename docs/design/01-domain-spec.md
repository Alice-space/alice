# 01. 统一领域模型与 Spec

## 1. 统一对象

本文件定义整个系统的标准对象。任何实现和子文档不得私自重命名核心对象；若后续文档新增对象，必须先回写到本文件。

## 2. 标识符约定

- `project_id`：全局唯一，稳定标识一个科研项目或全局自动化项目。
- `repository_id`：全局唯一，稳定标识一个仓库。
- `task_id`：全局唯一，标识一个任务实例。
- `run_id`：全局唯一，标识一次运行实例。
- `workflow_id`：全局唯一，标识一个长期工作流实例。
- `intent_id`：全局唯一，标识一次自然语言意图解释结果。
- `setting_id`：全局唯一，标识一条可变运行时设置。
- `proposal_id`：全局唯一，标识一条配置变更提案。
- `schedule_id`：全局唯一，标识一条周期调度配置。
- `event_id`：全局唯一，标识一个事件。
- `memory_id`：全局唯一，标识一条记忆。
- `eval_id`：全局唯一，标识一次评测对象。
- `baseline_id`：全局唯一，标识一个评测基线快照。
- `lock_id`：全局唯一，标识一把锁或租约。
- `executor_id`：全局唯一，标识一个执行目标。
- `integration_id`：全局唯一，标识一个外部系统或 MCP 连接点。

## 3. ProjectSpec

`ProjectSpec` 是项目主配置，是大多数治理策略的根对象。

### 必填字段

| 字段 | 含义 |
| --- | --- |
| `project_id` | 项目唯一 ID |
| `name` | 项目名称 |
| `goal` | 项目长期目标 |
| `repositories` | `RepositorySpec` 列表 |
| `default_branch` | 默认主分支 |
| `execution_targets` | 可用执行环境引用列表 |
| `budget_policy` | token、GPU、时间、重试预算 |
| `notification_policy` | 飞书/其他通知策略 |
| `approval_policy` | 哪些动作必须人工批准 |
| `artifact_policy` | 产物落盘、归档、保留策略 |
| `workspace_policy` | 可写目录、工作树隔离、清理规则 |
| `memory_policy` | 记忆读写、晋升、衰减策略 |
| `evaluation_policy` | PR 评测、基线选择、阈值比较规则 |
| `mutable_settings_policy` | 哪些设置允许通过自然语言修改 |

### 语义约束

- 一个项目可以有多个仓库，但一个写入型运行在同一时间只能拥有一个主写入仓库上下文。
- `workspace_policy` 必须显式声明允许写入的根目录，以及是否强制使用隔离 worktree 或 run workspace。
- `evaluation_policy` 决定代码优化任务是否必须经过 PR 评测门禁。
- `memory_policy` 决定项目记忆的容量、过期时间和可读作用域。

## 4. RepositorySpec

`RepositorySpec` 定义项目中的仓库及其工作区治理边界。

| 字段 | 含义 |
| --- | --- |
| `repository_id` | 仓库唯一 ID |
| `name` | 仓库名 |
| `root` | 本地或挂载后的仓库根路径 |
| `remote_url` | 远端地址 |
| `default_branch` | 仓库默认分支 |
| `workspace_strategy` | `shared` / `worktree` / `ephemeral_copy` |
| `protected_branches` | 受保护分支列表 |

规则：

- 写入型运行不得直接在受保护分支上提交，除非项目策略显式允许。
- `workspace_strategy` 必须与 `workspace_policy` 一致，不允许文档层面默认“随便在仓库里改”。

## 5. TaskSpec

`TaskSpec` 表示一项调度单位。它既可能来自人工输入，也可能来自 webhook、定时器或系统内部回写。

### 5.1 TaskTemplate

`TaskTemplate` 表示可被调度器、webhook 或恢复流程重复实例化的任务蓝图。

最小字段：

- `task_template_id`
- `project_id`
- `task_type`
- `goal_template`
- `default_write_scope`
- `default_acceptance_policy`
- `default_target_executor`

规则：

- `TaskTemplate` 不是运行实例，不持有 `run_status`。
- `Scheduler` 只能引用稳定的 `task_template_id`，不能直接重用某次历史 `run_id`。

### 必填字段

| 字段 | 含义 |
| --- | --- |
| `task_id` | 任务唯一 ID |
| `project_id` | 所属项目 |
| `task_type` | `query` / `code_change` / `experiment` / `evaluation` / `scheduled_watch` / `review` / `report` / `maintenance` |
| `priority` | 优先级 |
| `goal` | 本次任务目标 |
| `dependencies` | 前置依赖 |
| `expected_outputs` | 预期产物 |
| `allowed_models` | 可用模型或能力类别 |
| `resource_limits` | token、时间、GPU、远端资源上限 |
| `target_executor` | 默认执行目标 |
| `write_scope` | `none` / `repo_branch` / `project` / `settings` |
| `retry_policy` | 自动重试策略 |
| `acceptance_policy` | 完成/失败判定标准 |
| `memory_hints` | 需要读取的记忆范围与关键词 |
| `trigger` | `user` / `scheduler` / `webhook` / `eval_retry` / `manual_signal` / `recovery` |

### 语义约束

- 所有可执行请求都必须落成 `TaskSpec`，`fast path` 只是不创建完整长期工作流，不是跳过任务对象。
- `query` 类型允许走 `fast path`。
- `code_change` 类型默认进入 `task path`，除非只是只读 review。
- `scheduled_watch` 类型由 `Scheduler` 产生，必须携带去重游标或时间窗口。
- `evaluation` 类型是 `EvalTask` 的统一任务外壳，必须绑定 `commit_sha` 与基线选择结果。
- `eval_retry` 类型必须关联前一次 `RunRecord` 和失败摘要。

## 6. RunRecord

`RunRecord` 是运行事实，不是配置。它必须足够完整，允许恢复、审计与重放。

### 必填字段

| 字段 | 含义 |
| --- | --- |
| `run_id` | 运行实例 ID |
| `task_id` | 对应任务 |
| `workflow_id` | 所属工作流，可为空 |
| `parent_run_id` | 父运行，可为空 |
| `run_mode` | `fast` / `workflow` / `evaluation` / `maintenance` |
| `run_status` | 运行状态 |
| `workflow_phase` | 工作流阶段，可为空 |
| `state_version` | 状态版本号，用于 CAS 更新 |
| `timeline` | 关键时间线 |
| `cost_summary` | token、执行时长、资源消耗 |
| `log_index` | 日志索引 |
| `artifact_refs` | 产物引用列表 |
| `failure_info` | 失败分类、摘要、可恢复性 |
| `intervention_history` | 人工信号记录 |
| `result_summary` | 结果摘要 |
| `routing_decision` | `fast path` / `task path` / 升级信息 |
| `classifier_summary` | 入口分诊结论 |
| `superseded_by` | 新运行 ID，可为空 |

### 语义约束

- `fast` 模式允许 `workflow_id` 和 `workflow_phase` 为空。
- `workflow` 和 `evaluation` 模式必须有可恢复状态与终止条件。
- `run_status` 表示“这个运行活没活着”，`workflow_phase` 表示“这轮工作进行到哪一阶段”，两者不得混用。
- `failure_info` 不可只记录“失败”，必须记录错误分类和可操作解释。

## 7. RunStatus 与 WorkflowPhase

### 7.1 RunStatus

标准值：

- `Queued`
- `Running`
- `WaitingHuman`
- `Blocked`
- `Succeeded`
- `Failed`
- `Aborted`
- `Superseded`

规则：

- `Succeeded`、`Failed`、`Aborted`、`Superseded` 是终态。
- `Superseded` 不是失败，表示该运行被更新的输入或 commit 替换。
- 只有 `Blocked` 和 `WaitingHuman` 可以在恢复或人工信号后重新进入 `Running`。

### 7.2 WorkflowPhase

标准值：

- `Planning`
- `Implementing`
- `Experimenting`
- `Evaluating`
- `Reporting`
- `WaitingHuman`
- `Blocked`
- `Done`
- `Aborted`

规则：

- `Done` 和 `Aborted` 是工作流终态。
- `WaitingHuman` 和 `Blocked` 既可作为阶段，也必须同步反映到 `run_status`。
- `fast path` 默认不使用 `WorkflowPhase`。

## 8. Event

### 标准字段

| 字段 | 含义 |
| --- | --- |
| `event_id` | 事件 ID |
| `event_type` | 事件类型 |
| `source` | 来源子系统 |
| `target` | 目标子系统或对象 |
| `project_id` | 所属项目 |
| `task_id` | 关联任务 |
| `run_id` | 关联运行，可为空 |
| `severity` | 等级 |
| `payload` | 负载 |
| `attempt` | 当前尝试次数 |
| `created_at` | 产生时间 |
| `idempotency_key` | 幂等键 |

### 标准事件类型

- `TaskReceived`
- `TaskClassified`
- `TaskEscalated`
- `RunStarted`
- `RunStatusChanged`
- `WorkflowPhaseChanged`
- `RunSuperseded`
- `EvalRequested`
- `EvalCompleted`
- `EvalFailed`
- `ApprovalRequested`
- `ApprovalResolved`
- `SettingChangeRequested`
- `SettingApplied`
- `ConfigChangeProposed`
- `ScheduleTriggered`
- `MemoryPromoted`
- `HumanSignalReceived`
- `BudgetExceeded`
- `IntegrationFailed`
- `RunAborted`

## 9. ScheduleEntry

`ScheduleEntry` 定义一条持久化调度规则。

| 字段 | 含义 |
| --- | --- |
| `schedule_id` | 调度唯一 ID |
| `project_id` | 所属项目，可为空 |
| `task_template_id` | 触发时生成的任务模板引用 |
| `trigger_spec` | cron、interval 或窗口定义 |
| `dedupe_cursor` | 去重游标或上次处理窗口 |
| `timezone` | 调度时区 |
| `status` | `active` / `paused` / `archived` |
| `last_success_at` | 上次成功时间 |
| `last_failure_at` | 上次失败时间 |

规则：

- `ScheduleEntry` 的变更必须能回溯到 `RuntimeSetting`。
- 同一条调度规则在同一计划时间只能生成一个有效任务实例。

## 10. IntentSpec

`IntentSpec` 是自然语言解释层的标准输出。

| 字段 | 含义 |
| --- | --- |
| `intent_id` | 意图 ID |
| `raw_text` | 原始自然语言 |
| `actor_id` | 发起人 |
| `channel` | `cli` / `feishu` / 其他聊天通道 |
| `intent_kind` | `task_request` / `setting_change` / `state_query` / `approval_response` / `manual_signal` |
| `scope` | `global` / `project` / `run` |
| `parsed_payload` | 结构化意图载荷 |
| `confidence` | 解释置信度 |
| `risk_level` | 风险等级 |
| `requires_confirmation` | 是否必须回显确认 |

规则：

- 高风险、低置信度或涉及静态基线配置的意图必须确认。
- `setting_change` 不直接产生副作用，必须先通过设置验证。
- `manual_signal` 与 `approval_response` 不同；前者改变工作流方向，后者只回答一张待处理审批票。

## 11. RuntimeSetting、ConfigChangeProposal 与 ApprovalTicket

### 11.1 RuntimeSetting

`RuntimeSetting` 表示可由自然语言调整的运行时设置。

| 字段 | 含义 |
| --- | --- |
| `setting_id` | 设置 ID |
| `project_id` | 所属项目，可为空 |
| `setting_key` | 设置键 |
| `desired_value` | 目标值 |
| `source_intent_id` | 来源意图 |
| `mutable_class` | `safe` / `guarded` / `immutable` |
| `version` | 设置版本 |
| `status` | `draft` / `active` / `rejected` / `superseded` |

### 11.2 ConfigChangeProposal

`ConfigChangeProposal` 表示用户通过自然语言提出、但不能直接生效的静态配置变更请求。

| 字段 | 含义 |
| --- | --- |
| `proposal_id` | 提案 ID |
| `project_id` | 所属项目，可为空 |
| `proposal_kind` | `static_config_patch` / `secret_rotation_request` / `integration_rebind` |
| `source_intent_id` | 来源意图 |
| `target_ref` | 目标配置、文件或集成 |
| `proposed_change` | 结构化变更内容 |
| `risk_summary` | 风险摘要 |
| `status` | `draft` / `waiting_human` / `approved` / `rejected` / `applied` |

规则：

- 自然语言不得直接把 `immutable` 设置写成 `active`，只能生成 `ConfigChangeProposal`。
- `ConfigChangeProposal` 的落地动作必须保留审计事件与执行结果。

### 11.3 ApprovalTicket

`ApprovalTicket` 表示一条等待人工决策的高风险动作。

| 字段 | 含义 |
| --- | --- |
| `approval_id` | 审批 ID |
| `project_id` | 所属项目，可为空 |
| `run_id` | 关联运行 |
| `action_kind` | 待审批动作类型 |
| `requested_effect` | 请求执行的副作用摘要 |
| `risk_summary` | 风险摘要 |
| `status` | `pending` / `approved` / `rejected` / `expired` |
| `expires_at` | 过期时间 |

规则：

- `approval_response` 只能解析并推进既有 `ApprovalTicket`，不能凭空制造未注册动作。
- 审批票过期后，如无策略允许自动重提，则运行应转 `WaitingHuman` 或 `Aborted`。

## 12. MemoryRecord

### 标准字段

| 字段 | 含义 |
| --- | --- |
| `memory_id` | 记忆 ID |
| `scope` | `global` / `project` / `task` / `run` |
| `project_id` | 关联项目，可为空 |
| `repository_id` | 关联仓库，可为空 |
| `memory_type` | 记忆类型 |
| `summary` | 短摘要 |
| `content_ref` | 长内容引用 |
| `source` | 来源，例如用户输入、评测结果、人工决策 |
| `confidence` | 置信度 |
| `freshness` | 新鲜度分级 |
| `last_verified_at` | 上次验证时间 |
| `tags` | 检索标签 |

### 标准类型

- `preference`
- `fact`
- `decision`
- `failure_pattern`
- `playbook`
- `evaluation_baseline`

规则：

- `run` 作用域用于持久化工作记忆或恢复摘要，不自动晋升为长期记忆。
- 长期记忆必须带来源链路，且不能替代数据库、Git 和正式产物。

## 13. AcceptancePolicy

`AcceptancePolicy` 定义任务达标标准。

### 典型字段

- `metric_targets`
- `tolerance`
- `regression_guards`
- `max_retry_rounds`
- `manual_gate_required`
- `comparison_baseline`
- `min_sample_size`
- `determinism_policy`

### 典型用途

- 判断 PR 评测是否通过。
- 判断长期科研探索是否值得继续一轮。
- 判断定时观察任务是否需要升级告警等级。

## 14. EvalTask、BaselineSnapshot 与 EvalReport

### 14.1 EvalTask

`EvalTask` 是 `task_type = evaluation` 的特化任务，至少补充以下字段：

- `eval_id`
- `pr_id`
- `commit_sha`
- `baseline_ref`
- `dataset_ref`
- `environment_ref`
- `benchmark_suite_ref`
- `seed_policy`
- `aggregation_rule`

### 14.2 BaselineSnapshot

`BaselineSnapshot` 定义可比较的评测参考点，至少包含：

- `baseline_id`
- `project_id`
- `source_kind`
- `code_ref`
- `dataset_ref`
- `benchmark_suite_ref`
- `environment_ref`
- `metric_schema`
- `seed_policy`
- `sample_size`
- `metric_snapshot`
- `verified_at`

### 14.3 EvalReport

`EvalReport` 是一次评测的结构化输出，至少包含：

- `eval_id`
- `run_id`
- `comparable`
- `metric_deltas`
- `risk_findings`
- `artifact_refs`
- `recommended_action`
- `confidence`

规则：

- `EvalReport` 必须显式声明“能否与基线可比较”，不可默认假设可比。
- 无可靠基线时可以报告绝对指标，但不能自动宣布优化成功。

## 15. ExecutorCapability 与 LockRecord

### 15.1 ExecutorCapability

执行目标必须声明能力：

- `supports_shell`
- `supports_gpu`
- `supports_slurm`
- `supports_network`
- `workspace_root`
- `concurrency_limit`
- `max_runtime`
- `allowed_projects`

### 15.2 LockRecord

系统锁或租约至少包含：

- `lock_id`
- `lock_type`
- `lock_key`
- `owner_run_id`
- `lease_token`
- `created_at`
- `expires_at`
- `status`

规则：

- 所有长任务锁都必须基于租约续期。
- `lease_token` 用于防止僵尸运行在主控恢复后继续写入。

## 16. 全局不变量

- 任意用户操作都必须先形成 `IntentSpec`，再进入任务流、设置流或审批流。
- 任意执行型请求都必须生成 `TaskSpec`，任意任务运行都必须生成 `RunRecord`。
- 同一 `project_id + repository_id + branch` 在同一时刻最多允许一个写入型运行持有写锁。
- 同一 `run_id` 的终态写入必须幂等。
- 相同 `idempotency_key` 的外部触发不得产生重复副作用。
- 任意定时任务都必须具备去重条件。
- 任意自动回写到记忆系统的记录都必须带置信度和来源。
- 任意评测结论都必须绑定代码、数据与环境引用，不允许只凭自由文本结论推进门禁。
