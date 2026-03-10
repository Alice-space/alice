# TDR 07: `outbox + MCP`、控制面与调度

## 1. 目标

本文统一定义：

- `outbox + MCP`
- MCP HTTP/JSON 协议
- `schedule-management`
- `workflow-management`
- `ScheduledTask`
- scheduler fire 与补偿

## 2. `outbox + MCP`

### 2.1 基本原则

所有外部副作用都必须：

1. 先形成 `OutboxRecord`
2. 由 `outbox-dispatcher` 调 MCP
3. 收到结果后再写回 BUS

只读查询不走 `outbox`，但仍要留下 `ToolCallRecord`。

### 2.2 action 接口

建议核心到 MCP 统一使用：

- `POST /v1/actions`
- `GET /v1/actions/{action_id}`
- `POST /v1/actions/lookup`
- `POST /v1/queries`
- `GET /healthz`

### 2.3 action 请求

```go
type MCPActionRequest struct {
    ActionID        string          `json:"action_id"`
    IdempotencyKey  string          `json:"idempotency_key"`
    TraceID         string          `json:"trace_id"`
    TaskID          string          `json:"task_id"`
    ExecutionID     string          `json:"execution_id"`
    Domain          string          `json:"domain"`
    ActionType      string          `json:"action_type"`
    TargetRef       string          `json:"target_ref"`
    Payload         json.RawMessage `json:"payload"`
    DeadlineAt      time.Time       `json:"deadline_at"`
}
```

### 2.4 action 响应

```go
type MCPActionResponse struct {
    ActionID        string          `json:"action_id"`
    Status          string          `json:"status"`
    ExternalRef     string          `json:"external_ref"`
    Result          json.RawMessage `json:"result"`
    ErrorCode       string          `json:"error_code"`
    ErrorMessage    string          `json:"error_message"`
    RemoteRequestID string          `json:"remote_request_id"`
}
```

`Status` 枚举固定为：

- `accepted`：远端已接受，结果稍后通过 query 或 webhook 确认
- `completed`：远端已完成，结果可直接回写
- `rejected`：远端明确拒绝，需看错误码决定 `retry_wait` 或 `dead`

这里的 `Status` 只表示“提交请求的立即返回结果”，不表示后续轮询状态。

### 2.4a action 查询响应

`GET /v1/actions/{action_id}` 必须返回独立的状态 schema，而不是复用提交响应；按 `remote_request_id` / `idempotency_key` 的降级查询则通过 `POST /v1/actions/lookup` 完成：

```go
type MCPActionLookupRequest struct {
    ActionID        string `json:"action_id"`
    RemoteRequestID string `json:"remote_request_id"`
    IdempotencyKey  string `json:"idempotency_key"`
}
```

```go
type MCPActionStatusResponse struct {
    ActionID        string          `json:"action_id"`
    RemoteRequestID string          `json:"remote_request_id"`
    IdempotencyKey  string          `json:"idempotency_key"`
    Status          string          `json:"status"`
    ExternalRef     string          `json:"external_ref"`
    Result          json.RawMessage `json:"result"`
    ErrorCode       string          `json:"error_code"`
    ErrorMessage    string          `json:"error_message"`
    UpdatedAt       time.Time       `json:"updated_at"`
}
```

`MCPActionStatusResponse.Status` 枚举固定为：

- `pending`
- `running`
- `completed`
- `failed`
- `unknown`

查询优先级固定为：

1. `action_id`
2. `remote_request_id`
3. `idempotency_key`

### 2.5 query 请求

```go
type MCPQueryRequest struct {
    TraceID     string          `json:"trace_id"`
    QueryType   string          `json:"query_type"`
    QueryTarget string          `json:"query_target"`
    Params      json.RawMessage `json:"params"`
    DeadlineAt  time.Time       `json:"deadline_at"`
}
```

### 2.6 query 响应

```go
type MCPQueryResponse struct {
    Status       string          `json:"status"`
    ExternalRef  string          `json:"external_ref"`
    Result       json.RawMessage `json:"result"`
    ErrorCode    string          `json:"error_code"`
    ErrorMessage string          `json:"error_message"`
}
```

