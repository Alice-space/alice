# TDR 01: 核心领域模型与审计对象

## 1. 目标

本文把 CDR 中的核心概念落成可编码的领域模型。v1 的实现必须直接复用本文命名，避免再出现一套旧的 `Task/RouteDecision` 影子模型。

建议目录：

- `internal/domain/id.go`
- `internal/domain/event.go`
- `internal/domain/request.go`
- `internal/domain/task.go`
- `internal/domain/workflow.go`
- `internal/domain/audit.go`
- `internal/domain/validation.go`

## 2. 领域对象总表

| 对象 | 作用 | 是否 durable |
| --- | --- | --- |
| `ExternalEvent` | 所有输入的统一入口封套 | 是 |
| `EphemeralRequest` | 低风险、只读、短时请求容器 | 是 |
| `PromotionDecision` | promote 与否的结构化判断 | 是 |
| `DurableTask` | 需要治理与恢复的长期对象 | 是 |
| `WorkflowBinding` | task 绑定到某个 workflow revision 的记录 | 是 |
| `StepExecution` | 某个 step 的运行记录 | 是 |
| `Artifact` | workflow step 产物 | 是 |
| `ContextPack` | 子 agent 可消费的上下文快照 | 是 |
| `AgentDispatch` | 子 agent 调度契约 | 是 |
| `ApprovalRequest` | 通用 gate 对象 | 是 |
| `HumanWaitRecord` | `WaitingInput` / `WaitingRecovery` 的持久等待锚点 | 是 |
| `OutboxRecord` | 外部副作用动作 | 是 |
| `ReplyRecord` | 对用户或外部系统可见的回复 | 是 |
| `TerminalResult` | request/task 的终态摘要 | 是 |
| `UsageLedgerEntry` | token/成本/资源用量账本 | 是 |
| `ScheduledTask` | 调度对象 | 是 |
| `AgentProfile` | agent 可执行能力描述 | 否，来自注册表投影 |
| `MCPProfile` | MCP 域健康与配额描述 | 否，来自注册表投影 |
| `OperationLog` | **操作日志记录（应用日志，非真源）** | 否，可重建 |

### 2.1 关于 OperationLog

`OperationLog` 是应用层面的可观测性日志，与事件日志分离但关联：

- **事件日志（Event Log）**: 系统真源，JSONL 格式，记录所有状态变更事件
- **应用日志（Operation Log）**: 可观测性日志，结构化格式，记录操作详情、耗时、调试信息

两者通过以下字段关联：
- `trace_id`: 分布式追踪ID
- `request_id` / `task_id`: 业务对象ID
- `event_id`: 关联事件ID（如适用）

应用日志支持多级别（DEBUG/INFO/WARN/ERROR/FATAL）和组件级配置，用于：
1. 请求生命周期追踪
2. 性能分析
3. 调试和故障排查
4. 审计（辅助事件日志）

## 3. ID 与基础类型

### 3.1 ID 规范

所有主对象 ID 使用 ULID，建议保留前缀方便人工排查：

- `evt_`
- `req_`
- `dec_`
- `task_`
- `bind_`
- `exec_`
- `art_`
- `ctx_`
- `disp_`
- `apr_`
- `obx_`
- `rpl_`
- `res_`
- `sch_`

### 3.2 枚举

```go
type RequestStatus string
type TaskStatus string
type WaitingReason string
type PromotionResult string
type GateType string
type GateStatus string
type StepStatus string
type DispatchStatus string
type OutboxStatus string
type EventType string
type Role string
```

推荐枚举值：

- `RequestStatus`: `Open`, `Answered`, `Promoted`, `Expired`
- `TaskStatus`: `NewTask`, `Active`, `WaitingHuman`, `Succeeded`, `Failed`, `Cancelled`
- `WaitingReason`: `WaitingInput`, `WaitingConfirmation`, `WaitingBudget`, `WaitingRecovery`
- `PromotionResult`: `direct_answer`, `promote`, `ask_followup`, `escalate_human`
- `GateType`: `approval`, `confirmation`, `budget`, `evaluation`
- `GateStatus`: `open`, `approved`, `rejected`, `expired`, `superseded`
- `StepStatus`: `ready`, `running`, `succeeded`, `failed`, `superseded`, `cancelled`
- `DispatchStatus`: `created`, `dispatched`, `running`, `completed`, `failed`, `cancelled`, `expired`
- `OutboxStatus`: `pending`, `dispatching`, `succeeded`, `retry_wait`, `dead`
- `Role`: `reception`, `leader`, `helper`, `worker`, `reviewer`, `evaluator`

