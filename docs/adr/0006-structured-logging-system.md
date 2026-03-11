# ADR 0006: 结构化日志系统设计

## 状态
Accepted

## 上下文

Alice系统需要详细的可观测性来追踪请求生命周期、调试问题和审计操作。当前系统使用Go标准库`slog`但缺乏：
1. 统一的日志格式和标准
2. 关键业务操作的详细追踪
3. 与事件日志的关联机制
4. 可配置的日志级别和输出

## 决策

### 1. 日志分层架构

```
┌─────────────────────────────────────────┐
│           Application Layer             │
│  (业务操作日志: request, task, decision) │
├─────────────────────────────────────────┤
│           Runtime Layer                 │
│  (系统运行日志: worker, scheduler, mcp)  │
├─────────────────────────────────────────┤
│           Infrastructure Layer          │
│  (基础设施日志: store, http, recovery)   │
└─────────────────────────────────────────┘
```

### 2. 日志级别定义

| 级别 | 用途 | 示例 |
|-----|------|------|
| DEBUG | 详细调试信息，生产环境可关闭 | 进入函数参数详情 |
| INFO | 正常业务流程记录 | 请求处理完成 |
| WARN | 需要注意但非错误 | 重试操作、降级处理 |
| ERROR | 操作失败但系统可继续 | 外部调用失败 |
| FATAL | 系统无法继续运行 | 存储不可用时 |

### 3. 结构化字段标准

所有日志必须包含：
- `timestamp`: ISO8601格式时间戳
- `level`: 日志级别
- `logger`: 组件名称（如"bus", "reception", "agent"）
- `message`: 可读消息

业务操作日志必须包含：
- `trace_id`: 分布式追踪ID
- `request_id`: 请求ID（如适用）
- `task_id`: 任务ID（如适用）
- `event_id`: 关联事件ID
- `operation`: 操作类型（如"route", "promote", "execute"）
- `duration_ms`: 操作耗时
- `component`: 组件名称

### 4. 关键操作日志点

#### 4.1 入口层 (Ingress)
- `event_received`: 接收到外部事件
- `event_normalized`: 事件标准化完成
- `route_resolved`: 路由解析结果
- `dedupe_checked`: 去重检查结果

#### 4.2 决策层 (Reception & Policy)
- `reception_started`: 开始意图识别
- `reception_completed`: 意图识别完成（含decision详情）
- `policy_evaluation`: 策略评估过程
- `promotion_decision`: 最终promotion决定

#### 4.3 执行层 (BUS & Agent)
- `request_opened`: 创建EphemeralRequest
- `task_promoted`: Request提升为Task
- `workflow_bound`: Workflow绑定完成
- `agent_dispatched`: Agent调度
- `agent_response_received`: Agent响应
- `step_execution_started`: Step开始执行
- `step_execution_completed`: Step完成
- `outbox_queued`: 外部操作排队
- `outbox_dispatched`: 外部操作派发
- `human_wait_created`: 等待人类输入
- `human_action_received`: 接收到人类操作

#### 4.4 存储层 (Store)
- `events_appended`: 事件批次追加
- `snapshot_created`: 快照创建
- `index_rebuild_started`: 索引重建开始
- `index_rebuild_completed`: 索引重建完成

#### 4.5 Worker层
- `worker_started`: Worker启动
- `worker_tick`: Worker执行周期
- `worker_error`: Worker错误
- `worker_stopped`: Worker停止

### 5. 日志输出配置

```yaml
logging:
  level: "info"                    # debug, info, warn, error, fatal
  format: "json"                   # json, text
  console: true                    # 输出到控制台
  file:                            # 文件输出（可选）
    path: "data/logs/alice.log"
    max_size_mb: 100
    max_backups: 5
    max_age_days: 30
    compress: true
  # 按组件覆盖级别
  components:
    "bus": "debug"
    "reception": "debug"
    "agent": "info"
```

### 6. 与事件日志的关系

- **事件日志 (Event Log)**: 系统真源，包含所有状态变更事件
- **应用日志 (Application Log)**: 可观测性日志，包含操作详情、耗时、调试信息

两者通过 `trace_id`, `request_id`, `task_id`, `event_id` 关联。

## 后果

### 正面
1. 完整的请求生命周期可见性
2. 便于调试和性能分析
3. 支持结构化日志收集（如ELK/Loki）
4. 组件级日志级别控制

### 负面
1. 需要维护日志字段标准
2. 额外的日志调用开销（可通过级别控制）

## 实现计划

1. 创建 `internal/platform/logger.go` - 日志基础设施
2. 创建 `internal/platform/telemetry.go` - 追踪和上下文
3. 更新 `internal/platform/config.go` - 添加日志配置
4. 在各层添加日志调用
5. 创建日志输出到文件的实现
