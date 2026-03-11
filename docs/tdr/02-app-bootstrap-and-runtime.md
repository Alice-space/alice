# TDR 02: 启动装配与运行时

## 1. 目标

本文定义 `cmd/alice` 的装配、启动、停机和 worker 生命周期，确保系统在：

- 启动恢复
- 接收新流量
- 派发 workflow step
- 派发 outbox
- 执行 scheduler / reconciler

这些环节中没有“半启动”或“未恢复先接单”的窗口。

建议目录：

- `cmd/alice/main.go`
- `internal/cli/root.go`
- `internal/app/app.go`
- `internal/app/bootstrap.go`
- `internal/app/runtime.go`
- `internal/app/workers.go`

## 2. App 结构

```go
type App struct {
    Config          *Config
    Logger          *slog.Logger
    Clock           clock.Clock
    IDGen           IDGenerator
    HTTPServer      *http.Server
    Store           *store.Store
    Bus             *bus.Runtime
    Policy          *policy.Engine
    WorkflowRuntime *workflow.Runtime
    AgentRegistry   *agent.Registry
    MCPRegistry     *mcp.Registry
    Ops             *ops.Manager
    Workers         []Worker
}
```

`App` 只负责装配，不承载领域规则。

## 3. 配置模型

### 3.1 配置来源

v1 采用：

- YAML 配置文件
- 环境变量覆盖
- 极少量命令行 flag 覆盖

不引入动态配置中心。

### 3.2 配置分组

```go
type Config struct {
    HTTP       HTTPConfig
    Storage    StorageConfig
    Runtime    RuntimeConfig
    Promotion  PromotionConfig
    Workflow   WorkflowConfig
    MCP        MCPConfig
    Scheduler  SchedulerConfig
    Ops        OpsConfig
    Auth       AuthConfig
    CLI        CLIConfig
    Logging    LoggingConfig
}
```

关键字段：

- `HTTP.ListenAddr`
- `Storage.RootDir`
- `Storage.SnapshotInterval`
- `Runtime.ShardCount`
- `Runtime.OutboxWorkers`
- `Promotion.MinConfidence`
- `Workflow.ManifestRoots`
- `MCP.Domains[*].BaseURL`
- `Scheduler.PollInterval`
- `Ops.MetricsEnabled`
- `Logging.Level`
- `Logging.Components`

client mode 额外使用：

- `CLI.ServerBaseURL`
- `CLI.DefaultOutput`
- `CLI.Timeout`
- `CLI.WaitTimeout`
- `Ops.AdminEventInjectionEnabled`
- `Ops.AdminScheduleFireReplayEnabled`

### 3.3 日志配置

```go
type LoggingConfig struct {
    Level       string                     // debug, info, warn, error, fatal
    Format      string                     // json, text
    Console     bool                       // 输出到控制台
    File        *FileLogConfig             // 文件输出配置（可选）
    Components  map[string]string          // 按组件覆盖级别
}

type FileLogConfig struct {
    Path        string
    MaxSizeMB   int
    MaxBackups  int
    MaxAgeDays  int
    Compress    bool
}
```

日志级别优先级：`Components[component]` > `Level` > 默认值(`info`)

## 4. 启动顺序

启动必须严格分阶段：

1. 读取配置并做 schema 校验
2. **初始化日志系统**（根据 LoggingConfig 配置级别和输出）
3. 初始化 clock、ULID 生成器
4. 打开存储目录与文件句柄
5. 初始化事件日志、快照和 bbolt 物化层
6. 载入最新快照并重放事件日志
7. 构建 `bus`、`policy`、`workflow runtime`、`mcp registry`
8. 执行恢复任务：
   - 重建 outbox pending index
   - 重建 route index
   - 对账 inflight outbox
   - 重建 human action queue
9. 注册并启动后台 workers
10. 最后再开放 HTTP 监听
11. 标记 `/readyz=true`

原因：

- 先恢复，再接流量
- 避免 scheduler 或 webhook 在系统未 ready 时推进新状态
- **日志系统尽早初始化，确保后续步骤都有日志记录**

CLI client mode 不参与上述启动序列。它只解析配置、构建 HTTP client，然后通过远端 server 执行动作。

## 5. 停机顺序

优雅停机必须按反向顺序：

1. 标记 `/readyz=false`
2. 停止接收新 webhook / admin 写请求
3. 停止 scheduler、step dispatcher、outbox dispatcher
4. 等待正在执行的命令批次结束
5. 刷新投影和必要快照
6. 关闭 HTTP server
7. 关闭 bbolt 与日志句柄

停止过程不要求“杀死所有外部动作”，但必须保证：

- 不再启动新的动作
- 当前 inflight 状态可由恢复流程继续

## 6. Worker 模型

### 6.1 固定 worker 列表

v1 建议固定以下 worker：

- `request-expirer`
- `step-ready-dispatcher`
- `outbox-dispatcher`
- `approval-expirer`
- `scheduler`
- `schedule-fire-reconciler`
- `outbox-reconciler`
- `projection-rebuilder`
- `notifier`

其中：

- route index、dedupe、pending outbox 这些 commit-critical 索引不作为后台 worker，它们必须在事件 append 后同步应用
- `projection-rebuilder` 只负责 lagging view 或灾后重建，不承担路由正确性