## 4. 核心聚合与记录

### 4.1 `ExternalEvent`

```go
type ExternalEvent struct {
    EventID            string
    EventType          EventType
    SourceKind         string
    TransportKind      string
    SourceRef          string
    ActorRef           string
    RequestID          string
    TaskID             string
    ReplyToEventID     string
    ConversationID     string
    ThreadID           string
    RepoRef            string
    IssueRef           string
    PRRef              string
    CommentRef         string
    ScheduledTaskID    string
    ControlObjectRef   string
    WorkflowObjectRef  string
    CoalescingKey      string
    ParentEventID      string
    CausationID        string
    IdempotencyKey     string
    Verified           bool
    PayloadRef         string
    ReceivedAt         time.Time
}
```

约束：

- 所有输入先成为 `ExternalEvent`
- `ExternalEvent` 是 route 的输入，不是“原始消息 DTO”
- scheduler 触发、人类按钮回流、repo comment、webhook 都用同一对象
- `SourceKind` 表示语义输入类型，用于 route/coalescing/policy
- `TransportKind` 表示 adapter 入口，用于审计与诊断，不参与 route key 选择

`ExternalEvent.EventType` 的最低 ingress 基线：

- `DirectInputReceived`
- `WebFormMessageReceived`
- `RepoIssueCommentReceived`
- `RepoPRCommentReceived`
- `ControlPlaneMessageReceived`
- `HumanActionSubmitted`
- `ScheduleTriggered`

这些 ingress 类型被事件日志持久化时，仍统一包在 `ExternalEventIngested` 的 payload 里；它们不是新的 `EventEnvelope.EventType` 行。

### 4.2 `EphemeralRequest`

```go
type EphemeralRequest struct {
    RequestID             string
    Status                RequestStatus
    OpenedByEventID       string
    LastEventID           string
    TraceID               string
    IntentSummary         string
    RouteSnapshot         RouteSnapshot
    PromotionDecisionID   string
    ContextPackIDs        []string
    AgentDispatchIDs      []string
    LastReplyID           string
    TerminalResultID      string
    PromotedTaskID        string
    ExpiresAt             time.Time
    UpdatedAt             time.Time
}
```

约束：

- `EphemeralRequest` 不是迷你 task
- request 不绑定 workflow
- `Answered` / `Expired` 默认不复用

### 4.3 `PromotionDecision`

```go
type PromotionDecision struct {
    DecisionID                 string
    RequestID                  string
    IntentKind                 string
    RequiredRefs               []string
    RiskLevel                  string
    ExternalWrite              bool
    CreatePersistentObject     bool
    Async                      bool
    MultiStep                  bool
    MultiAgent                 bool
    ApprovalRequired           bool
    BudgetRequired             bool
    RecoveryRequired           bool
    ProposedWorkflowIDs        []string
    SelectedWorkflowID         string
    Result                     PromotionResult
    ReasonCodes                []string
    Confidence                 float64
    ProducedBy                 string
    ProducedAt                 time.Time
}
```

约束：

- `Reception` 只能提出 `PromotionDecision`
- 是否 promote 由策略层裁决
- `SelectedWorkflowID` 只有在候选唯一且校验通过时才可写入

### 4.4 `DurableTask`

```go
type DurableTask struct {
    TaskID                 string
    SourceRequestID        string
    OpenedByEventID        string
    TraceID                string
    Status                 TaskStatus
    WaitingReason          WaitingReason
    CurrentBindingID       string
    CurrentStepExecutionID string
    RiskLevel              string
    BudgetPolicyRef        string
    CancellationRef        string
    UpdatedAt              time.Time
}
```

顶层不允许出现：

- `planning`
- `code_audit`
- `merge_approval_pending`
- `budget_exceeded`

这些都属于 workflow step 或 gate 结果，不是 task 顶层状态。

### 4.5 `WorkflowBinding`

```go
type WorkflowBinding struct {
    BindingID         string
    TaskID            string
    WorkflowID        string
    WorkflowSource    string
    WorkflowRev       string
    ManifestDigest    string
    ManifestRef       string
    EntryStepID       string
    BoundByEventID    string
    BoundReason       string
    SupersededBy      string
    Active            bool
    BoundAt           time.Time
}
```

约束：

- 单条 binding 记录不可原地改写
- 若允许重规划，只能追加新 binding 记录
- 旧 binding 的 step/gate/outbox 审计必须保留

### 4.6 `StepExecution`

