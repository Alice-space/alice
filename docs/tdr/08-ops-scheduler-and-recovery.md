# TDR 08: 运维视图、Scheduler 与恢复

## 1. 目标

本文定义 `internal/ops` 的实现，覆盖展示投影、通知、scheduler、巡检和恢复流程。

建议目录：

- `internal/ops/readmodel.go`
- `internal/ops/notifier.go`
- `internal/ops/scheduler.go`
- `internal/ops/reconciler.go`
- `internal/ops/health.go`

## 2. OpsReadModel

```go
type OpsReadModel struct {
    TaskID            string
    Status            domain.TaskStatus
    WaitingReason     domain.WaitingReason
    IssueRef          *domain.IssueRef
    PRRef             *domain.PRRef
    PlanVersion       uint32
    PRHeadSHA         string
    BudgetRemaining   decimal.Decimal
    QueueDepth        int
    MCPHealth         map[string]string
    LastStateChangeAt time.Time
    LastError         string
}
```

数据来源：

- 事件 reducer
- store 中的 queue / outbox / MCP health 汇总

## 3. Notifier

`Notifier` 只消费结构化通知事件，不直接读取业务数据库拼消息。

通知类型：

- `task_state_changed`
- `waiting_human_required`
- `budget_alert`
- `audit_arbitration_required`
- `merge_approval_required`
- `system_health_alert`

建议接口：

```go
type Notifier interface {
    Send(ctx context.Context, msg Notification) error
}
```

渠道：

- 飞书卡片
- Web 后台通知中心
- issue / PR 评论补充

## 4. Scheduler

`ScheduledTask` 存储为领域对象，由 scheduler 触发生成普通任务。

```go
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

要求：

- 触发本身也要写事件日志
- 启动时补偿错过窗口
- 每次触发前先注册稳定的 `fire_id`
- 每次触发生成带 `ScheduleFireID` 与 `IdempotencyKey` 的普通 `CreateTask` 命令
- `LastRunAt` 和 `LastFireID` 只是游标；真正的幂等依据是 `ScheduleFire`

稳定触发键规则：

- `fire_id = hash(schedule_id + scheduled_for_window)`
- `scheduled_for_window` 取本次 cron / interval 应触发的逻辑时间，而不是实际执行时间
- `RegisterScheduleFire` 对同一 `fire_id` 必须幂等

推荐调度流程：

```text
tick schedule
  -> compute scheduled_for_window
  -> derive fire_id
  -> submit RegisterScheduleFire(fire_id)
  -> if fire.status in {registered, task_create_pending} and fire.task_id is empty:
       submit CreateTask(idempotency_key=fire_id)
  -> if fire.status == task_created:
       no-op
  -> update NextRunAt / LastRunAt / LastFireID
```

`ScheduleFire` 必须有显式状态推进：

- `registered`
- `task_create_pending`
- `task_created`
- `abandoned`

推荐事实链路：

1. `RegisterScheduleFire` 写入 `ScheduleFireRegistered(status=registered)`
2. 在真正提交 `CreateTask` 前，先写 `ScheduleFireTaskCreatePending`
3. `CreateTask` 成功时，`TaskCreated` 与 `ScheduleFireTaskAttached(task_id=...)` 必须在同一个已提交事件批次内出现
4. 多次恢复仍无法建 task 时，才写 `ScheduleFireAbandoned`

这样即使在 `RegisterScheduleFire` 成功后、`CreateTask` 失败前崩溃，恢复时仍会因为 fire 处于 `registered/task_create_pending` 且没有 `task_id`，被 scheduler 或 reconciler 继续补建，而不是永久丢掉这次触发。

## 5. Reconcilers

第一版至少实现以下巡检器：

| 名称 | 周期 | 职责 |
| --- | --- | --- |
| `outbox_reconciler` | 1m | 对账长时间 `pending/inflight` 动作 |
| `audit_reconciler` | 30s | 检查审核租约、deadline、缺席席位 |
| `eval_reconciler` | 30s | 查询 cluster job 状态并补写结果 |
| `schedule_fire_reconciler` | 30s | 补建 `registered/task_create_pending` 且缺少 `task_id` 的 fire |
| `issue_pr_reconciler` | 2m | 校验 issue / PR 与 BUS 镜像是否一致 |
| `projection_rebuilder` | 手工/开机 | 重建损坏投影 |

## 6. 死信与人工处理

当对象无法自动恢复时，必须进入显式死信队列而不是静默丢失。

建议模型：

```go
type DeadLetter struct {
    DeadID        string
    ObjectType    string
    ObjectID      string
    Reason        string
    SuggestedNext string
    CreatedAt     time.Time
}
```

来源包括：

- 重试耗尽的 `OutboxAction`
- 无法解析的 webhook
- 状态不一致且自动对账失败的任务

## 7. 管理面接口

建议只读接口：

- `GET /api/v1/tasks/{task_id}`
- `GET /api/v1/tasks`
- `GET /api/v1/ops/health`
- `GET /api/v1/ops/outbox`
- `GET /api/v1/ops/deadletters`

建议受控写接口：

- `POST /api/v1/tasks/{task_id}/resume`
- `POST /api/v1/tasks/{task_id}/cancel`
- `POST /api/v1/outbox/{action_id}/retry`
- `POST /api/v1/deadletters/{dead_id}/resolve`

所有写接口都必须回到 BUS，禁止直接改 store 记录。

这些接口分别映射为：

- `ResumeTask`
- `CancelTask`
- `RetryOutboxAction`
- `ResolveDeadLetter`

## 8. 指标与告警

至少暴露以下指标：

- `bus_shard_queue_depth`
- `event_log_append_latency`
- `outbox_pending_count`
- `outbox_dead_count`
- `audit_round_stuck_count`
- `eval_job_running_count`
- `budget_exceeded_count`
- `mcp_domain_circuit_open`
- `confirmation_expired_count`

告警优先级：

- P1：事件日志不可写、快照恢复失败、merge 状态不一致
- P2：outbox 大量积压、MCP 断路器打开、评测作业失联
- P3：单任务长时间 `waiting_human`、单任务多轮审核冲突

## 9. 故障恢复准则

系统遇到故障时遵循：

1. 先保护状态真源
2. 再停止危险副作用
3. 再尝试自动恢复
4. 最后通知人工

典型场景：

- 事件日志损坏：停止接收新流量，进入只读告警模式
- GitHub MCP 失效：暂停 GitHub 相关动作，不影响 cluster 对账
- Cluster 作业状态未知：保持任务不前推，进入 `waiting_human` 或阶段报告

## 10. 最小测试矩阵

- scheduler 补偿触发测试
- 同一 `fire_id` 补偿重放不重复建任务测试
- `ScheduleFireRegistered` 后崩溃，恢复时可继续补建 task 测试
- outbox dead letter 产生测试
- eval reconciler 补写结果测试
- 只读 API 与写接口权限隔离测试
- MCP 单域异常不影响其他域测试
