# TDR 05: Outbox 与 MCP 集成

## 1. 目标

本文定义所有外部副作用的统一执行模型。目标是保证：

- 外部动作一定先被持久化
- 动作可重试、可对账、可恢复
- MCP 是动作执行器，不是业务决策器

本文只覆盖有副作用或需要回执确认的外部动作。只读 MCP 查询不走 `outbox`，见 [04-ingress-reception-and-confirmation.md](./04-ingress-reception-and-confirmation.md) 中的 `sync_mcp_read`。

建议目录：

- `internal/store/outbox.go`
- `internal/mcp/client.go`
- `internal/mcp/dispatcher.go`
- `internal/mcp/github/`
- `internal/mcp/gitlab/`
- `internal/mcp/cluster/`
- `internal/mcp/control/`

## 2. OutboxAction 模型

```go
type OutboxStatus string

const (
    OutboxPending  OutboxStatus = "pending"
    OutboxInflight OutboxStatus = "inflight"
    OutboxDone     OutboxStatus = "done"
    OutboxFailed   OutboxStatus = "failed"
    OutboxDead     OutboxStatus = "dead"
)

type OutboxAction struct {
    ActionID         string
    TaskID           string
    EventID          string
    Domain           domain.MCPDomain
    ActionType       domain.OutboxActionType
    IdempotencyKey   string
    Payload          json.RawMessage
    Status           OutboxStatus
    AttemptCount     uint32
    NextAttemptAt    time.Time
    LastErrorCode    string
    LastErrorMessage string
    CreatedAt        time.Time
    UpdatedAt        time.Time
}
```

`OutboxAction` 是由 `OutboxActionCreated` 等事件物化出来的执行队列，不是独立于事件日志之外的第二真源。

`OutboxAction.Status` 推荐状态机：

- `pending`
- `inflight`
- `done`
- `failed`
- `dead`

## 3. 幂等键规则

默认格式：

```text
task_id:event_id:action_type
```

约束：

- 同一业务动作只能生成一个幂等键
- 幂等键必须写入 `OutboxAction`
- 幂等键必须透传给 MCP
- MCP 执行器必须把幂等键继续透传到底层 API

示例：

- `t_123:e_456:create_issue`
- `t_123:e_789:merge_pr`

## 4. 支持的动作类型

第一版至少支持：

- `create_issue`
- `comment_issue`
- `create_pr`
- `update_pr`
- `comment_pr`
- `submit_review`
- `merge_pr`
- `close_issue`
- `start_cluster_job`
- `cancel_cluster_job`
- `control_call`
- `send_notification`

每种 `ActionType` 都需要对应 payload schema 和 domain adapter。

## 5. MCP 抽象

```go
type Client interface {
    Domain() domain.MCPDomain
    Do(ctx context.Context, req Request) (Response, error)
}

type Request struct {
    RequestID      string
    TaskID         string
    CausationID    string
    OriginActor    string
    ActionType     string
    IdempotencyKey string
    Payload        json.RawMessage
    Timeout        time.Duration
}

type Response struct {
    RequestID    string
    ExternalRef  string
    Retryable    bool
    ErrorCode    string
    RawResponse  json.RawMessage
}
```

约束：

- `Retryable` 必须由 MCP adapter 明确返回
- `ExternalRef` 用于后续对账，例如 issue ID、PR ID、job ID
- `RawResponse` 只保留必要回执，不保存敏感凭据

## 6. Dispatcher 执行流程

```text
select pending outbox action
  -> submit MarkOutboxInflight
  -> call domain MCP with idempotency key
  -> on success:
       submit CompleteOutboxAction
       submit follow-up command to update task state
  -> on retryable failure:
       submit RequeueOutboxAction(next_attempt_at, last_error)
  -> on non-retryable failure:
       submit MarkOutboxDead(last_error) when retries exhausted or error is fatal
       submit workflow failure command if action is critical
```

这里的 `pending outbox action` 来自 `outbox_queue` 物化索引。该索引损坏或缺失时，必须能够通过事件日志 replay 重建。