```go
type StepExecution struct {
    ExecutionID        string
    TaskID             string
    BindingID          string
    StepID             string
    Role               Role
    Status             StepStatus
    Attempt            uint32
    ParentDispatchID   string
    InputArtifactIDs   []string
    OutputArtifactIDs  []string
    CheckpointRef      string
    ResumeToken        string
    RemoteExecutionRef string
    LeaseOwner         string
    LeaseExpiresAt     time.Time
    LastHeartbeatAt    time.Time
    FailureCode        string
    FailureMessage     string
    SupersededBy       string
    StartedAt          time.Time
    FinishedAt         time.Time
}
```

### 4.7 `Artifact`

```go
type Artifact struct {
    ArtifactID       string
    TaskID           string
    BindingID        string
    ExecutionID      string
    Family           string
    SchemaID         string
    SchemaVersion    string
    ContentRef       string
    Summary          string
    SupersededBy     string
    CreatedAt        time.Time
}
```

artifact family 基线：

- `task_brief`
- `plan`
- `analysis_notes`
- `candidate_patch`
- `test_notes`
- `review_result`
- `evaluation_result`
- `report`
- `schedule_request`
- `workflow_change_request`
- `validation_report`

### 4.8 `ContextPack` 与 `AgentDispatch`

```go
type ContextPack struct {
    ContextPackID        string
    OwnerKind            string
    OwnerID              string
    SummaryRef           string
    ConversationSliceRef string
    ArtifactIDs          []string
    ExternalRefSnapshot  map[string]string
    WorkingStateRef      string
    ContextDigest        string
    CreatedAt            time.Time
}

type AgentDispatch struct {
    DispatchID        string
    OwnerKind         string
    OwnerID           string
    ParentExecutionID string
    InitiatorRole     Role
    AgentLabel        string
    RequestedRole     Role
    Goal              string
    ContextPackID     string
    InputRefs         []string
    ExpectedOutputs   []string
    AllowedTools      []string
    AllowedMCP        []string
    SandboxTemplate   string
    BudgetCapRef      string
    DeadlineAt        time.Time
    WriteScopeRef     string
    ReturnToRef       string
    IdempotencyKey    string
    RunnerKind        string
    Attempt           uint32
    RemoteExecutionRef string
    CheckpointRef     string
    ResumeToken       string
    LeaseOwner        string
    LeaseExpiresAt    time.Time
    LastHeartbeatAt   time.Time
    FailureCode       string
    FailureMessage    string
    Status            DispatchStatus
    CreatedAt         time.Time
    CompletedAt       time.Time
}
```

关键约束：

- request 内 helper 默认只读
- 子 agent 默认不能直接推进 task 顶层状态
- `allowed_tools`、`allowed_mcp`、`write_scope` 不能超过父 execution 的上界

### 4.9 `ApprovalRequest`

```go
type ApprovalRequest struct {
    ApprovalRequestID  string
    TaskID             string
    BindingID          string
    StepExecutionID    string
    GateType           GateType
    Status             GateStatus
    TargetVersionRef   string
    RequiredSlots      []string
    DeadlineAt         time.Time
    AggregationPolicy  string
    OpenedByEventID    string
    ResolvedByEventID  string
}
```

`ApprovalRequest` 只承载需要人类回流的 gate：

- `approval`
- `confirmation`
- `budget`

`evaluation` gate 默认不实例化 `ApprovalRequest`；它由 `evaluation_result` 和 runtime 自动判定是否继续、回退或结束。只有 manifest 明确要求“评测后转人工决策”时，才额外打开 `approval` / `budget` gate。

### 4.10 `HumanWaitRecord`

`HumanWaitRecord` 只承载非 gate 型的人类等待：

- `WaitingInput`
- `WaitingRecovery`

```go
type HumanWaitRecord struct {
    HumanWaitID       string
    TaskID            string
    BindingID         string
    StepExecutionID   string
    WaitingReason     WaitingReason
    InputSchemaID     string
    ResumeOptions     []string
    PromptRef         string
    Status            string
    OpenedByEventID   string
    ResolvedByEventID string
    DeadlineAt        time.Time
}
```

`WaitingBudget` 和 `WaitingConfirmation` 继续由 `ApprovalRequest` 承载，不复用 `HumanWaitRecord`。

### 4.11 `OutboxRecord`

