# TDR 01: 领域模型与状态机

## 1. 目标

本文定义第一版的领域对象、命令、事件、状态与不变式。实现代码必须直接复用本文命名，避免 `draft` 的 CamelCase、`cdr` 的 snake_case 和代码枚举三套并存。

建议目录：

- `internal/domain/task.go`
- `internal/domain/event.go`
- `internal/domain/command.go`
- `internal/domain/types.go`
- `internal/domain/validate.go`

## 2. 核心枚举

```go
type TaskType string
type TaskStatus string
type RouteType string
type DeliveryMode string
type WaitingReason string
type RiskLevel string
type AuditTargetType string
type AuditVerdictStatus string
type OutboxActionType string
type MCPDomain string
```

推荐枚举值：

- `TaskType`: `query`, `control`, `async_code`, `scheduled_task`
- `RouteType`: `sync_direct`, `sync_mcp_read`, `control_write`, `async_issue`, `wait_human`
- `DeliveryMode`: `merge`, `report_only`
- `TaskStatus`: `task_created`, `waiting_human`, `sync_running`, `issue_sync_pending`, `issue_created`, `planning`, `plan_audit`, `coding`, `evaluation`, `budget_exceeded`, `code_audit`, `pending_arbitration`, `merge_approval_pending`, `merging`, `merged`, `closed`, `failed`, `cancelled`
- `WaitingReason`: `waiting_input`, `waiting_confirmation`, `waiting_budget`, `waiting_recovery`
- `RiskLevel`: `low`, `medium`, `high`, `critical`
- `AuditTargetType`: `plan`, `code`
- `AuditVerdictStatus`: `accepted`, `approve`, `reject`, `abstain`, `missing`
- `MCPDomain`: `control`, `github`, `gitlab`, `cluster`

## 3. Task 聚合根

`Task` 是系统聚合根。所有状态推进都必须以 `Task` 为中心。

```go
type Task struct {
    TaskID             string
    TraceID            string
    Type               TaskType
    Status             TaskStatus
    WaitingReason      WaitingReason
    RiskLevel          RiskLevel
    TaskVersion        uint64
    GlobalHLC          string
    Source             TaskSource
    DeliveryMode       DeliveryMode
    Identity           IdentityContext
    Budget             BudgetState
    ActiveIssueRef     *IssueRef
    ActivePRRef        *PRRef
    ActivePlan         *PlanRef
    ActiveEvalSpec     *EvalSpecRef
    ActiveCancelToken  *CancellationTokenRef
    PlanAuditRound     uint32
    CodeAuditRound     uint32
    LastError          *TaskError
    CreatedAt          time.Time
    UpdatedAt          time.Time
    ClosedAt           *time.Time
}
```

约束：

- `TaskVersion` 每次成功状态推进后加 1
- `WaitingReason` 仅在 `waiting_human` 状态有值
- `Task.Type` 是稳定业务类型，不随入口瞬时路由变化
- `ActiveIssueRef` 在 `issue_created` 及之后阶段必须非空
- `ActivePRRef` 在 `coding` 之后通常非空；报告型任务可为空
- `ActivePlan` 在 `plan_audit`、`coding` 及之后阶段必须非空
- `DeliveryMode=report_only` 的任务才允许从 `code_audit` 直接进入 `closed`

## 4. 附属对象

### 4.1 输入与身份

```go
type UserRequest struct {
    RequestID       string
    SourceChannel   string
    ThreadID        string
    AliceUserID     string
    ExternalActorID string
    RawText         string
    Attachments     []AttachmentRef
    CoalescingKey   string
    ReceivedAt      time.Time
}

type IngressTraceStage string

type IngressTrace struct {
    TraceID         string
    RequestID       string
    Stage           IngressTraceStage
    RouteDecision   *RouteDecision
    ReasonSummary   string
    OccurredAt      time.Time
}

type TaskSource struct {
    SourceType      string
    SourceID        string
    ExternalEventID string
    ScheduleID      string
    ScheduleFireID  string
    IdempotencyKey  string
}

type IdentityContext struct {
    AliceUserID  string
    Roles        []string
    MFAValidated bool
    Bindings     []IdentityBinding
}
```

推荐阶段值：

- `IngressTraceStage`: `received`, `classified`

说明：

- `received` 与 `classified` 只属于入口审计轨迹，不属于 `TaskStatus`
- `CreateTask` 之前系统可能已经有 `IngressTrace`，但此时还没有正式 `Task` 聚合
- `RouteDecision` 是入口瞬时执行决策，可持久化到 `IngressTrace` 或事件审计，但不是 `Task` 真值字段
- `IngressTrace` 应按 `trace_id` append-only 存储，供入口恢复、排障和安全审计使用

### 4.2 计划、审核与评测

```go
type PlanRef struct {
    PlanArtifactID string
    PlanVersion    uint32
}

type EvalSpecRef struct {
    EvalSpecID string
    Version    uint32
}

type AuditRoundState struct {
    TargetType      AuditTargetType
    TargetVersion   string
    Round           uint32
    RequestID       string
    ExpectedAgents  []string
    DeadlineAt      time.Time
    LeaseDuration   time.Duration
    Status          string
}
```

