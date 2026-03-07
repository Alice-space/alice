# 02. 控制平面与工作流设计

## 1. 控制平面职责

控制平面是系统唯一的协调中枢，负责：

- 接收来自 CLI、飞书等通道的自然语言请求，以及 webhook、scheduler 等系统事件。
- 使用基于模型的 `IntentCompiler` 将人类输入解释成 `IntentSpec`。
- 调用 `TaskClassifier` 进行入口分诊。
- 创建 `TaskSpec`、`RunRecord`、`workflow_id` 和必要的 `ScheduleEntry` / `ConfigChangeProposal`。
- 推进运行状态与工作流阶段。
- 调度 runtime、执行器和集成适配器。
- 统一写日志、记预算、发通知、写事件、做恢复对账。

控制平面不负责：

- 自己实现模型调用协议。
- 自己实现 Git / SSH / SLURM / 邮件细节。
- 自己保存原始知识库长文本。

## 2. 统一处理流水线

### 2.1 请求进入

```text
Natural Language / Webhook / Scheduler
        -> IntentCompiler or EventAdapter
        -> IntentSpec / Event
        -> TaskClassifier / SettingsValidator / SignalResolver
        -> TaskSpec | RuntimeSetting | ConfigChangeProposal | ApprovalTicket update
        -> RunRecord + Event Log + Side Effects
```

### 2.2 意图分流

| `intent_kind` | 进入组件 | 主输出 |
| --- | --- | --- |
| `task_request` | `TaskClassifier` | `TaskSpec` + `RunRecord` |
| `setting_change` | `SettingsValidator` | `RuntimeSetting` 或 `ConfigChangeProposal` |
| `state_query` | `QueryHandler` | 只读响应 |
| `approval_response` | `ApprovalResolver` | 审批结果事件 |
| `manual_signal` | `SignalResolver` | 附着现有工作流的人工信号 |

规则：

- 自然语言入口的 `intent_kind` 必须以模型输出为准，不允许用关键词匹配、前缀匹配或正则规则直接做分流。
- 所有可执行请求都必须落成 `TaskSpec`，即使是 `fast path`。
- `setting_change` 不直接执行外部副作用，必须先验证可变边界。
- `manual_signal` 不新建工作流，而是向目标运行注入结构化信号。

## 3. 运行真相模型

系统中有三层“真相”，必须严格区分：

1. `Event`
   所有进入、裁决、副作用结果和人工信号的时间序列。
2. `RunRecord.run_status`
   这次运行现在是否在跑、是否阻塞、是否结束。
3. `RunRecord.workflow_phase`
   长任务当前所处阶段，例如 `Planning`、`Evaluating`。

规则：

- `run_status` 和 `workflow_phase` 不得复用同一个字段。
- `fast path` 只有 `run_status`，默认没有 `workflow_phase`。
- 状态推进顺序是：先写事件，再以 CAS 更新 `RunRecord`。

## 4. fast path 与 task path

### 4.1 fast path

- 适用于简单问答、低风险查询、轻量观察类动作。
- 创建 `TaskSpec` 和轻量 `RunRecord`，但默认不创建完整 `workflow_id`。
- 默认只能使用 `FastSingleAgentRuntime`。
- 仍需产生日志、预算和审计记录。
- 处理中若发现超出阈值，必须生成 `TaskEscalated` 并切入 `task path`。

### 4.2 task path

- 创建完整 `workflow_id`。
- 初始化 `run_status = Queued`、`workflow_phase = Planning`。
- 所有阶段切换都产生 `WorkflowPhaseChanged` 事件。
- 任一外部动作失败时，根据异常等级进入重试、降级、阻塞、人工等待或终止。

### 4.3 evaluation path

- `evaluation` 是特殊 `task path`，其 `run_mode = evaluation`。
- 可以附着到代码变更工作流，也可以作为独立评测工作流运行。
- 必须绑定 `commit_sha`、基线引用和聚合规则。

## 5. 入口类型

| 类型 | 来源 | 是否直接来自人类自然语言 | 是否一定建工作流 |
| --- | --- | --- | --- |
| `user_cli_nl` | 本地 CLI 中的自然语言 | 是 | 否 |
| `user_feishu_nl` | 飞书中的自然语言 | 是 | 否 |
| `scheduler` | 定时器 | 否 | 视任务而定 |
| `git_webhook` | PR 打开、更新、标签变更 | 否 | 是 |
| `integration_webhook` | 邮件、系统告警、外部回调 | 否 | 视任务而定 |
| `manual_signal_nl` | 人工自然语言介入 | 是 | 不新建，附着现有工作流 |

## 6. 工作流状态机

### 6.1 WorkflowPhase 转移表

