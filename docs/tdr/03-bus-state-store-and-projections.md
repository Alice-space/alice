# TDR 03: BUS、State Store、投影与恢复

## 1. 目标

本文定义 Alice v1 的真状态实现，包括：

- 命令执行与分片串行
- JSONL 事件日志
- 快照
- bbolt 物化索引
- dedupe / route index / outbox pending index
- 投影、重放、对账、dead letter

## 2. 真源与物化层

### 2.1 真源

唯一 durable commit 边界是事件日志 append。

意味着：

- 只有事件批次落盘后，状态推进才算成功
- `DurableTask` 当前状态只是从事件重放出来的视图
- `outbox`、dedupe、route index、投影全部属于物化层

### 2.2 物化层

v1 使用 `bbolt` 保存可重建索引：

- `requests_by_route`
- `tasks_by_route`
- `open_requests`
- `active_tasks`
- `pending_outbox`
- `dedupe_window`
- `schedule_sources`
- `approval_queue`
- `human_action_queue`
- `ops_views`

约束：

- 物化层损坏时，必须能删除并从事件重建
- 不允许“只写 bbolt、不写事件日志”

## 3. 事件日志

### 3.1 文件布局

```text
data/eventlog/
  000000000001.jsonl
  000000000002.jsonl
```

切段规则建议：

- 单段达到 `128MB` 切段
- 或单段超过 `1h` 滚动切段

### 3.2 行格式

每一行一个 `EventEnvelope` JSON：

```json
{
  "event_id": "evt_01...",
  "aggregate_kind": "request",
  "aggregate_id": "req_01...",
  "event_type": "PromotionAssessed",
  "sequence": 12,
  "global_hlc": "2026-03-10T18:00:00.123456Z#0001",
  "parent_event_id": "evt_...",
  "causation_id": "cmd_...",
  "correlation_id": "trace_...",
  "produced_at": "2026-03-10T18:00:00Z",
  "producer": "bus",
  "payload_schema_id": "event.promotion_assessed",
  "payload_version": "v1alpha1",
  "payload": {}
}
```

### 3.3 追加规则

单次命令执行可以产生一个事件批次：

- 批次必须整体 append 成功
- 批次内事件顺序固定
- append 成功前不得更新 bbolt 物化层

### 3.4 append 后可见性

为了避免“事件已提交，但路由索引还没追上”导致错路由，v1 把物化层分为两类：

| 类别 | 组成 | 一致性要求 |
| --- | --- | --- |
| commit-critical index | `requests_by_route`、`tasks_by_route`、`open_requests`、`active_tasks`、`pending_outbox`、`dedupe_window`、`schedule_sources` | 事件 append 成功后必须同步 apply，成功后才能对入口返回成功 |
| lagging projection | `approval_queue`、`human_action_queue`、`ops_views` | 允许短暂延迟，由后台 projector catch up |

强规则：

- `route-index-updater` 不是独立 worker；route key 变更必须由 append 同一 goroutine 同步应用
- 如果事件 append 成功，但 commit-critical index apply 失败，进程必须立刻标记 `ready=false` 并 fail-fast 退出
- 恢复启动后必须先重建 commit-critical index，再开放入口流量
- admin 只读视图允许基于 lagging projection 返回“最新已投影版本”，但路由与 dedupe 不允许 lag

## 4. 分片执行模型

### 4.1 aggregate key

默认按 aggregate key 串行：

- request 命令按 `request_id`
- task 命令按 `task_id`
- scheduler fire 注册按 `scheduled_task_id`

还没有 durable ID 时，按 route key 临时定位：

- `reply_to_event_id`
- `repo_ref + issue_ref`
- `repo_ref + pr_ref`
- `scheduled_task_id`
- `control_object_ref`
- `workflow_object_ref`
- `conversation_id + thread_id`
- `coalescing_key`

### 4.2 shard 算法

```go
shard = xxhash.Sum64String(aggregateKey) % uint64(shardCount)
```

不使用随机分发；否则同一个对象会被并发推进。

## 5. 快照

### 5.1 快照内容

快照是恢复加速器，不是独立真源。建议快照至少包含：

- 活跃 `EphemeralRequest`
- 活跃 `DurableTask`
- active `WorkflowBinding`
- active `StepExecution`
- open `ApprovalRequest`
- pending `OutboxRecord`
- active `ScheduledTask`
- 对应物化索引的 checkpoint

### 5.2 快照格式

- 文件格式：gzip 压缩 JSON
- 建议路径：`data/snapshots/<snapshot-id>.json.gz`
- 记录最后覆盖的 `global_hlc` 和每个 aggregate 的最后 `sequence`

### 5.3 生成时机

- 每 `N` 个事件批次
- 或每 `M` 分钟
- 或优雅停机前

## 6. 重放与恢复

### 6.1 启动恢复步骤

