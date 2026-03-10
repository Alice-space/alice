# TDR 08: 运维视图、告警与恢复工具

## 1. 目标

本文定义：

- 只读投影
- 运维接口
- 指标与告警
- reconciler
- 人工待办视图
- 恢复工具

它不负责业务真状态推进，只负责让 BUS 可观测、可恢复、可操作。

## 2. 只读视图

### 2.1 `DirectAnswerView`

面向 `EphemeralRequest`：

- `request_id`
- `updated_hlc`
- 输入摘要
- `PromotionDecision` 结果
- toolcall 摘要
- 最终回复
- 耗时

### 2.2 `WorkflowTaskView`

面向 `DurableTask`：

- `task_id`
- `updated_hlc`
- 顶层状态
- `waiting_reason`
- 当前 binding
- step 历史
- gate 队列
- artifact 摘要
- 预算状态
- 最近 outbox 状态

### 2.3 `HumanActionQueueView`

- `entry_kind`
- `entry_id`
- `approval_request_id`
- `human_wait_id`
- `task_id`
- `step_execution_id`
- `gate_type`
- `waiting_reason`
- `status`
- `allowed_decisions`
- `expires_at`
- `updated_hlc`
- 操作入口

### 2.4 `ScheduleView`

- `scheduled_task_id`
- `enabled`
- `spec_kind`
- `spec_text`
- `timezone`
- `target_workflow_id`
- `target_workflow_source`
- `target_workflow_rev`
- `schedule_revision`
- `next_fire_at`
- `last_fire_at`
- `updated_hlc`

### 2.5 `EventView`

- `event_id`
- `event_type`
- `aggregate_kind`
- `aggregate_id`
- `causation_id`
- `payload_schema_id`
- `payload_version`
- `global_hlc`
- 可选 `external.source_kind`
- 可选 `external.transport_kind`
- 可选 `external.source_ref`
- 可选 `external.actor_ref`
- 可选 `external.reply_to_event_id`
- 可选 `external.payload_ref`

### 2.6 `ApprovalView`

- `approval_request_id`
- `task_id`
- `step_execution_id`
- `gate_type`
- `status`
- `allowed_decisions`
- `expires_at`
- `updated_hlc`
- `note`

### 2.7 `HumanWaitView`

- `human_wait_id`
- `task_id`
- `step_execution_id`
- `waiting_reason`
- `status`
- `allowed_decisions`
- `rewind_targets`
- `expires_at`
- `updated_hlc`
- `note`

### 2.8 `DeadLetterView`

- `deadletter_id`
- `source_event_id`
- `failure_stage`
- `last_error`
- `retryable`
- `first_failed_at`
- `last_failed_at`
- `updated_hlc`

### 2.9 `OpsOverviewView`

- shard backlog
- pending outbox
- dead letter 深度
- MCP 健康
- 最近快照时间
- 最近恢复结果

### 2.10 通用列表响应

所有单对象 `GET` 都应返回稳定 envelope：

```go
type GetResponse[T any] struct {
    Item       T      `json:"item"`
    VisibleHLC string `json:"visible_hlc"`
}
```

所有 list endpoint 都应返回稳定 envelope：

```go
type ListResponse[T any] struct {
    Items      []T    `json:"items"`
    NextCursor string `json:"next_cursor"`
    OrderBy    string `json:"order_by"`
    VisibleHLC string `json:"visible_hlc"`
}
```

默认约束：

- 默认排序使用 `updated_hlc desc`
- `events` 列表例外，使用 `global_hlc desc`
- 所有 list/read endpoint 都支持 `min_hlc`
- 当提供 `wait_timeout_ms` 时，server 可以等待投影追到指定 `min_hlc`
- `--wait` 成功条件是 `visible_hlc >= min_hlc`

## 3. Admin 只读接口

建议提供：