`MCPQueryResponse.Status` 枚举固定为：

- `pending`
- `running`
- `completed`
- `failed`
- `unknown`

### 2.7 webhook 回执

支持 webhook 回执的 MCP 域，必须把回执也规范成稳定 schema：

```go
type MCPWebhookReceipt struct {
    ActionID        string          `json:"action_id"`
    RemoteRequestID string          `json:"remote_request_id"`
    IdempotencyKey  string          `json:"idempotency_key"`
    Status          string          `json:"status"`
    ExternalRef     string          `json:"external_ref"`
    Result          json.RawMessage `json:"result"`
    ErrorCode       string          `json:"error_code"`
    ErrorMessage    string          `json:"error_message"`
    ReceivedAt      time.Time       `json:"received_at"`
}
```

## 3. MCP 域

v1 固定五个 MCP 域：

- `github`
- `gitlab`
- `cluster`
- `control`
- `workflow-registry`

其中：

- `control` 负责调度对象与 Alice 内部高风险控制项
- `workflow-registry` 负责 workflow revision 查询、发布、回滚；若初期由 GitHub/GitLab 承担，也要保留独立域抽象

## 4. 错误与重试

### 4.1 错误分类

- `retryable`
- `permanent`
- `rate_limited`
- `unauthorized`
- `conflict`
- `not_found`

### 4.2 重试策略

- `retryable` / `rate_limited`：指数退避
- `unauthorized` / `permanent`：直接转 `dead`
- `conflict`：先做对账，再决定是否重试

### 4.3 action lookup 与对账键

reconciler 与 webhook 处理都必须遵循同一查找顺序：

1. 先按 `action_id`
2. 再按 `remote_request_id`
3. 最后按 `idempotency_key`

如果三个键都不存在，回执不得直接推进业务 task，必须进入 dead letter 或人工处理。

### 4.4 webhook 回执与自回声抑制

对于 Alice 自己发出的 comment / PR / publish 动作：

- action 提交成功后写入 `remote_request_id`
- `OutboxRecord.ReceiptWindowUntil` 记录最近一次允许匹配回执的时间窗
- webhook 回流时，如果命中同一 `action_id` 或 `remote_request_id` 或 `idempotency_key`，且仍在 receipt window 内，则只做回执确认，不重新触发业务 step

### 4.5 outbox 状态推进

| MCP 结果 | `OutboxRecord.Status` | 处理 |
| --- | --- | --- |
| `completed` | `succeeded` | 写回 `LastExternalRef`、`RemoteRequestID`、业务引用 |
| `accepted` | `dispatching` | 等待 webhook 或 `GET /v1/actions/{action_id}` |
| `rejected + retryable/rate_limited` | `retry_wait` | 计算 `NextAttemptAt` |
| `rejected + permanent/unauthorized` | `dead` | 进入 dead letter |
| `rejected + conflict` | `dispatching` | 先 lookup，对账后再决定 `succeeded/retry_wait/dead` |

lookup 状态到 outbox 状态的映射固定为：

| `MCPActionStatusResponse.Status` | `OutboxRecord.Status` | 处理 |
| --- | --- | --- |
| `pending` / `running` | `dispatching` | 刷新 `LastReceiptStatus` 和 `ReceiptWindowUntil` |
| `completed` | `succeeded` | 补写业务引用并收口 |
| `failed` | `retry_wait` 或 `dead` | 按 `ErrorCode` 判定 |
| `unknown` | `dispatching` 或 `dead` | 保守重试；超过阈值转人工 |

### 4.6 “远端成功，本地未回写”恢复

恢复流程：

1. 扫描 `dispatching` 超时动作
2. 优先调 `GET /v1/actions/{action_id}`；缺失时降级按 `remote_request_id/idempotency_key` lookup
3. 若远端 `completed`，补写 `OutboxRecord.LastExternalRef`、`RemoteRequestID` 和业务引用
4. 若远端 `failed`，按错误码转 `retry_wait` 或 `dead`
5. 若 `pending/running`，刷新 `ReceiptWindowUntil` 并保留 `dispatching`
6. 若 `unknown`，保守重试或升级人工处理