```go
type OutboxRecord struct {
    ActionID          string
    TaskID            string
    BindingID         string
    ExecutionID       string
    MCPDomain         string
    ActionType        string
    ExternalTargetRef string
    IdempotencyKey    string
    PayloadRef        string
    Status            OutboxStatus
    RemoteRequestID   string
    LastExternalRef   string
    LastReceiptStatus string
    ReceiptWindowUntil time.Time
    AttemptCount      uint32
    NextAttemptAt     time.Time
    LastErrorCode     string
    LastErrorMessage  string
    CreatedAt         time.Time
    UpdatedAt         time.Time
}
```

幂等键基线：

```text
<task_id>:<causation_id>:<action_type>
```

对于 schedule fire 派生动作：

```text
<scheduled_task_id>:<fire_id>:<action_type>
```

### 4.12 `ToolCallRecord`

```go
type ToolCallRecord struct {
    CallID          string
    OwnerKind       string
    OwnerID         string
    ExecutionID     string
    DispatchID      string
    ToolOrMCP       string
    RequestRef      string
    ResponseRef     string
    Status          string
    StartedAt       time.Time
    FinishedAt      time.Time
}
```

### 4.13 `ReplyRecord`

```go
type ReplyRecord struct {
    ReplyID          string
    OwnerKind        string
    OwnerID          string
    ReplyChannel     string
    ReplyToEventID   string
    PayloadRef       string
    Final            bool
    DeliveryStatus   string
    DeliveredAt      time.Time
}
```

### 4.14 `TerminalResult`

```go
type TerminalResult struct {
    ResultID        string
    OwnerKind       string
    OwnerID         string
    FinalStatus     string
    SummaryRef      string
    FinalReplyID    string
    PrimaryRef      string
    ClosedAt        time.Time
}
```

### 4.15 `UsageLedgerEntry`

```go
type UsageLedgerEntry struct {
    EntryID         string
    TaskID          string
    ExecutionID     string
    Domain          string
    Kind            string
    TokenUsed       int64
    CostMicros      int64
    ResourceUnits   int64
    BudgetRemaining int64
    RecordedAt      time.Time
}
```

## 5. 事件封套

所有事件写入日志前必须进入统一封套：

```go
type EventEnvelope struct {
    EventID        string
    AggregateKind  string
    AggregateID    string
    EventType      string
    Sequence       uint64
    GlobalHLC      string
    ParentEventID  string
    CausationID    string
    CorrelationID  string
    TraceID        string
    ProducedAt     time.Time
    Producer       string
    PayloadSchemaID string
    PayloadVersion string
    Payload        json.RawMessage
}
```

规则：

- 一个命令可以产生一个事件批次
- 批次内 `GlobalHLC` 单调递增
- 同一 aggregate 的 `Sequence` 必须连续
- `EventType -> PayloadSchemaID + PayloadVersion` 的映射必须由静态注册表冻结
- reducer、snapshot、dead letter、admin replay 只按 schema 版本解码，不允许“按当前代码猜 payload”

### 5.1 payload ABI 注册表

v1 必须在 `api/events/v1alpha1/` 下维护 JSON Schema，并在代码中保留同名 Go struct。

最低事件注册表如下：