因此，dispatcher 相关的运行时位置必须都有对应事实事件：

- `OutboxActionInflightMarked`
- `OutboxActionRequeued`
- `OutboxActionCompleted`
- `OutboxActionFailed`
- `OutboxActionDeadMarked`

恢复时必须依据这些事件重建：

- 当前 `Status`
- `AttemptCount`
- `NextAttemptAt`
- `LastErrorCode / LastErrorMessage`
- 是否已经进入 `dead`

建议接口：

```go
type Dispatcher interface {
    Dispatch(ctx context.Context, action OutboxAction) error
}
```

## 7. 重试与退避

退避策略：

- 1m
- 2m
- 5m
- 10m
- 30m

规则：

- `Retryable=true` 才进入下一次退避
- 达到最大次数后转 `dead`
- `dead` 由巡检器和人工后台处理
- `NextAttemptAt` 必须由 `OutboxActionRequeued` 事件显式记录，不能只保存在内存调度器里

动作级别：

| 动作 | 失败策略 |
| --- | --- |
| `create_issue` | 重试，耗尽后让任务进入 `waiting_human` 或 `failed` |
| `comment_issue` | 重试，允许稍后补发 |
| `create_pr` | 重试，耗尽后回到 `coding` 并标错 |
| `merge_pr` | 有限重试，耗尽后任务进入 `failed` 或 `waiting_human` |
| `start_cluster_job` | 重试，失败则让评测回退或进入 `waiting_human` |
| `cancel_cluster_job` | 重试但不阻断主流程，需持续告警 |

## 8. 断路器与限流

每个 MCP domain 维护独立状态：

```go
type DomainHealth struct {
    Domain          domain.MCPDomain
    CircuitState    string
    LastErrorAt     time.Time
    ConsecutiveFail uint32
    RateLimited     bool
}
```

规则：

- 某一域失败不影响其他域
- 断路器打开时拒绝新动作，但允许健康探测
- 手工恢复或半开状态探测成功后再恢复流量

## 9. 域适配器要求

### 9.1 GitHub / GitLab MCP

必须支持：

- create issue / comment issue
- create or update PR
- submit review
- merge PR
- close issue
- query issue / PR state for reconciliation

### 9.2 Cluster MCP

必须支持：

- submit job
- get job status
- cancel job
- fetch artifacts metadata

### 9.3 Control MCP

必须支持：

- query usage
- read or write settings
- schedule management

## 10. 对账策略

重启或恢复时，对 `pending` / `inflight` 动作执行对账：

1. 用 `ExternalRef` 或幂等键查询外部状态
2. 若外部已执行成功，则补写 `OutboxActionCompleted`
3. 若外部未执行且动作此前是 `inflight`，则补写 `OutboxActionRequeued`
4. 若状态不一致且无法判断，进入 `dead` 并告警

特别规则：

- `merge_pr` 对账必须以远端实际 merge 状态为准
- `create_issue` 对账必须避免重复建单

## 11. gRPC / HTTP 协议要求

无论 MCP 最终使用 gRPC 还是 HTTP，协议都必须显式承载：

- `idempotency_key`
- `request_id`
- `task_id`
- `causation_id`
- `timeout_ms`
- `origin_actor`

禁止把幂等和追踪元数据仅放在日志中而不放协议里。

`causation_id` 的用途：

- 把 Alice 主动发出的 comment / review / merge 请求与回流 webhook 关联起来
- 在 webhook 回流时抑制把 Alice 自己的回执再次当成新的 Planner / Audit / Coding 输入

## 12. 最小测试矩阵

- `OutboxAction` 重试与退避测试
- `OutboxActionInflightMarked / Requeued / DeadMarked` 的 replay 重建测试
- 非重试错误直接转 `failed/dead` 测试
- MCP 返回成功但 BUS 回写失败的恢复对账测试
- 相同幂等键重复调度不重复副作用测试
- 回流 webhook 基于 `causation_id` 抑制自触发测试
- 单域断路器打开不影响其他域测试