- `GET /v1/events/{event_id}`
- `GET /v1/requests/{request_id}`
- `GET /v1/requests/{request_id}/events`
- `GET /v1/requests/{request_id}/toolcalls`
- `GET /v1/requests/{request_id}/reply`
- `GET /v1/requests`
- `GET /v1/tasks/{task_id}`
- `GET /v1/tasks`
- `GET /v1/tasks/{task_id}/steps`
- `GET /v1/tasks/{task_id}/artifacts`
- `GET /v1/tasks/{task_id}/outbox`
- `GET /v1/schedules/{scheduled_task_id}`
- `GET /v1/schedules`
- `GET /v1/approvals/{approval_request_id}`
- `GET /v1/human-waits/{human_wait_id}`
- `GET /v1/events`
- `GET /v1/deadletters`
- `GET /v1/deadletters/{deadletter_id}`
- `GET /v1/human-actions`
- `GET /v1/human-actions/{entry_id}`
- `GET /v1/ops/overview`

读接口的最小查询参数应统一支持：

- 单对象 `GET`：`min_hlc`、`wait_timeout_ms`
- list endpoint：`min_hlc`、`wait_timeout_ms`、`limit`、`cursor`

## 4. Admin 运维接口

建议提供：

- `POST /v1/admin/submit/events`
- `POST /v1/admin/submit/fires`
- `POST /v1/admin/resolve/approval`
- `POST /v1/admin/resolve/wait`
- `POST /v1/admin/reconcile/outbox`
- `POST /v1/admin/reconcile/schedules`
- `POST /v1/admin/rebuild/indexes`
- `POST /v1/admin/replay/from/{hlc}`
- `POST /v1/admin/tasks/{task_id}/cancel`
- `POST /v1/admin/deadletters/{id}/redrive`

这些接口全部需要：

- 单独鉴权
- 独立审计日志
- 不绕过事件日志直接改 durable 对象
- 若会推进业务状态，必须额外生成新的 `ExternalEvent`

## 5. 指标

建议固定以下 Prometheus 指标：

| 指标 | 类型 | 标签 |
| --- | --- | --- |
| `alice_event_append_total` | counter | `result` |
| `alice_shard_queue_depth` | gauge | `shard` |
| `alice_request_open_total` | gauge | 无 |
| `alice_task_active_total` | gauge | `workflow_id` |
| `alice_outbox_pending_total` | gauge | `domain` |
| `alice_outbox_dispatch_total` | counter | `domain`, `action_type`, `result` |
| `alice_gate_open_total` | gauge | `gate_type` |
| `alice_scheduler_fire_total` | counter | `workflow_id`, `result` |
| `alice_mcp_request_total` | counter | `domain`, `kind`, `result` |
| `alice_mcp_breaker_state` | gauge | `domain` |

## 6. 日志字段

所有结构化日志至少带：

- `trace_id`
- `event_id`
- `request_id`
- `task_id`
- `binding_id`
- `execution_id`
- `action_id`
- `approval_request_id`

没有这些主键，恢复排查成本会急剧上升。

## 7. notifier

`Notifier` 只消费结构化事件，不直接读业务表拼消息。

通知类型建议固定为：

- `waiting_human_required`
- `budget_alert`
- `outbox_dead_letter`
- `schedule_missed_window`
- `workflow_publish_required`
- `system_health_alert`

## 8. reconciler

### 8.1 `outbox-reconciler`

职责：

- 扫描 `dispatching` 超时动作
- 查询远端状态
- 补写 BUS 回执或重新入队

### 8.2 `schedule-fire-reconciler`

职责：

- 检查 missed window
- 补发 `ScheduleTriggered`
- 发现重复 fire 并做幂等拦截

### 8.3 `approval-expirer`

职责：

- 关闭超时 gate
- 写 `expired` 事件
- 更新 `HumanActionQueueView`

## 9. 恢复手册基线

TDR 先固定恢复动作，不展开运行手册 prose：

1. 事件日志损坏：
   - 停止入口
   - 定位损坏段
   - 使用最后快照 + 完整段重建
2. bbolt 物化层损坏：
   - 删除 `indexes/`
   - 从事件日志重建
3. outbox 大量 `dead`：
   - 查域级断路器与鉴权错误
   - 修复后人工触发 reconcile
4. scheduler missed window：
   - 先看 `fire_id`
   - 再决定是否补偿触发

## 10. 测试建议

必须覆盖：

- 指标标签稳定
- 恢复后 `HumanActionQueueView` 一致
- bbolt 删除后 admin 读接口仍能重建
- `dead letter` 可以人工重驱
- schedule missed window 会告警而不是 silent drop