| `EventType` | `AggregateKind` | `PayloadSchemaID` | 用途 |
| --- | --- | --- | --- |
| `ExternalEventIngested` | `request` | `event.external_event_ingested` | 记录标准化后的入口事件 |
| `EphemeralRequestOpened` | `request` | `event.request_opened` | 打开 request |
| `PromotionAssessed` | `request` | `event.promotion_assessed` | 固化 `PromotionDecision` |
| `RequestPromoted` | `request` | `event.request_promoted` | request 原子转入 durable path |
| `RequestAnswered` | `request` | `event.request_answered` | 直答完成 |
| `ContextPackRecorded` | `request`, `task` | `event.context_pack_recorded` | 记录上下文裁剪结果 |
| `AgentDispatchRecorded` | `request`, `task` | `event.agent_dispatch_recorded` | 创建 agent dispatch |
| `AgentDispatchCheckpointed` | `request`, `task` | `event.agent_dispatch_checkpointed` | 更新 dispatch checkpoint / heartbeat |
| `AgentDispatchCompleted` | `request`, `task` | `event.agent_dispatch_completed` | dispatch 收口 |
| `ToolCallRecorded` | `request`, `task` | `event.tool_call_recorded` | 工具或只读 MCP 调用记录 |
| `TaskPromotedAndBound` | `task` | `event.task_promoted_and_bound` | 原子创建 task + binding |
| `TaskWaitingHumanMarked` | `task` | `event.task_waiting_human_marked` | task 进入 `WaitingHuman` |
| `TaskResumed` | `task` | `event.task_resumed` | 人类回流后恢复 task |
| `WorkflowBindingSuperseded` | `task` | `event.binding_superseded` | 追加式 rebind |
| `StepExecutionStarted` | `task` | `event.step_execution_started` | step attempt 开始 |
| `StepExecutionCheckpointed` | `task` | `event.step_execution_checkpointed` | 持久 checkpoint / heartbeat |
| `StepExecutionCompleted` | `task` | `event.step_execution_completed` | step 成功完成 |
| `StepExecutionFailed` | `task` | `event.step_execution_failed` | step 失败 |
| `StepExecutionCancelled` | `task` | `event.step_execution_cancelled` | step 显式取消 |
| `StepExecutionRewound` | `task` | `event.step_execution_rewound` | runtime 按规则回退 step |
| `ApprovalRequestOpened` | `task` | `event.approval_request_opened` | 打开人工 gate |
| `ApprovalRequestResolved` | `task` | `event.approval_request_resolved` | 人工 gate 关闭 |
| `HumanWaitRecorded` | `task` | `event.human_wait_recorded` | 打开 `WaitingInput/WaitingRecovery` 等待锚点 |
| `HumanWaitResolved` | `task` | `event.human_wait_resolved` | 关闭等待锚点 |
| `OutboxQueued` | `task` | `event.outbox_queued` | 排队外部副作用 |
| `OutboxReceiptRecorded` | `task` | `event.outbox_receipt_recorded` | 记录 MCP 提交/查询/回执结果 |
| `ReplyRecorded` | `request`, `task` | `event.reply_recorded` | 结构化回复 |
| `TerminalResultRecorded` | `request`, `task` | `event.terminal_result_recorded` | 终态摘要 |
| `UsageLedgerRecorded` | `task` | `event.usage_ledger_recorded` | 记录预算/成本 |
| `ScheduledTaskRegistered` | `task` | `event.scheduled_task_registered` | 注册或更新 schedule |
| `ScheduleTriggered` | `task` | `event.schedule_triggered` | 固化 fire 实例 |

除了显式标注可空的字段，payload schema 默认不允许新增必填字段时不升版本。

### 5.2 最小 payload struct

建议最低 Go payload 基线如下：

