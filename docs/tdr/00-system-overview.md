# TDR 00: 系统总览与模块划分

## 1. 目标

本文把 `draft.md` 与 `docs/cdr/*` 收敛为实现导向的系统总览，回答三件事：

- 进程级和包级模块应如何切分
- 各模块之间的调用方向与依赖边界是什么
- 核心运行时从启动到收尾的主链路是什么

本文是后续详细设计的导航文档，不重复定义所有细节字段。

## 2. 进程拓扑

第一版保持“一个核心运行时 + 多个独立 MCP”的结构。

```text
cmd/alice
  ├─ ingress adapters
  ├─ BUS / State Store / Workflow
  ├─ Agent runtime
  ├─ scheduler / reconciler / notifier
  └─ read model / admin api

cmd/mcp-control
cmd/mcp-github
cmd/mcp-gitlab
cmd/mcp-cluster
```

设计要求：

- `cmd/alice` 是唯一真实状态持有者
- MCP 进程不持有业务真状态，只执行受控外部动作
- 外部平台状态变化必须回流 BUS 后才算完成

## 3. 包级模块映射

| 目录 | 责任 | 不能做什么 |
| --- | --- | --- |
| `cmd/alice` | 参数解析、配置装配、依赖初始化、服务启动 | 不承载业务规则 |
| `internal/app` | 统一创建 bus、store、workflow、http server、worker | 不直接处理领域状态 |
| `internal/domain` | 定义 `Task`、事件、命令、状态、校验函数 | 不依赖 IO 和基础设施 |
| `internal/bus` | 串行执行同一 `task_id` 的命令并推进状态 | 不直接调用外部系统 |
| `internal/store` | 事件日志、快照、投影、`outbox`/去重物化索引、确认对象持久化 | 不做领域决策 |
| `internal/ingress` | Webhook 接入、消息接入、格式标准化、签名校验 | 不直接修改 Task 状态 |
| `internal/agent` | 与具体 LLM/Agent 运行时集成，返回结构化结果 | 不绕过 BUS 改状态 |
| `internal/workflow` | 计划、审核、编码、评测、合并的流程编排与命令装配 | 不自己持久化状态 |
| `internal/mcp` | 封装 MCP 客户端、幂等、限流、断路器与域适配 | 不决定是否放行副作用 |
| `internal/ops` | `OpsReadModel`、通知、巡检、scheduler、健康检查 | 不成为业务真源 |
| `internal/platform` | 配置、日志、时间、ID、鉴权、HTTP/gRPC 基础能力 | 不嵌入业务规则 |

## 4. 依赖方向

统一依赖方向如下：

```text
cmd -> app
app -> platform + store + bus + ingress + workflow + mcp + ops
workflow -> domain + bus + mcp + agent
ingress -> domain + bus + store + platform
bus -> domain + store
store -> domain
agent -> domain + platform
ops -> domain + store + platform
```

约束：

- `domain` 只能被依赖，不能反向依赖其他模块
- `bus` 只负责任务串行推进和状态落地，不直接访问 GitHub / GitLab / Cluster
- 所有外部访问必须经过 `mcp` 或明确的入口适配层

## 5. 运行时主链路

### 5.1 入口链路

```text
External Input
  -> ingress normalize
  -> bus submit command
  -> state store persist event
  -> workflow dispatch
  -> outbox / notifier / read model update
```

### 5.2 代码任务主链路

```text
task_created
  -> issue_created
  -> planning
  -> plan_audit
  -> coding
  -> evaluation? 
  -> code_audit
  -> merge_approval_pending?
  -> merging
  -> merged
  -> closed
```

### 5.3 恢复链路

```text
startup
  -> load config
  -> open event log
  -> load latest snapshot
  -> replay events
  -> rebuild projections
  -> scan pending outbox / audit / eval jobs
  -> start reconciler and ingress servers
```

## 6. 模块级职责分配

### 6.1 `internal/app`

- 创建核心依赖图
- 启动 HTTP server、webhook server、scheduler、worker loops
- 管理优雅停机顺序

### 6.2 `internal/domain`

- 定义领域枚举、结构体和校验器
- 定义命令与事件类型
- 定义 reducer 可依赖的不变式

### 6.3 `internal/bus`

- 以 `task_id` 为维度串行执行命令
- 负责命令到事件的状态推进
- 调度 projector、outbox dispatcher、notifier

### 6.4 `internal/store`

- 提供事件追加写、快照读写、只读投影更新
- 物化 dedupe 索引、`outbox` 队列、`Confirmation`、`AuditRequest`
- 支持启动恢复与巡检对账

### 6.5 `internal/ingress`

- 提供 Feishu / Web / GitHub / GitLab 适配器
- 把外部数据标准化为 `UserRequest` 或 `ExternalEvent`
- 做签名校验、去重预检查和 actor 识别

### 6.6 `internal/workflow`

- 管理入口分流
- 管理计划、审核、编码、评测和合并阶段的推进规则
- 输出给 `bus` 的领域命令

### 6.7 `internal/mcp`

- 定义统一 `Client` 接口
- 管理域级客户端：GitHub、GitLab、Cluster、Control
- 执行限流、超时、重试、断路器和幂等透传

### 6.8 `internal/ops`

- 构建 `OpsReadModel`
- 对接飞书卡片、通知推送、人工确认
- 做健康检查、巡检、死信与恢复建议输出

## 7. 建议实现顺序

为降低返工，建议代码按以下顺序落地：

1. `internal/domain`
2. `internal/store`
3. `internal/bus`
4. `internal/app`
5. `internal/ingress`
6. `internal/mcp`
7. `internal/workflow`
8. `internal/ops`
9. `internal/agent`

理由：

- 先固定状态与持久化，后续模块才不会各说各话
- 先有 BUS 和 store，工作流才能落到真实状态上
- 先实现通用外部动作管道，后实现 Planner / Coding / Audit 等 Agent

## 8. 实现检查清单

编码前应确认以下问题已经被后续 TDR 或 ADR 明确：

- 状态名与事件名是否已经冻结
- 同一 `task_id` 的串行执行器是否有清晰实现
- 事件日志、快照、`outbox`/dedupe 物化索引、确认对象是否都有清晰格式
- 所有外部副作用是否都能通过 `outbox` 恢复
- 审核和评测是否都绑定明确版本
- 人工确认与取消是否有统一回流模型

若其中任一点不明确，不应直接开始实现工作流代码。