## 5. `schedule-management`

### 5.1 `ScheduledTask`

```go
type ScheduledTask struct {
    ScheduledTaskID string
    SpecKind        string
    SpecText        string
    Timezone        string
    InputTemplate   string
    TargetWorkflowID string
    TargetWorkflowSource string
    TargetWorkflowRev string
    ScheduleRevision string
    Enabled         bool
    NextFireAt      time.Time
    LastFireAt      time.Time
    UpdatedAt       time.Time
}
```

关键点：

- 必须固化 `workflow_id/source/rev`
- 必须固化 `schedule_revision`
- 不能只存“workflow 名字”

### 5.2 cron 语义

v1 使用 `robfig/cron/v3`：

- 统一时区解释
- 统一下一次触发计算
- 统一 DST 边界行为

### 5.3 step 与动作

默认 step：

- `parse_request`
- `validate_request`
- `apply_change`
- `report`

典型 outbox action：

- `control.create_schedule`
- `control.update_schedule`
- `control.pause_schedule`
- `control.delete_schedule`

## 6. scheduler

### 6.1 fire 生成

scheduler 每次扫描到应触发的 `ScheduledTask` 时，先生成：

- `fire_id`
- 带 `source_schedule_revision` 的 `ScheduleTriggered` `ExternalEvent`

然后再进入统一入口。

### 6.2 `fire_id`

```text
fire_id = sha256(<scheduled_task_id> + ":" + <scheduled_for_window>)
```

### 6.2a `ScheduleTriggered` payload

```go
type ScheduleTriggeredPayload struct {
    FireID                  string    `json:"fire_id"`
    ScheduledTaskID         string    `json:"scheduled_task_id"`
    ScheduledForWindow      time.Time `json:"scheduled_for_window"`
    SourceScheduleRevision  string    `json:"source_schedule_revision"`
    TargetWorkflowID        string    `json:"target_workflow_id"`
    TargetWorkflowSource    string    `json:"target_workflow_source"`
    TargetWorkflowRev       string    `json:"target_workflow_rev"`
}
```

这个 payload 不是冗余字段，而是恢复和争议审计的最低契约。

### 6.3 missed window

v1 建议：

- 最多补偿最近 `N` 个窗口
- 超过上限则告警并要求人工处理

## 7. `workflow-management`

### 7.1 关键对象

- `workflow_change_request`
- `validation_report`
- 候选 diff / PR
- 新 `workflow_source`
- 新 `workflow_rev`
- 新 `manifest_digest`

### 7.2 默认 step

- `parse_change_request`
- `read_current_workflow`
- `produce_diff`
- `validate_change`
- `publish`
- `report`

常见 gate：

- `approval`
- `confirmation`

### 7.3 publish 动作

典型 action：

- `workflow-registry.publish`
- `workflow-registry.rollback`
- `github.create_pr` / `gitlab.create_mr`（如果由代码托管域承载）

### 7.4 不漂移规则

- 已运行 task 保持旧 binding
- 新 task 使用新 revision
- 显式 rebind 只能追加新 binding 记录

## 8. 关键接口

```go
type MCPClient interface {
    Action(ctx context.Context, req *MCPActionRequest) (*MCPActionResponse, error)
    Query(ctx context.Context, req *MCPQueryRequest) (*MCPQueryResponse, error)
}

type Scheduler interface {
    Tick(ctx context.Context, now time.Time) error
}
```

## 9. 测试建议

必须覆盖：

- `OutboxRecord` 幂等键稳定
- MCP 成功但 BUS 未回写后的恢复
- webhook self-echo 只确认不重跑业务
- `ScheduledTask` 固化 workflow revision
- 相同 `fire_id` 不会重复 promote 新 task
- workflow 发布后旧 task 不漂移