```go
type ExternalEventIngestedPayload struct {
    Event ExternalEvent `json:"event"`
}

type EphemeralRequestOpenedPayload struct {
    RequestID         string    `json:"request_id"`
    OpenedByEventID   string    `json:"opened_by_event_id"`
    RouteSnapshotRef  string    `json:"route_snapshot_ref"`
    ActivatedRouteKeys []string `json:"activated_route_keys"`
    ExpiresAt         time.Time `json:"expires_at"`
}

type PromotionAssessedPayload struct {
    RequestID          string    `json:"request_id"`
    DecisionID         string    `json:"decision_id"`
    Result             string    `json:"result"`
    SelectedWorkflowID string    `json:"selected_workflow_id"`
    ReasonCodes        []string  `json:"reason_codes"`
    Confidence         float64   `json:"confidence"`
}

type RequestPromotedPayload struct {
    RequestID         string    `json:"request_id"`
    TaskID            string    `json:"task_id"`
    RouteSnapshotRef  string    `json:"route_snapshot_ref"`
    RevokedRouteKeys  []string  `json:"revoked_route_keys"`
    PromotedAt        time.Time `json:"promoted_at"`
}

type RequestAnsweredPayload struct {
    RequestID         string    `json:"request_id"`
    FinalReplyID      string    `json:"final_reply_id"`
    RevokedRouteKeys  []string  `json:"revoked_route_keys"`
    AnsweredAt        time.Time `json:"answered_at"`
}

type ContextPackRecordedPayload struct {
    ContextPackID     string            `json:"context_pack_id"`
    OwnerKind         string            `json:"owner_kind"`
    OwnerID           string            `json:"owner_id"`
    SummaryRef        string            `json:"summary_ref"`
    ArtifactIDs       []string          `json:"artifact_ids"`
    ExternalRefs      map[string]string `json:"external_refs"`
    ContextDigest     string            `json:"context_digest"`
    CreatedAt         time.Time         `json:"created_at"`
}

type AgentDispatchRecordedPayload struct {
    DispatchID        string    `json:"dispatch_id"`
    OwnerKind         string    `json:"owner_kind"`
    OwnerID           string    `json:"owner_id"`
    ParentExecutionID string    `json:"parent_execution_id"`
    RequestedRole     string    `json:"requested_role"`
    Goal              string    `json:"goal"`
    ContextPackID     string    `json:"context_pack_id"`
    AllowedTools      []string  `json:"allowed_tools"`
    AllowedMCP        []string  `json:"allowed_mcp"`
    WriteScopeRef     string    `json:"write_scope_ref"`
    ReturnToRef       string    `json:"return_to_ref"`
    DeadlineAt        time.Time `json:"deadline_at"`
}

type AgentDispatchCheckpointedPayload struct {
    DispatchID        string    `json:"dispatch_id"`
    Attempt           uint32    `json:"attempt"`
    Status            string    `json:"status"`
    RemoteExecutionRef string   `json:"remote_execution_ref"`
    CheckpointRef     string    `json:"checkpoint_ref"`
    ResumeToken       string    `json:"resume_token"`
    LastHeartbeatAt   time.Time `json:"last_heartbeat_at"`
}

type AgentDispatchCompletedPayload struct {
    DispatchID        string    `json:"dispatch_id"`
    Status            string    `json:"status"`
    OutputArtifactRefs []string `json:"output_artifact_refs"`
    FailureCode       string    `json:"failure_code"`
    FailureMessage    string    `json:"failure_message"`
    CompletedAt       time.Time `json:"completed_at"`
}

type ToolCallRecordedPayload struct {
    CallID           string    `json:"call_id"`
    OwnerKind        string    `json:"owner_kind"`
    OwnerID          string    `json:"owner_id"`
    ExecutionID      string    `json:"execution_id"`
    DispatchID       string    `json:"dispatch_id"`
    ToolOrMCP        string    `json:"tool_or_mcp"`
    RequestRef       string    `json:"request_ref"`
    ResponseRef      string    `json:"response_ref"`
    Status           string    `json:"status"`
    StartedAt        time.Time `json:"started_at"`
    FinishedAt       time.Time `json:"finished_at"`
}

type TaskPromotedAndBoundPayload struct {
    RequestID          string    `json:"request_id"`
    TaskID             string    `json:"task_id"`
    BindingID          string    `json:"binding_id"`
    WorkflowID         string    `json:"workflow_id"`
    WorkflowSource     string    `json:"workflow_source"`
    WorkflowRev        string    `json:"workflow_rev"`
    ManifestDigest     string    `json:"manifest_digest"`
    EntryStepID        string    `json:"entry_step_id"`
    ReplyToEventID     string    `json:"reply_to_event_id"`
    RepoRef            string    `json:"repo_ref"`
    IssueRef           string    `json:"issue_ref"`
    PRRef              string    `json:"pr_ref"`
    ScheduledTaskID    string    `json:"scheduled_task_id"`
    ControlObjectRef   string    `json:"control_object_ref"`
    WorkflowObjectRef  string    `json:"workflow_object_ref"`
    ActivatedRouteKeys []string  `json:"activated_route_keys"`
    RouteSnapshotRef   string    `json:"route_snapshot_ref"`
    PromotedAt         time.Time `json:"promoted_at"`
}

type TaskWaitingHumanMarkedPayload struct {
    TaskID           string    `json:"task_id"`
    WaitingReason    string    `json:"waiting_reason"`
    StepExecutionID  string    `json:"step_execution_id"`
    WaitRef          string    `json:"wait_ref"`
    EnteredAt        time.Time `json:"entered_at"`
}

type TaskResumedPayload struct {
    TaskID           string    `json:"task_id"`
    WaitingReason    string    `json:"waiting_reason"`
    StepExecutionID  string    `json:"step_execution_id"`
    ResumeDecision   string    `json:"resume_decision"`
    ResumePointRef   string    `json:"resume_point_ref"`
    ResumedAt        time.Time `json:"resumed_at"`
}

type StepExecutionStartedPayload struct {
    ExecutionID        string    `json:"execution_id"`
    TaskID             string    `json:"task_id"`
    BindingID          string    `json:"binding_id"`
    StepID             string    `json:"step_id"`
    Attempt            uint32    `json:"attempt"`
    ParentDispatchID   string    `json:"parent_dispatch_id"`
    InputArtifactIDs   []string  `json:"input_artifact_ids"`
    LeaseOwner         string    `json:"lease_owner"`
    LeaseExpiresAt     time.Time `json:"lease_expires_at"`
    RemoteExecutionRef string    `json:"remote_execution_ref"`
}

type StepExecutionCheckpointedPayload struct {
    ExecutionID        string    `json:"execution_id"`
    Attempt            uint32    `json:"attempt"`
    CheckpointRef      string    `json:"checkpoint_ref"`
    ResumeToken        string    `json:"resume_token"`
    RemoteExecutionRef string    `json:"remote_execution_ref"`
    LastHeartbeatAt    time.Time `json:"last_heartbeat_at"`
}

type StepExecutionCompletedPayload struct {
    ExecutionID        string    `json:"execution_id"`
    Attempt            uint32    `json:"attempt"`
    OutputArtifactRefs []string  `json:"output_artifact_refs"`
    SummaryRef         string    `json:"summary_ref"`
    CompletedAt        time.Time `json:"completed_at"`
}

type StepExecutionFailedPayload struct {
    ExecutionID    string    `json:"execution_id"`
    Attempt        uint32    `json:"attempt"`
    FailureCode    string    `json:"failure_code"`
    FailureMessage string    `json:"failure_message"`
    Retryable      bool      `json:"retryable"`
    FailedAt       time.Time `json:"failed_at"`
}

type StepExecutionCancelledPayload struct {
    ExecutionID        string    `json:"execution_id"`
    Attempt            uint32    `json:"attempt"`
    ReasonCode         string    `json:"reason_code"`
    RemoteExecutionRef string    `json:"remote_execution_ref"`
    CancelledAt        time.Time `json:"cancelled_at"`
}

type StepExecutionRewoundPayload struct {
    TaskID            string    `json:"task_id"`
    FromExecutionID   string    `json:"from_execution_id"`
    ToStepID          string    `json:"to_step_id"`
    DecisionRef       string    `json:"decision_ref"`
    RewoundAt         time.Time `json:"rewound_at"`
}

type ApprovalRequestOpenedPayload struct {
    ApprovalRequestID string    `json:"approval_request_id"`
    TaskID            string    `json:"task_id"`
    StepExecutionID   string    `json:"step_execution_id"`
    GateType          string    `json:"gate_type"`
    TargetVersionRef  string    `json:"target_version_ref"`
    RequiredSlots     []string  `json:"required_slots"`
    DeadlineAt        time.Time `json:"deadline_at"`
}

type ApprovalRequestResolvedPayload struct {
    ApprovalRequestID string    `json:"approval_request_id"`
    Resolution        string    `json:"resolution"`
    ResolvedByActor   string    `json:"resolved_by_actor"`
    ResolutionRef     string    `json:"resolution_ref"`
    ResolvedAt        time.Time `json:"resolved_at"`
}

type HumanWaitRecordedPayload struct {
    HumanWaitID      string    `json:"human_wait_id"`
    TaskID           string    `json:"task_id"`
    StepExecutionID  string    `json:"step_execution_id"`
    WaitingReason    string    `json:"waiting_reason"`
    InputSchemaID    string    `json:"input_schema_id"`
    ResumeOptions    []string  `json:"resume_options"`
    PromptRef        string    `json:"prompt_ref"`
    DeadlineAt       time.Time `json:"deadline_at"`
}

type HumanWaitResolvedPayload struct {
    HumanWaitID      string    `json:"human_wait_id"`
    WaitingReason    string    `json:"waiting_reason"`
    Resolution       string    `json:"resolution"`
    ResolvedByActor  string    `json:"resolved_by_actor"`
    ResolutionRef    string    `json:"resolution_ref"`
    ResolvedAt       time.Time `json:"resolved_at"`
}

type OutboxQueuedPayload struct {
    ActionID       string    `json:"action_id"`
    Domain         string    `json:"domain"`
    ActionType     string    `json:"action_type"`
    TargetRef      string    `json:"target_ref"`
    IdempotencyKey string    `json:"idempotency_key"`
    PayloadRef     string    `json:"payload_ref"`
    DeadlineAt     time.Time `json:"deadline_at"`
}

type OutboxReceiptRecordedPayload struct {
    ActionID        string    `json:"action_id"`
    ReceiptSource   string    `json:"receipt_source"`
    ReceiptKind     string    `json:"receipt_kind"`
    ReceiptStatus   string    `json:"receipt_status"`
    RemoteRequestID string    `json:"remote_request_id"`
    ExternalRef     string    `json:"external_ref"`
    ErrorCode       string    `json:"error_code"`
    ErrorMessage    string    `json:"error_message"`
    RecordedAt      time.Time `json:"recorded_at"`
}

type ReplyRecordedPayload struct {
    ReplyID         string    `json:"reply_id"`
    OwnerKind       string    `json:"owner_kind"`
    OwnerID         string    `json:"owner_id"`
    ReplyChannel    string    `json:"reply_channel"`
    ReplyToEventID  string    `json:"reply_to_event_id"`
    PayloadRef      string    `json:"payload_ref"`
    Final           bool      `json:"final"`
    DeliveredAt     time.Time `json:"delivered_at"`
}

type TerminalResultRecordedPayload struct {
    ResultID          string    `json:"result_id"`
    OwnerKind         string    `json:"owner_kind"`
    OwnerID           string    `json:"owner_id"`
    FinalStatus       string    `json:"final_status"`
    FinalReplyID      string    `json:"final_reply_id"`
    RevokedRouteKeys  []string  `json:"revoked_route_keys"`
    ClosedAt          time.Time `json:"closed_at"`
}

type UsageLedgerRecordedPayload struct {
    EntryID          string    `json:"entry_id"`
    TaskID           string    `json:"task_id"`
    ExecutionID      string    `json:"execution_id"`
    Domain           string    `json:"domain"`
    Kind             string    `json:"kind"`
    TokenUsed        int64     `json:"token_used"`
    CostMicros       int64     `json:"cost_micros"`
    ResourceUnits    int64     `json:"resource_units"`
    BudgetRemaining  int64     `json:"budget_remaining"`
    RecordedAt       time.Time `json:"recorded_at"`
}

type ScheduledTaskRegisteredPayload struct {
    ScheduledTaskID    string    `json:"scheduled_task_id"`
    ScheduleRevision   string    `json:"schedule_revision"`
    TargetWorkflowID   string    `json:"target_workflow_id"`
    TargetWorkflowRev  string    `json:"target_workflow_rev"`
    Enabled            bool      `json:"enabled"`
    NextFireAt         time.Time `json:"next_fire_at"`
    RegisteredAt       time.Time `json:"registered_at"`
}
```

