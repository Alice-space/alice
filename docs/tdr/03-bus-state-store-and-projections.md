# TDR 03: BUS、State Store 与投影

## 1. 目标

本文定义系统真状态的实现方式，包括串行执行器、事件日志、快照、去重、投影和背压。

建议目录：

- `internal/bus/bus.go`
- `internal/bus/executor.go`
- `internal/bus/handler.go`
- `internal/store/eventlog.go`
- `internal/store/snapshot.go`
- `internal/store/projection.go`
- `internal/store/dedupe.go`

## 2. 责任边界

### 2.1 BUS 负责

- 接收命令
- 基于 `task_id` 串行执行
- 调用命令处理器生成事件
- 以单次 append 方式落一组原子事件批次
- 在事件提交后更新投影和可重建索引
- 发布后续内部任务，例如 outbox dispatch、notifier

### 2.2 State Store 负责

- 持久化事件
- 持久化快照、投影、确认对象和可重建索引
- 支持恢复和对账

BUS 不直接操作外部平台；State Store 不做领域判断。

## 3. 执行模型

### 3.1 分片策略

同一 `task_id` 必须进入同一个 shard。

```go
shard = fnv32(taskID) % shardCount
```

如果命令尚未生成 `task_id`，例如入口建任务阶段，使用：

- 已知外部 issue 时：`external_issue_binding_key`
- 其他情况：`request_id`

一旦 `task_id` 生成，后续命令必须全部按 `task_id` 路由。

### 3.2 命令执行步骤

```text
receive command
  -> locate shard
  -> load current task snapshot/projection
  -> validate task_version and state
  -> run handler to generate committed event batch
  -> append event batch to log
  -> apply reducers
  -> refresh projections / outbox queue / dedupe index
  -> emit post-commit hooks
```

严格要求：

- “先落日志，后更新投影”
- 任一阶段失败必须返回错误，不得伪造成功

### 3.3 单次提交边界

第一版只有一个权威耐久化提交边界：事件日志 append。

命令处理器必须在内存中先产出完整事件批次，再一次性写入日志。一个批次里可以同时包含：

- 主状态事件，例如 `TaskCreated`、`PlanPublished`
- 外部事件受理事件，例如 `ExternalEventAccepted`
- 副作用意图事件，例如 `OutboxActionCreated`
- 确认与调度事件，例如 `ConfirmationIssued`、`ScheduleFireRegistered`

由此派生的：

- `outbox` 队列
- 外部事件 dedupe 索引
- 只读投影

都属于可重放重建的物化结果，不允许成为第二个独立耐久化真源。

## 4. 事件日志设计

### 4.1 文件布局

建议磁盘布局：

```text
data/
  eventlog/
    2026-03-08-0001.jsonl
  snapshot/
    latest.json
    task/
  projection/
    task_view.jsonl
    ops_view.jsonl
    audit_view.jsonl
  index/
    outbox_queue.jsonl
    external_event_index.jsonl
```

其中 `index/` 下对象只是加速查询和恢复的物化索引，损坏后必须允许由事件日志重放重建。

### 4.2 写入策略

- 单 writer goroutine 持有事件日志文件句柄
- shard worker 通过有界 channel 把 append 请求发给 writer
- writer 追加成功后返回 commit ack

这样做的原因：

- 避免多 goroutine 抢文件锁
- 容易实现顺序 checkpoint

### 4.3 事件格式

每行一个 `EventEnvelope` JSON。

要求字段：

- `event_id`
- `event_type`
- `task_id`
- `task_version`
- `global_hlc`
- `parent_event_id`
- `causation_id`
- `origin_actor`
- `occurred_at`
- `payload`

## 5. 快照设计

### 5.1 触发条件

满足任一条件即触发：

- 自上次快照起追加了 `N` 条事件
- 距离上次快照超过 `M` 分钟

推荐默认值：

- `N = 1000`
- `M = 5m`

### 5.2 快照内容

快照至少包含：

