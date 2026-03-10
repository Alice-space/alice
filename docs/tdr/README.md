# Alice 技术设计报告（TDR）

## 1. 文档定位

`docs/tdr/` 承接 [draft.md](../draft.md) 与 [cdr/](../cdr/README.md)，目标不是重复概念设计，而是把已经收敛的边界继续下沉成可编码的实现方案。

这一版 TDR 明确以最新 CDR 为准，不再沿用旧的：

- “所有入口先建 `Task`”
- `RouteDecision`
- `sync_direct` / `sync_mcp_read` / `async_issue`
- `planning -> plan_audit -> coding -> code_audit`

TDR 在本仓库里冻结三类内容：

- v1 的实现边界：哪些对象、接口、状态和文件结构必须落地
- v1 的技术基线：主语言、进程拓扑、存储、协议和依赖库
- v1 的实现顺序：哪些模块先写、哪些 ADR 仍需补齐

TDR 不冻结：

- prompt 文本
- 具体模型提供商 SDK
- 某个 workflow manifest 的业务内容细节
- v2 的分布式拆分方案

## 2. 输入基线

本目录以以下文档为唯一上游基线：

- [draft.md](../draft.md)
- [README.md](../cdr/README.md)
- [entry_processing_detail.md](../cdr/entry_processing_detail.md)
- [issue_workflow_detail.md](../cdr/issue_workflow_detail.md)
- [control_plane_workflow_detail.md](../cdr/control_plane_workflow_detail.md)
- [typical_cases.md](../cdr/typical_cases.md)
- [adr/](../adr/README.md)

如果 TDR 与 CDR 冲突，以 CDR 为准；如果实现需要改变 CDR 边界，应先补 ADR，再回改 CDR/TDR。

## 3. v1 冻结边界

这版 TDR 默认冻结以下边界：

- 所有输入先标准化为 `ExternalEvent`
- 入口先命中已有对象，否则创建 `EphemeralRequest`
- `Reception` 只产出 `PromotionDecision`，不能自己裁决
- `PromotionDecision` 命中硬规则后，才 promote 成 `DurableTask`
- `DurableTask` 顶层状态只保留 `NewTask / Active / WaitingHuman / Succeeded / Failed / Cancelled`
- 业务步骤来自 workflow manifest，而不是 Go 平台级状态枚举
- `WorkflowBinding` 记录不可原地改写；若允许重规划，只能追加新 binding 记录
- 所有外部副作用都走 `outbox + MCP`
- 控制面对象必须使用 `scheduled_task_id / control_object_ref / workflow_object_ref`
- 人类补充、审批、预算恢复、取消都是新的 `ExternalEvent`

## 4. 文档目录

推荐按下面顺序阅读和实现：

1. [00-system-overview.md](./00-system-overview.md)
   作用：进程拓扑、模块边界、依赖方向、代码目录映射、实现顺序。
2. [01-domain-model.md](./01-domain-model.md)
   作用：核心对象、顶层状态、事件封套、审计骨架、不变式。
3. [02-app-bootstrap-and-runtime.md](./02-app-bootstrap-and-runtime.md)
   作用：`cmd/alice` 装配、启动/停机、worker 生命周期、配置与健康检查。
4. [03-bus-state-store-and-projections.md](./03-bus-state-store-and-projections.md)
   作用：事件日志、快照、可重建索引、分片执行、背压、重放与恢复。
5. [04-entry-routing-and-promotion.md](./04-entry-routing-and-promotion.md)
   作用：多入口接入、路由键优先级、命中算法、`PromotionDecision`、request 级审计。
6. [05-workflow-runtime-and-agent-coordination.md](./05-workflow-runtime-and-agent-coordination.md)
   作用：manifest 加载、唯一候选裁决、`WorkflowBinding`、`StepExecution`、gate、`AgentDispatch`。
7. [06-delivery-and-research-workflows.md](./06-delivery-and-research-workflows.md)
   作用：`issue-delivery` 与 `research-exploration` 的 v1 step、artifact、回退与预算规则。
8. [07-external-effects-control-plane-and-scheduler.md](./07-external-effects-control-plane-and-scheduler.md)
   作用：`outbox + MCP`、控制面 workflow、`ScheduledTask`、`ScheduleTriggered`、发布与不漂移规则。
9. [08-ops-scheduler-and-recovery.md](./08-ops-scheduler-and-recovery.md)
   作用：只读投影、运维接口、告警、巡检、reconciler、人工待办和恢复工具。
10. [09-cli-and-kernel-test-surface.md](./09-cli-and-kernel-test-surface.md)
   作用：`cmd/alice` 的 CLI client mode、测试入口、只读运维面和受控恢复命令。

## 5. 技术基线

### 5.1 主实现语言与传输

- 主实现语言：Go `1.26`
- 核心运行时：单主二进制 `cmd/alice`
- MCP：同仓库内独立二进制或独立服务
- 核心与 MCP 的 v1 传输：HTTP/JSON
- Webhook / 管理接口：HTTP/JSON

v1 不同时冻结 `HTTP/JSON` 和 `gRPC/protobuf` 两套传输栈。先把 HTTP 契约跑通，再根据运行经验决定是否补 gRPC ADR。

### 5.2 持久化基线

- 真实提交边界：JSONL append-only 事件日志
- 恢复加速：周期性快照
- 可重建索引与投影：嵌入式 KV 物化，真源仍是事件日志
- `outbox` / dedupe / route index / 只读投影：都属于可重建物化层，不是第二耐久化真源

### 5.3 依赖库基线

以下依赖属于 v1 建议固定基线：