实现要求：

- payload struct 名称与 schema 文件名保持一一对应
- 新增字段必须先改 schema，再改 reducer
- `ScheduleTriggeredPayload` 继续以 [07-external-effects-control-plane-and-scheduler.md](/Users/alice/Developer/alice/docs/tdr/07-external-effects-control-plane-and-scheduler.md) 为准

## 6. 命令基线

第一版至少需要这些命令族：

- `IngestExternalEvent`
- `OpenEphemeralRequest`
- `AppendRequestEvent`
- `AssessPromotion`
- `CreateContextPack`
- `RecordAgentDispatch`
- `CheckpointAgentDispatch`
- `CompleteAgentDispatch`
- `RecordToolCall`
- `PromoteAndBindWorkflow`
- `MarkTaskWaitingHuman`
- `ResumeTask`
- `StartStepExecution`
- `CompleteStepExecution`
- `FailStepExecution`
- `CancelStepExecution`
- `RewindStepExecution`
- `SupersedeBinding`
- `CreateApprovalRequest`
- `ResolveApprovalRequest`
- `CreateHumanWaitRecord`
- `ResolveHumanWaitRecord`
- `QueueOutboxRecord`
- `RecordOutboxResult`
- `UpdateUsageLedger`
- `RecordReply`
- `CloseRequest`
- `CloseTask`
- `RegisterScheduledTask`
- `RecordScheduleFire`