### 4.3 Issue / PR / 副作用

```go
type IssueRef struct {
    Provider   string
    Repo       string
    IssueID    string
    IssueURL   string
}

type PRRef struct {
    Provider    string
    Repo        string
    PRID        string
    PRURL       string
    HeadSHA     string
    MergeStatus string
}

type OutboxRecord struct {
    ActionID       string
    TaskID         string
    Domain         MCPDomain
    ActionType     OutboxActionType
    IdempotencyKey string
    PayloadRef     string
    Status         string
    AttemptCount   uint32
    NextAttemptAt  time.Time
    LastError      string
}
```

### 4.4 确认与调度

```go
type Confirmation struct {
    ConfirmationID    string
    ConfirmationToken string
    TaskID            string
    ActionHash        string
    RequestedBy       string
    ApproverID        string
    Channel           string
    ExpiresAt         time.Time
    Status            string
}

type ScheduledTask struct {
    ScheduleID      string
    OwnerID         string
    TriggerSpec     string
    Payload         json.RawMessage
    NextRunAt       time.Time
    LastRunAt       *time.Time
    LastFireID      string
    Status          string
}

type ScheduleFire struct {
    FireID         string
    ScheduleID     string
    ScheduledFor   time.Time
    TaskID         string
    IdempotencyKey string
    Status         string
}
```

推荐状态值：

- `ScheduleFire.Status`: `registered`, `task_create_pending`, `task_created`, `abandoned`

## 5. 命令模型

命令由入口、workflow、reconciler 或 outbox 回执发起，交给 `bus` 执行。

| 命令 | 用途 |
| --- | --- |
| `CreateTask` | 创建新任务并进入 `task_created` |
| `RegisterExternalEvent` | 记录外部事件受理与稳定 dedupe 键 |
| `BindIssue` | 绑定现有 issue 或新建镜像 issue，并进入 `issue_created` |
| `MarkIssueSyncPending` | 标记等待 issue 镜像建立 |
| `MarkWaitingHuman` | 进入 `waiting_human`，附 `waiting_reason` |
| `ConsumeConfirmation` | 消费确认对象并恢复后续流程 |
| `StartSyncRun` | 启动同步处理 |
| `MarkOutboxInflight` | 把某个 `outbox` 动作标记为 `inflight` |
| `CompleteOutboxAction` | 把某个 `outbox` 动作标记为完成并写入外部回执摘要 |
| `RequeueOutboxAction` | 把某个 `outbox` 动作按退避重新排队 |
| `MarkOutboxDead` | 把某个 `outbox` 动作标记为 `dead` |
| `PublishPlan` | 保存计划、切换到 `plan_audit` |
| `StartAuditRound` | 为计划或代码启动一轮审核 |
| `RecordAuditLease` | 审核 Agent 接单或续租 |
| `SubmitAuditVerdict` | 记录审核意见 |
| `FinalizeAuditRound` | 聚合审核结论 |
| `StartCoding` | 进入 `coding` |
| `AttachOrUpdatePR` | 绑定或更新活动 PR |
| `StartEvaluation` | 进入 `evaluation` |
| `RecordEvalResult` | 写入评测结果 |
| `TripBudgetFuse` | 进入 `budget_exceeded` |
| `RequestMergeApproval` | 进入 `merge_approval_pending` |
| `StartMerging` | 进入 `merging` |
| `MarkMerged` | 标记已合并 |
| `CloseTask` | 进入 `closed` |
| `FailTask` | 进入 `failed` |
| `CancelTask` | 进入 `cancelled` |
| `RegisterScheduleFire` | 注册一次定时触发实例，确保补偿触发可幂等 |
| `MarkScheduleFireTaskCreatePending` | 把某次 fire 标记为正在补建 task |
| `AttachScheduleFireTask` | 把已创建的 `task_id` 绑定回对应 fire |
| `AbandonScheduleFire` | 多次补建失败后放弃某次 fire 并进入人工处理 |
| `ResumeTask` | 人工恢复 `waiting_human` 或恢复型挂起任务 |
| `RetryOutboxAction` | 对指定 `outbox` 动作发起人工重试 |
| `ResolveDeadLetter` | 对死信对象给出人工处理结论 |

规则：

- 命令不直接持久化；命令处理器先校验状态，再产生事件
- 同一命令重复提交时，必须能通过 `task_version` 或幂等键拒绝重复执行
- 所有管理面写接口都必须映射到这里定义的命令，不能直接绕过 BUS 改 store

## 6. 事件模型

事件是状态变更的事实记录，必须 append-only。

推荐事件：