这些 worker 都是平台内核的一部分，不是业务 workflow step。

### 6.2 Worker 接口

```go
type Worker interface {
    Name() string
    Start(ctx context.Context) error
}
```

要求：

- 每个 worker 都必须可独立关闭
- 每个 worker 都必须暴露健康状态
- 每个 worker 的 panic 都必须被捕获并转为日志 + 进程级 fail-fast 策略判定

## 7. 运行时并发模型

### 7.1 命令执行

- 入口适配层只提交命令，不直接改状态
- `bus` 按 aggregate key 做分片串行执行
- 同一 aggregate 的命令绝不并发写

### 7.2 MCP 调用

- MCP 调用在 `outbox-dispatcher` 内并发执行
- 并发度按 MCP 域单独限制
- 断路器和限流器都挂在 MCP 域边界

### 7.3 Step 执行

- `step-ready-dispatcher` 只做“把 ready step 交给合适执行器”
- 真正的 step 运行要带 lease
- 同一 task 同一时刻只允许一个 active step execution

## 8. 健康检查与管理接口

### 8.1 基础接口

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`

### 8.2 Admin 只写接口

建议保留以下内部管理接口：

- `POST /v1/admin/submit/events`
- `POST /v1/admin/submit/fires`
- `POST /v1/admin/resolve/approval`
- `POST /v1/admin/resolve/wait`
- `POST /v1/admin/replay/from/{hlc}`
- `POST /v1/admin/reconcile/outbox`
- `POST /v1/admin/reconcile/schedules`
- `POST /v1/admin/rebuild/indexes`
- `POST /v1/admin/tasks/{task_id}/cancel`
- `POST /v1/admin/deadletters/{id}/redrive`

这些接口都必须：

- 经过单独鉴权
- 留 `ExternalEvent` 或 admin audit 日志
- 不能绕过事件日志直接写 durable 状态

### 8.3 CLI client mode

`cmd/alice` 的 client 子命令模式建议按下列模块组织：

```go
type CLIApp struct {
    Config     *Config
    Logger     *slog.Logger
    HTTPClient *http.Client
    Renderer   *cli.Renderer
}
```

```go
type CLIConfig struct {
    ServerBaseURL string
    DefaultOutput string
    Timeout       time.Duration
    WaitTimeout   time.Duration
    TokenEnvVar   string
}
```

约束：

- client mode 不能打开 store、快照或 bbolt 文件
- client mode 只能依赖配置、鉴权、HTTP client 与输出渲染
- `serve` 和 client mode 共用一份配置模型，但运行路径严格分离

## 9. panic 与 fail-fast 策略

默认原则：

- 核心状态推进路径 panic：记录 fatal 日志并退出进程
- 单个 MCP 调用 panic：隔离在 worker 内，写错误并按 outbox 失败处理
- 单个 notifier panic：只影响通知 worker，不影响主状态

这里不追求“永不退出”，而追求“退出后可以靠恢复流程重建”。

## 10. 运行目录布局

```text
data/
  eventlog/
  snapshots/
  indexes/
  deadletters/
  blobs/
  logs/               # 应用日志目录
    alice.log
    alice-2026-03-10.log
```

建议说明：

- `eventlog/`：JSONL 段文件（系统真源）
- `snapshots/`：快照文件
- `indexes/`：bbolt 物化索引
- `deadletters/`：无法继续处理的事件引用
- `blobs/`：大 payload、artifact、原始 webhook 体
- `logs/`：**结构化应用日志**，与事件日志分离但关联

## 12. 日志系统规范

### 12.1 日志分层

| 层级 | 组件示例 | 用途 |
|-----|---------|------|
| Application | reception, agent | 业务操作追踪 |
| Runtime | bus, worker, scheduler | 系统运行时状态 |
| Infrastructure | store, ingress | 基础设施操作 |

### 12.2 标准日志字段

所有日志必须包含：
- `timestamp`: ISO8601 (e.g., `2026-03-10T18:00:00.123Z`)
- `level`: DEBUG/INFO/WARN/ERROR/FATAL
- `logger`: 组件名称
- `msg`: 可读消息

业务日志额外包含：
- `trace_id`: 追踪ID
- `request_id`: 请求ID
- `task_id`: 任务ID
- `event_id`: 事件ID
- `operation`: 操作类型
- `duration_ms`: 耗时

### 12.3 关键日志点

| 阶段 | 日志键 | 级别 | 说明 |
|-----|-------|-----|------|
| 入口 | `event_received` | INFO | 接收到外部事件 |
| 路由 | `route_resolved` | DEBUG | 路由解析结果 |
| 决策 | `promotion_decision` | INFO | Promotion决定 |
| 执行 | `agent_dispatched` | INFO | Agent调用 |
| 执行 | `agent_response` | INFO | Agent响应 |
| 存储 | `events_appended` | DEBUG | 事件追加 |
| 错误 | `operation_failed` | ERROR | 操作失败 |

## 11. 测试建议

启动装配至少覆盖：

- 空目录冷启动
- 快照 + 日志恢复启动
- `readyz` 在恢复完成前为 false
- outbox inflight 恢复
- scheduler 在 fake clock 下的启动/停机测试
- worker panic 不会 silently swallow