命令名按“意图”命名，不按旧平台级业务阶段命名。

## 7. 顶层不变式

1. 任何输入在进入业务逻辑前都必须先变成 `ExternalEvent`。
2. 任何直接回答都必须关联某个 `EphemeralRequest`。
3. 任何 `DurableTask` 都必须拥有且仅有一个 active `WorkflowBinding`。
4. task 从创建起就必须拥有 active binding，不允许存在“已 promote 但未绑定”的中间态。
5. 单条 `WorkflowBinding` 记录不可原地改写。
6. 任何外部副作用都必须先有 `OutboxRecord`。
7. 任何人类 gate 回流都必须先写成新的 `ExternalEvent`。
8. `reply_to_event_id` 命中普通 task，但显式控制面对象键指向另一个治理域时，不能继续吞进原 task。
9. `Answered` / `Expired` 的 request 与终态 task 默认不复用。
10. `WaitingInput` / `WaitingRecovery` 状态下，必须存在且仅存在一个 active `HumanWaitRecord`。
11. `WaitingBudget` / `WaitingConfirmation` 状态下，必须存在且仅存在一个 active `ApprovalRequest`。

## 8. 面向 API 的最小只读视图

为了降低 API 读取复杂度，建议从领域模型直接派生以下只读视图：

- `DirectAnswerView`
- `WorkflowTaskView`
- `HumanActionQueueView`
- `OpsOverviewView`

这些视图来自事件重放与 bbolt 物化层，不是新的持久化真源。