- `TaskCreated`
- `ExternalEventAccepted`
- `ExternalEventRejected`
- `TaskWaitingHumanMarked`
- `SyncRunStarted`
- `SyncRunCompleted`
- `IssueSyncPendingMarked`
- `IssueBound`
- `PlanPublished`
- `AuditRoundStarted`
- `AuditLeaseAccepted`
- `AuditVerdictSubmitted`
- `AuditRoundFinalized`
- `CodingStarted`
- `PRAttached`
- `EvaluationStarted`
- `EvaluationCompleted`
- `BudgetFuseTripped`
- `MergeApprovalRequested`
- `MergeStarted`
- `TaskMerged`
- `TaskClosed`
- `TaskFailed`
- `TaskCancelled`
- `OutboxActionCreated`
- `OutboxActionInflightMarked`
- `OutboxActionRequeued`
- `OutboxActionCompleted`
- `OutboxActionFailed`
- `OutboxActionDeadMarked`
- `ConfirmationIssued`
- `ConfirmationConsumed`
- `ScheduleFireRegistered`
- `ScheduleFireTaskCreatePending`
- `ScheduleFireTaskAttached`
- `ScheduleFireAbandoned`
- `ArbitrationRequested`
- `ArbitrationResolved`
- `DeadLetterRaised`
- `DeadLetterResolved`

推荐事件信封：

```go
type EventEnvelope struct {
    EventID         string
    EventType       string
    TaskID          string
    TaskVersion     uint64
    GlobalHLC       string
    ParentEventID   string
    CausationID     string
    OriginActor     string
    OccurredAt      time.Time
    Payload         json.RawMessage
}
```

## 7. 状态转移规则

实现中必须维护显式状态转移表，而不是在 handler 里散落判断。
`Task` 生命周期从 `task_created` 开始；`received` 与 `classified` 只能出现在 `IngressTrace`。

| 当前状态 | 允许转移 |
| --- | --- |
| `task_created` | `sync_running`, `waiting_human`, `issue_sync_pending`, `issue_created`, `failed`, `cancelled` |
| `sync_running` | `closed`, `failed`, `cancelled` |
| `issue_sync_pending` | `issue_created`, `waiting_human`, `failed`, `cancelled` |
| `issue_created` | `planning`, `failed`, `cancelled` |
| `planning` | `plan_audit`, `failed`, `cancelled` |
| `plan_audit` | `planning`, `coding`, `pending_arbitration`, `cancelled` |
| `coding` | `evaluation`, `code_audit`, `failed`, `cancelled` |
| `evaluation` | `coding`, `code_audit`, `budget_exceeded`, `waiting_human`, `failed`, `cancelled` |
| `budget_exceeded` | `waiting_human`, `cancelled` |
| `code_audit` | `coding`, `pending_arbitration`, `merge_approval_pending`, `merging`, `closed`, `cancelled` |
| `merge_approval_pending` | `merging`, `coding`, `cancelled` |
| `merging` | `merged`, `waiting_human`, `failed`, `cancelled` |
| `merged` | `closed` |
| `waiting_human` | `sync_running`, `issue_sync_pending`, `planning`, `coding`, `evaluation`, `merge_approval_pending`, `cancelled` |
| `pending_arbitration` | `planning`, `coding`, `code_audit`, `cancelled` |

## 8. 不变式

实现必须至少校验以下不变式：

1. 同一 `task_id` 任意时刻只允许一个活动编码轮次。
2. 同一 `task_id` 任意时刻只允许一个活动 PR。
3. `plan_audit` 中的 verdict 必须绑定当前 `plan_version`。
4. `code_audit` 中的 verdict 必须绑定当前 `pr_head_sha`。
5. 旧版本 verdict 只能落审计，不允许推进主状态。
6. `waiting_human` 必须带 `waiting_reason`。
7. `budget_exceeded` 触发后，不得再创建新的 `coding` / `evaluation` 任务。
8. 外部副作用必须先生成 `OutboxActionCreated`，后执行。
9. `outbox` 队列和外部事件 dedupe 索引必须能仅由事件日志重放重建，不能额外要求第二耐久化提交边界。
10. 同一 `schedule_id + scheduled_for` 只能注册一个 `ScheduleFire`。
11. 进入 `waiting_human`、`pending_arbitration`、`failed`、`cancelled`、`budget_exceeded` 等挂起或终态前，必须先撤销活动 `CancellationToken`。
12. `received`、`classified` 不得作为 `TaskStatus` 或 reducer 分支条件出现。

## 9. reducer 实现要求

每个事件必须由纯 reducer 更新内存态和投影态：

```go
type Reducer interface {
    Apply(task *Task, event EventEnvelope) error
}
```

禁止行为：

- reducer 内做网络调用
- reducer 内生成新 ID
- reducer 内读取外部当前时间

这些值应在命令处理阶段准备好，再作为事件 payload 落盘。

## 10. 最小测试矩阵

`internal/domain` 至少要有以下测试：

- 状态转移表测试
- 旧 `plan_version` verdict 被拒绝测试
- 旧 `pr_head_sha` verdict 被拒绝测试
- `waiting_human` 缺少 `waiting_reason` 校验测试
- `budget_exceeded` 后禁止新评测测试
- 事件 reducer 幂等重放测试