| 类别 | 选择 | 用途 | 为什么现在固定 |
| --- | --- | --- | --- |
| 嵌入式 KV | `go.etcd.io/bbolt` | 物化 route index、dedupe、outbox pending index、只读投影 | 简单、稳定、嵌入式；又不把事件真源变成关系表 |
| YAML 解析 | `gopkg.in/yaml.v3` | 解析 workflow manifest、配置模板 | manifest 语义与 `manifest_digest` 直接相关，必须固定解析器 |
| Schema 校验 | `github.com/santhosh-tekuri/jsonschema/v6` | 校验 workflow manifest、artifact schema、MCP payload schema | CDR 已要求“manifest 先于 prompt”，这里需要机器校验器落地 |
| 单调 ID | `github.com/oklog/ulid/v2` | 生成 `event_id`、`request_id`、`task_id`、`action_id` 等 | 可排序、可读、实现简单 |
| 调度表达式 | `github.com/robfig/cron/v3` | 解析 cron、计算下一次触发 | cron 细节不值得手写 |
| 重试退避 | `github.com/cenkalti/backoff/v5` | MCP 调用、对账重试、恢复任务重试 | 避免平台层手写退避细节 |
| 断路器 | `github.com/sony/gobreaker/v2` | MCP 域级断路器 | 将失败隔离在域边界，避免拖垮 BUS |
| 限流 | `golang.org/x/time/rate` | MCP 域级限流、入口背压门限 | 简单可靠，避免自造限流轮子 |
| 指标 | `github.com/prometheus/client_golang` | `/metrics` 暴露与内部计数器/直方图 | 运维接口需要尽早稳定 |
| 可控时钟 | `github.com/benbjohnson/clock` | scheduler、gate 超时、恢复测试 | 没有 fake clock，时间相关测试会失真 |

以下选择明确不作为 v1 固定基线：

- `cobra`：单二进制入口较少，标准库 `flag` 足够
- `viper`：配置结构可控，标准库 + `yaml.v3` 足够
- `zap`：Go `1.26` 的 `log/slog` 足够支撑 v1 结构化日志
- gRPC SDK：MCP 先统一 HTTP/JSON，避免双栈
- 任意 LLM 提供商官方 SDK：v1 通过适配层和 `net/http` 封装，暂不绑定

## 6. 代码目录映射

建议代码目录映射如下：

| 目录 | 责任 |
| --- | --- |
| `cmd/alice/` | 核心运行时入口 |
| `cmd/mcp-github/` | GitHub MCP 入口 |
| `cmd/mcp-gitlab/` | GitLab MCP 入口 |
| `cmd/mcp-cluster/` | Cluster MCP 入口 |
| `cmd/mcp-control/` | Control MCP 入口 |
| `cmd/mcp-workflow-registry/` | Workflow Registry MCP 入口 |
| `internal/app/` | 装配、生命周期、配置加载 |
| `internal/domain/` | 核心对象、状态、不变式、命令与事件 |
| `internal/bus/` | 命令执行、路由、状态推进、内部事件调度 |
| `internal/store/` | 事件日志、快照、bbolt 物化索引、重放 |
| `internal/ingress/` | Feishu/Web/GitHub/GitLab/Webhook/Scheduler 输入适配 |
| `internal/policy/` | promotion、workflow 归属、gate、风险/预算策略 |
| `internal/workflow/` | manifest 加载、binding、step runtime、gate runtime |
| `internal/agent/` | `Reception`、leader、reviewer、worker、evaluator 适配层 |
| `internal/mcp/` | MCP HTTP 客户端、域适配器、重试、断路器 |
| `internal/ops/` | 投影、告警、通知、scheduler、reconciler、admin API |
| `internal/platform/` | `slog`、clock、ID、auth、HTTP middleware、文件布局 |
| `api/` | workflow schema、MCP schema、HTTP payload schema 草案 |
| `internal/cli/` | CLI client mode 的命令树、渲染、HTTP 客户端与输出格式 |

## 7. 实现顺序

最短落地顺序建议是：

1. `00-03`：先把运行时、事件日志、快照、索引和 admin/health 基础设施跑通。
2. `04`：再实现统一入口、路由和 `PromotionDecision`。
3. `05`：再落 workflow manifest、binding、step runtime 和 `AgentDispatch`。
4. `07`：然后补 `outbox + MCP`、控制面 workflow 与 scheduler。
5. `06`：最后再把 `issue-delivery` 和 `research-exploration` manifest/runtime 补齐。
6. `08`：并行补运维投影、告警、恢复工具。
7. `09`：最后把 CLI 测试面和 admin/read model 门面稳定下来。

也就是说，先做“薄内核”，再做“业务 workflow”。

## 8. 仍需 ADR 的问题

本轮 TDR 会先给出默认方案，但下面这些点最好继续沉淀成 ADR：

- workflow manifest schema 的版本策略
- `bbolt` 物化层的 bucket 布局与 compact 策略
- LLM provider 适配层是否继续坚持 `net/http`，还是上 SDK
- Workflow Registry MCP 是独立服务，还是先由 GitHub/GitLab MCP 兼任
- 人类动作卡片/按钮 token 的鉴权与过期格式

## 9. 结论

从第一性原理看，Alice v1 的核心不是“写一个会做计划和改代码的 agent”，而是先把：

- 输入怎么进入系统
- 哪些工作必须变成 durable object
- workflow 如何绑定且不漂移
- 外部副作用如何幂等、可恢复地发出
- 人类如何受控地介入

这几条内核边界做对。

因此，这套 TDR 选择的最短路径是：

- 保持单主二进制核心 + 独立 MCP
- 用 JSONL 事件日志做真源
- 用 `EphemeralRequest + PromotionDecision + DurableTask + WorkflowBinding` 做对象基线
- 用 manifest/runtime/gate/outbox 组合承载所有可变业务流程

后续所有正文都默认沿着这条路径展开。
