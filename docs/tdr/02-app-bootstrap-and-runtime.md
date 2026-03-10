# TDR 02: 启动装配与运行时

## 1. 目标

本文定义 `cmd/alice` 与 `internal/app` 的实现边界，确保核心运行时可启动、可恢复、可优雅停机。

建议目录：

- `cmd/alice/main.go`
- `internal/app/app.go`
- `internal/app/bootstrap.go`
- `internal/app/runtime.go`

## 2. App 结构

```go
type App struct {
    Config        *config.Config
    Logger        *zap.Logger
    Clock         clock.Clock
    IDGen         id.Generator
    HTTPServer    *http.Server
    GRPCServer    *grpc.Server
    Bus           *bus.Bus
    Store         *store.Store
    Ingress       *ingress.Router
    Workflow      *workflow.Manager
    MCPRegistry   *mcp.Registry
    Ops           *ops.Manager
    Workers       []Worker
    ShutdownGroup *shutdown.Group
}
```

`App` 只做装配，不持有业务逻辑。

## 3. 启动顺序

启动必须严格分阶段：

1. 读取配置并校验
2. 初始化日志、时钟、ID 生成器
3. 打开持久化目录与文件句柄
4. 初始化 `store`
5. 加载快照并回放事件
6. 初始化 `bus`、`workflow`、`mcp`、`ops`
7. 运行恢复对账
8. 启动 outbox worker、audit lease checker、scheduler、reconciler
9. 最后才对外开放 HTTP / webhook / admin API

原因：

- 先恢复状态，再接收新事件
- 避免系统还没 ready 就接受 webhook

## 4. 停机顺序

优雅停机顺序必须与启动反向：

1. 拒绝新流量
2. 停止 scheduler 产生新任务
3. 停止 ingress 消费
4. 等待 in-flight command 执行完成或超时
5. flush event log / snapshot checkpoints
6. 停止 worker 与 notifier
7. 关闭 MCP 连接与服务器

推荐总停机窗口：`30s`。超过窗口时输出未完成任务摘要。

## 5. Worker 模型

核心运行时至少包含以下后台 worker：

| Worker | 周期/触发 | 职责 |
| --- | --- | --- |
| `outbox_dispatcher` | 常驻 | 消费待执行 `outbox` |
| `audit_lease_checker` | 5s | 处理审核租约超时 |
| `eval_job_reconciler` | 30s | 对账集群作业状态 |
| `issue_sync_reconciler` | 1m | 重试 issue 镜像建立 |
| `snapshotter` | N 条事件或 M 分钟 | 生成快照 |
| `scheduler_runner` | 常驻 | 触发定时任务 |
| `ops_projector` | 事件触发 | 刷新只读视图 |
| `health_reporter` | 30s | 上报 MCP、队列、磁盘状态 |

这些 worker 都应通过统一接口注册：

```go
type Worker interface {
    Name() string
    Start(ctx context.Context) error
}
```

## 6. 配置模型

建议配置结构：

```go
type Config struct {
    Runtime     RuntimeConfig
    HTTP        HTTPConfig
    Store       StoreConfig
    Bus         BusConfig
    Ingress     IngressConfig
    MCP         MCPConfig
    Audit       AuditConfig
    Budget      BudgetConfig
    Notify      NotifyConfig
    Scheduler   SchedulerConfig
}
```

关键配置项：

- `Store.EventLogDir`
- `Store.SnapshotDir`
- `Store.SnapshotEveryNEvents`
- `Store.SnapshotEveryDuration`
- `Bus.ShardCount`
- `Bus.LowPriorityQueueLimit`
- `Bus.HighPriorityQueueLimit`
- `Audit.DefaultDeadline`
- `Audit.LeaseDuration`
- `Budget.DefaultHardLimit`
- `MCP.Domains[*].Endpoint`
- `MCP.Domains[*].Timeout`
- `Notify.FeishuWebhook`

## 7. Ready / Live 语义

### 7.1 Liveness

进程活着即可返回 200，但应包含：

- goroutine 泄漏阈值
- event log writer 心跳
- 主循环 panic 保护状态

### 7.2 Readiness

只有以下条件满足才返回 ready：

- 快照回放完成
- 事件日志可写
- `bus` shard 全部启动
- 至少必需的 MCP 域已连接
- 初始 reconciler 已完成一轮

## 8. panic 与错误策略

规则：

- 单个 webhook handler panic 不得导致进程退出
- 单个 worker panic 由 supervisor 拉起，并记录严重告警
- event log writer panic 应触发进程 fail-fast，因为会破坏状态真源

建议：

- 入口 goroutine 使用 recover
- 写路径关键 goroutine 使用 fail-fast + crash-only restart

## 9. 依赖注入要求

第一版不引入重量级 DI 框架，使用显式构造函数：

```go
func NewStore(cfg StoreConfig, deps StoreDeps) (*Store, error)
func NewBus(cfg BusConfig, deps BusDeps) (*Bus, error)
func NewWorkflow(cfg WorkflowConfig, deps WorkflowDeps) (*Manager, error)
func NewApp(cfg *Config) (*App, error)
```

要求：

- 构造函数不做后台启动
- `Start(ctx)` 与 `Close(ctx)` 明确分离
- 便于测试中构造半成品依赖

## 10. 最小测试矩阵

- 冷启动恢复测试
- snapshot + replay 一致性测试
- readiness 在恢复前拒绝流量测试
- shutdown 时 in-flight 命令 drain 测试
- 必需 MCP 不可用时启动失败测试