- 全量 `Task` 聚合状态
- 活跃 `AuditRequest`
- 活跃 `Confirmation`
- 活跃 `OutboxRecord`
- 活跃 `ScheduleFire`
- 最近 checkpoint 信息

### 5.3 快照写入

快照采用“写临时文件再 rename”的原子替换模式，禁止覆盖半写入文件。

## 6. 投影模型

建议维护三类投影：

| 投影 | 用途 |
| --- | --- |
| `TaskView` | 按任务展示状态、issue、PR、等待原因 |
| `OpsReadModel` | 展示健康、预算、队列积压、MCP 状态 |
| `AuditView` | 展示审核轮次、席位、租约、结论 |

投影更新原则：

- 由 reducer 派生
- 更新失败不回滚已提交事件
- 失败后记录恢复任务，由 reconciler 重建投影

## 7. 去重与外部事件索引

外部事件 dedupe 必须以事件日志为真源，索引本身只是物化结果。

做法：

- 外部事件首次受理时，命令处理器在同一个事件批次中写入 `ExternalEventAccepted` 或 `ExternalEventRejected`
- `external_event_index` 由 replay 或 reducer 从这些事件派生
- 入口快速路径可以先查索引；索引缺失时仍必须以日志重放结果为准恢复

```go
type ExternalEventRecord struct {
    ExternalEventID string
    Provider        string
    ReceivedAt      time.Time
    TaskID          string
    Status          string
    ExpiresAt       time.Time
}
```

策略：

- 验签失败事件不写 dedupe 成功态
- 重复事件命中 dedupe 时直接返回已处理
- dedupe 记录保留至少 7 天
- 即使 `external_event_index` 尚未来得及落盘，只要 `ExternalEventAccepted` 已在日志中，恢复后也必须重新识别为已处理

## 8. 背压

背压由两个指标共同触发：

- writer queue 条数 / 字节数
- shard queue 长度

背压等级：

- `normal`
- `degraded`
- `shed_low_priority`
- `critical`

建议行为：

| 等级 | 行为 |
| --- | --- |
| `degraded` | 打告警，保留全部流量 |
| `shed_low_priority` | 拒绝低优先级同步请求和 issue 镜像补建 |
| `critical` | 仅保留确认、取消、恢复、健康请求 |

## 9. 恢复算法

启动恢复分四步：

1. 读取最新快照
2. 重放快照之后的事件日志
3. 重建或校验投影、`outbox_queue` 和 `external_event_index`
4. 扫描长时间 `pending` 的 outbox / audit / eval objects

如果投影损坏：

- 允许删除投影并完全由事件重放重建
- 禁止修改事件日志来“修投影”

## 10. 关键接口

```go
type CommandBus interface {
    Submit(ctx context.Context, cmd domain.Command) error
}

type EventLog interface {
    Append(ctx context.Context, events []domain.EventEnvelope) error
    Replay(ctx context.Context, after Checkpoint, fn func(domain.EventEnvelope) error) error
}

type SnapshotStore interface {
    LoadLatest(ctx context.Context) (*domain.Snapshot, error)
    Save(ctx context.Context, snap *domain.Snapshot) error
}

type ProjectionStore interface {
    UpsertTaskView(ctx context.Context, view domain.TaskView) error
    UpsertOpsView(ctx context.Context, view domain.OpsReadModel) error
}
```

## 11. 必要失败处理

- 事件日志 append 失败：命令整体失败，不更新投影
- 投影或索引更新失败：事件已提交，记录恢复任务并告警；恢复必须依赖 replay 重建
- 快照失败：不中断主链路，但上报告警
- 不允许把 `outbox` 或 dedupe 设计为独立成功才算提交；它们只能是日志提交后的可恢复派生物

## 12. 最小测试矩阵

- 单任务命令串行执行测试
- 多任务跨 shard 并行测试
- append 成功但 projection 失败恢复测试
- snapshot + replay 后 task 状态一致测试
- 背压等级切换测试
- 重复 webhook 命中 dedupe 测试