| 当前阶段 | 触发 | 下一阶段 |
| --- | --- | --- |
| `Planning` | 计划完成且需要代码修改 | `Implementing` |
| `Planning` | 计划完成且直接实验 | `Experimenting` |
| `Planning` | 需要人工确认方向 | `WaitingHuman` |
| `Implementing` | 代码提交后需要评测 | `Evaluating` |
| `Experimenting` | 实验完成 | `Evaluating` |
| `Evaluating` | 达标 | `Reporting` |
| `Evaluating` | 不达标但可重试 | `Implementing` 或 `Planning` |
| `Evaluating` | 不达标且需人工介入 | `WaitingHuman` |
| 任意非终态 | 环境故障可恢复 | `Blocked` |
| `Blocked` | 恢复成功 | 原阶段或 `Planning` |
| `WaitingHuman` | 收到人工信号 | 指定阶段 |
| `Reporting` | 报告完成 | `Done` |
| 任意非终态 | 明确终止 | `Aborted` |

### 6.2 RunStatus 转移要求

- `Queued -> Running`
- `Running -> WaitingHuman | Blocked | Succeeded | Failed | Aborted | Superseded`
- `WaitingHuman -> Running | Aborted`
- `Blocked -> Running | Failed | Aborted`

规则：

- `Superseded` 只用于“新输入替代旧运行”，例如 PR 新 commit 到达。
- 工作流进入 `Done` 时，对应 `run_status` 必须是 `Succeeded`。
- 工作流进入 `Aborted` 时，对应 `run_status` 必须是 `Aborted`。

## 7. 设置验证与配置提案流

用户可以用自然语言调整系统设置，但设置修改必须分层处理：

1. 可变运行时设置
   可直接生效，例如定时任务、通知频率、预算软阈值、观察目标列表。
2. 守护型设置
   可由自然语言提出，但需要确认或审批，例如默认模型偏好、评测重试轮数、重要告警阈值。
3. 静态基线配置
   不允许直接由自然语言生效，例如密钥、根目录、MCP 地址、硬安全限制。系统只能生成 `ConfigChangeProposal`。

处理流程：

1. `IntentCompiler` 生成 `setting_change`。
2. `SettingsValidator` 判断设置层级、风险和作用域。
3. 如需确认，回显结构化解释给用户。
4. 可直接生效的请求写入 `RuntimeSetting`，并生成 `SettingApplied`。
5. 不可直接生效的请求写入 `ConfigChangeProposal`，并生成 `ConfigChangeProposed`。
6. 所有副作用都必须带审计事件与来源 `intent_id`。

## 8. 调度器

`Scheduler` 是控制平面内建组件，不是外部 cron 包装。

### 8.1 调度对象

- `ScheduleEntry`
- 延迟重试任务
- 健康检查任务
- 周期摘要任务

### 8.2 调度要求

- 每条 `ScheduleEntry` 必须有稳定 `schedule_id` 和 `task_template_id`。
- 每次触发必须生成新的 `run_id`。
- 必须记录上次成功时间、上次失败时间和去重游标。
- 若主控重启，调度器必须基于持久化元数据恢复。
- 同一计划时间只能产生一个有效任务实例。

## 9. 审批与人工信号

人工信号统一建模为命令，不允许直接改数据库。对人类而言，人工信号也必须通过自然语言产生，再被解释成结构化信号。

### 支持的信号

- `pause_project`
- `resume_project`
- `change_priority`
- `change_direction`
- `force_report`
- `abort_run`
- `approve_action`
- `reject_action`

### 处理规则

- 若目标工作流不存在，信号直接拒绝。
- 若工作流已终态，信号只入审计，不再影响执行。
- 高风险信号必须带操作者身份。
- 审批票与人工信号不同：审批只回答一个待决动作，人工信号可以改变整条工作流方向。

## 10. 副作用持久化与恢复

### 10.1 外部动作原则

对会产生外部副作用且无法安全重复的动作，不能采用“先做再记”的流程。控制平面必须使用持久化 action intent 或 outbox 语义：

1. 生成待执行动作和幂等键。
2. 将动作意图、目标对象、租约 token 和预期副作用先落库。
3. 执行外部动作。
4. 写结果事件并推进 `RunRecord`。
5. 若主控在第 3 步后崩溃，恢复流程必须先对账再决定是否补写或重试。

适用动作：

- 创建分支、PR、check。
- 提交远端作业。
- 发送通知与审批卡片。
- 标记邮件游标、写外部工单。

### 10.2 幂等性

- webhook 事件必须带 `idempotency_key`。
- 调度触发必须带 `schedule_id + scheduled_time`。
- 人工信号必须带 `workflow_id + signal_seq`。
- 相同幂等键重复到达时，只允许复用先前处理结果，不允许重复副作用。

### 10.3 启动恢复

主控重启后必须执行 reconciliation：

1. 扫描所有非终态 `RunRecord`。
2. 恢复未完成锁与租约状态。
3. 查询外部真实状态，例如 PR、远端作业、未确认通知。
4. 对未完成 action intent 做补写、重试、标记 `Blocked` 或转人工。
5. 再恢复调度器。

## 11. 快速任务升级机制

`fast path` 在以下条件下必须升级：

- 需要多次写入型动作。
- 需要开分支或 PR。
- 需要跨多台机器执行。
- 需要持续运行超过短任务阈值。
- 需要人工审批。
- 需要高成本模型多轮推理。

升级动作：

1. 当前轻量运行写入中止摘要。
2. 生成 `TaskEscalated`。
3. 创建新的 `TaskSpec` 与 `RunRecord`。
4. 切入完整 `task path`。