1. 找到最新快照
2. 载入快照到内存
3. 从快照 checkpoint 之后继续扫描 JSONL
4. 重建 commit-critical bbolt 索引
5. 对账 `pending_outbox`
6. 对账 `ScheduledTask` fire 状态
7. 重建 lagging projection，如 `HumanActionQueueView`

### 6.2 恢复后必须执行的 reconciler

- `outbox-reconciler`
- `schedule-fire-reconciler`
- `approval-expirer`
- `projection-rebuilder`

## 7. route index

route index 必须支持以下查询：

- 按 `task_id`
- 按 `request_id`
- 按 `reply_to_event_id`
- 按 `repo_ref + issue_ref`
- 按 `repo_ref + pr_ref`
- 按 `scheduled_task_id`
- 按 `control_object_ref`
- 按 `workflow_object_ref`
- 按 `conversation_id + thread_id`
- 按 `coalescing_key`

记录的不是“所有历史对象”，而是“当前可命中的活跃对象集合”。

### 7.1 路由一致性语义

route index 的语义不是“最终一致的缓存”，而是入口正确性的一部分：

- 新写入的 `reply_to_event_id`、repo 键、控制面键、`task_id/request_id` 必须在提交响应前可见
- request / task 终态时，对应 active route key 必须在同一提交批次里撤销
- `coalescing_key` 可以带时间桶，但时间桶计算必须基于事件 `ReceivedAt`，不能基于 projector 执行时间

因此，route index 的 reducer 必须只依赖事件 payload，不允许再去查“当前对象表”补充字段。

补充规则：

- `scheduled_task_id` 有两种查询语义：一类是“活跃控制面 request/task 路由”，另一类是“调度来源对象查找”。
- `ScheduleTriggered` 进入系统时，先查“调度来源对象索引”，而不是普通活跃 task route index。
- 调度来源对象索引保存的是 `ScheduledTask` 快照引用，不受 active task 生命周期影响。

## 8. dedupe

### 8.1 dedupe key

不同来源 dedupe key 规则：

- webhook：外部 delivery id
- IM / Web：平台 message id
- human action：action token + decision hash
- scheduler：`fire_id`

### 8.2 dedupe 策略

- dedupe 是时间窗内强去重
- 过期 dedupe 记录可被压缩
- dedupe 命中后返回“已处理”而不是 silent drop

## 9. `fire_id` 与调度补偿

调度触发实例必须有稳定 `fire_id`：

```text
fire_id = sha256(<scheduled_task_id> + ":" + <scheduled_for_window>)
```

其中 `scheduled_for_window` 使用 scheduler 计算出的窗口时间，而不是“实际触发时刻”。

这样才能在以下场景中安全补偿：

- 进程重启
- missed window 补触发
- webhook 回执迟到

## 10. outbox pending index

pending index 必须支持：

- 按 MCP 域拉取待执行动作
- 按 `next_attempt_at` 排序
- 按 `action_id` 更新状态
- 按 `idempotency_key` 查重

`pending_outbox` 只是一份队列视图，真正状态以事件重放为准。

## 11. dead letter

以下情况进入 dead letter：

- payload schema 永久非法
- 引用对象已终态且策略不允许重开
- 超过最大重试且不是暂时性错误
- 无法完成人工修复自动重试

dead letter 记录必须至少包含：

- `event_id`
- 原始来源
- 失败原因码
- 人工处理建议

## 12. 背压

### 12.1 入口背压

当以下任一指标超过阈值时，对低优先级流量启用背压：

- shard 队列长度
- pending outbox 深度
- dead letter 增长率
- MCP 域断路器打开

### 12.2 背压行为

- 公开信息查询：可拒绝或延迟
- 控制面写请求：可接受事件，但进入等待
- 已有关联 task 的人类回流：不得直接丢弃

## 13. 接口建议

```go
type EventStore interface {
    Append(ctx context.Context, batch []domain.EventEnvelope) error
    Replay(ctx context.Context, fromHLC string, fn func(domain.EventEnvelope) error) error
}

type SnapshotStore interface {
    LoadLatest(ctx context.Context) (*Snapshot, error)
    Save(ctx context.Context, snapshot *Snapshot) error
}

type IndexStore interface {
    GetRouteTarget(ctx context.Context, key RouteLookup) (*RouteTarget, error)
    ApplyEvents(ctx context.Context, events []domain.EventEnvelope) error
    Rebuild(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}
```

建议把接口落成两层：

```go
type CriticalIndexStore interface {
    ApplyCritical(ctx context.Context, events []domain.EventEnvelope) error
    RebuildCritical(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}

type ProjectionStore interface {
    ApplyLagging(ctx context.Context, events []domain.EventEnvelope) error
    RebuildLagging(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}
```

## 14. 测试建议

必须覆盖：

- 事件批次 append 成功/失败的原子性
- 快照恢复后 route index 一致
- bbolt 删除后可完整重建
- scheduler `fire_id` 补偿幂等
- 同一 aggregate 命令不会并发推进
- outbox pending index 与事件日志一致
