# TDR 09: CLI 与 Kernel Test Surface

## 1. 目标

本文定义 Alice v1 的 CLI 技术设计。这里的 CLI 不是“补一个没有前端时勉强能用的小工具”，而是：

- 前端缺席时的最小完整测试面
- `cmd/alice` 的标准化客户端模式
- shell / CI / smoke test 的稳定入口

从第一性原理看，CLI 想要真正测试 Alice 内核，至少必须覆盖三类能力：

- 输入源注入
- 治理对象观测
- 受控状态推进

如果只覆盖其中一类，就只能测到零散 HTTP 接口，测不到 `ExternalEvent -> PromotionDecision -> DurableTask -> WorkflowBinding -> outbox + MCP` 这条主链路。

## 2. 非目标

v1 的 CLI 不承担这些职责：

- 不成为 BUS / store / bbolt / JSONL 的旁路写工具
- 不把高阶业务动作固化成 CLI 特权命令
- 不成为 fake MCP 或 fake scheduler 的宿主
- 不替代 Web / Feishu 交互壳

因此，v1 不冻结：

- `schedule create`
- `workflow publish`
- `repo merge`
- 任何直接绕过 `PromotionDecision` 或 workflow runtime 的“捷径命令”

这些动作仍应通过 `submit message/event -> PromotionDecision -> DurableTask -> workflow` 被测试。

## 3. 设计原则

### 3.1 CLI 是薄客户端，不是第二个内核

CLI 只能通过已冻结的 HTTP/JSON 契约访问 Alice：

- ingress 命令提交 `ExternalEvent`
- admin 命令提交 admin write request
- 只读命令读取 `OpsReadModel` 或按 ID 查询对象

CLI 不允许：

- 直接 import `internal/bus`
- 直接写 `data/eventlog/`
- 直接改 `indexes/`
- 直接调 reducer 或 command handler

### 3.2 CLI 同时有两种角色

CLI 必须明确分成两类动作：

- ingress client：模拟真实输入源，把消息或结构化输入注入 Alice
- admin client：做恢复、重放、人工决策、死信重驱等高权限运维动作

不能把这两类角色混成“本地直连 BUS”的单一抽象，否则会破坏 source、auth、audit 语义。

### 3.3 CLI 不引入第二个二进制

v1 只冻结一个二进制：

```text
cmd/alice
  - serve
  - client mode subcommands
```

不单独引入 `alicectl`。原因很简单：

- 当前仓库还处在文档先行阶段
- 核心运行时与测试面需要一起收敛
- 提前拆二进制只会增加配置、版本和发布复杂度

## 4. 运行模式

### 4.1 子命令模式

`cmd/alice` 至少支持两种运行模式：

- `alice serve`
- `alice <client-subcommand>`

其中：

- `serve` 启动核心运行时
- client 子命令只通过 HTTP/JSON 调已经运行的 Alice server

### 4.2 运行前提

client 模式默认要求：

- Alice server 已启动
- 指定 `--server`
- 对 admin/privileged 命令提供 `--token`

除 `serve` 外，CLI 不直接打开 store、快照或事件日志文件。

默认部署策略：

- `submit message` 可在普通内部环境启用
- `submit event` 仅在 `Ops.AdminEventInjectionEnabled=true` 时启用
- `submit fire` 仅在 `Ops.AdminScheduleFireReplayEnabled=true` 时启用

### 4.3 提交确认与读一致性

CLI 作为测试面，不能只做到“写请求被接受”；还必须能稳定读到自己刚写入的结果。

因此 v1 固定两条契约：

- 所有写接口响应都必须返回 `commit_hlc`
- 所有读/list 接口都必须支持 `min_hlc` 与 `wait_timeout_ms`

CLI 侧对应固定能力：

- 所有命中明确 `request/task` route target 的写命令都支持 `--wait`
- 上述命令都支持 `--wait-timeout`
- 当指定 `--wait` 时，CLI 必须使用响应中的 `commit_hlc` 继续轮询对应读接口，直到 `visible_hlc >= commit_hlc` 或超时

不属于这类自动等待的命令：

- `admin replay`
- `admin reconcile outbox`
- `admin reconcile schedules`
- `admin rebuild indexes`
- `admin redrive deadletter`

这些命令应返回 `admin_action_id`，并通过运维视图或后续状态接口观察进度，而不是自动假设某个业务对象必然会变化。

这条契约是为了保证：

- `submit message -> get request`
- `submit message -> get task`
- `resolve approval -> get task`
- `submit fire -> get task`

这些链路在 shell/CI 中是稳定可重复的，而不是依赖“碰巧 projection 已经追上”。

## 5. 稳定对象树

CLI 面向的顶层对象只冻结这些：

- `event`
- `request`
- `task`
- `schedule`
- `approval`
- `human-wait`
- `ops`
- `deadletter`

这些对象不是 CLI 自己发明的展示模型，而是 Alice 核心对象或只读投影的可操作门面。

### 5.1 request 视图

`alice get request` 至少应输出：

- `request_id`
- `updated_hlc`
- `status`
- `promotion_decision`
- `context_packs`
- `agent_dispatches`
- `toolcalls`
- `reply`
- `terminal_result`

### 5.2 task 视图

`alice get task` 至少应输出：

- `task_id`
- `updated_hlc`
- `status`
- `waiting_reason`
- `binding`
- `steps`
- `artifacts`
- `outbox`
- `usage`
- `open approvals`
- `open human waits`

### 5.3 schedule 视图

`alice get schedule` 至少应输出：

- `scheduled_task_id`
- `updated_hlc`
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

### 5.4 approval 视图

`alice get approval` 至少应输出：

- `approval_request_id`
- `updated_hlc`
- `task_id`
- `step_execution_id`
- `gate_type`
- `status`
- `allowed_decisions`
- `expires_at`
- `note`

### 5.5 human-wait 视图

`alice get human-wait` 至少应输出：

- `human_wait_id`
- `updated_hlc`
- `task_id`
- `step_execution_id`
- `waiting_reason`
- `status`
- `allowed_decisions`
- `rewind_targets`
- `expires_at`
- `note`

### 5.6 deadletter 视图

`alice get deadletter` 至少应输出：

- `deadletter_id`
- `updated_hlc`
- `source_event_id`
- `failure_stage`
- `last_error`
- `retryable`
- `first_failed_at`
- `last_failed_at`

### 5.7 event 视图

`alice get event` 至少应输出：

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

### 5.8 list 视图

在没有前端时，CLI 必须有 discovery 能力，因此 v1 需要稳定的 list surface：

- 活跃 requests
- 活跃 tasks
- open human actions
- schedules
- deadletters
- 最近事件

## 6. 命令树

### 6.1 核心命令面

建议 v1 冻结以下命令：

```text
alice serve

alice submit message
alice submit event
alice submit fire

alice get event
alice get request
alice get task
alice get schedule
alice get approval
alice get human-wait
alice get deadletter
alice get ops

alice list requests
alice list tasks
alice list schedules
alice list human-actions
alice list deadletters
alice list events

alice resolve approval
alice resolve wait

alice cancel task

alice admin replay
alice admin reconcile outbox
alice admin reconcile schedules
alice admin rebuild indexes
alice admin redrive deadletter
```

### 6.2 `submit` 族

#### `alice submit message`

用途：

- 模拟真实人类请求
- 走与 Web/IM 相同的“直接输入”语义

最小参数：

- `--text`
- 可选 `--actor`
- 可选 `--source-ref`
- 可选 `--idempotency-key`
- 可选 `--conversation-id`
- 可选 `--thread-id`
- 可选 `--repo`
- 可选 `--reply-to-event`
- 可选 `--trace-id`

行为：

- 由 server 侧标准化成消息型 `ExternalEvent`
- 提交 `IngestExternalEvent`
- 返回 `event_id`、命中的 `request_id/task_id` 与 `commit_hlc`

默认会话规则：

- 未提供 `--conversation-id` 时，每次调用都创建新的 conversation
- 未提供 `--thread-id` 时，默认使用 `root`
- 需要复用同一对话线程时，必须显式传入 `conversation_id`

#### `alice submit event`

用途：

- 受控注入结构化 `ExternalEventInput`
- 用于测试复杂 route key、reply chain 和非消息型入口
- 不是“原样伪造完整 `ExternalEvent`”

最小参数：

- `--file <json>`
- 或 stdin JSON
- 可选 `--actor`
- 可选 `--source-ref`
- 可选 `--idempotency-key`
- 必须带 `--token`

行为：

- 提交给 admin/test 专用 endpoint
- server 侧校验该 endpoint 已启用
- server 侧把 `ExternalEventInput` 规范化成正式 `ExternalEvent`
- 不允许 client 侧跳过 server 直接写事件日志

约束：

- client 不能提供 `event_id`
- client 不能提供 `received_at`
- client 不能提供 `verified`
- client 不能提供 `fire_id`
- client 不能提供 `source_schedule_revision`
- client 不能直接注入 `approval_request_id` / `human_wait_id` 这类人类动作内部对象键
- 需要测试 schedule fire 或人类动作时，必须使用专用命令

推荐允许的 `input_kind`：

- `web_form_message`
- `repo_issue_comment`
- `repo_pr_comment`
- `control_plane_message`

#### `alice submit fire`

用途：

- 测试 `ScheduleTriggered`
- 补偿或手工复现某个 fire

最小参数：

- `--scheduled-task-id`
- `--scheduled-for`
- 可选 `--actor`
- 可选 `--idempotency-key`
- 可选 `--reason`
- 必须带 `--token`

行为：

- 提交 admin/test fire request
- server 侧校验该 endpoint 已启用
- server 侧读取权威 `ScheduledTask`
- server 侧派生 `fire_id`
- server 侧派生 `source_schedule_revision`
- server 侧派生 `target_workflow_id/source/rev`
- server 侧写 admin audit，再生成新的 `ScheduleTriggered`
- 之后仍走统一 route / promote 逻辑

### 6.3 `get/list` 族

这些命令都是只读命令，仅读 admin/read model：

- `get request|task|schedule|approval|human-wait|deadletter|event|ops`
- `list requests|tasks|schedules|human-actions|deadletters|events`

所有 list 命令都必须支持：

- `--limit`
- `--cursor`
- `--output`
- `--min-hlc`
- `--wait-timeout`

命令级过滤矩阵固定如下：

- `list requests`：`--status`、`--conversation-id`、`--actor`、`--updated-since`
- `list tasks`：`--status`、`--workflow-id`、`--repo`、`--waiting-reason`
- `list schedules`：`--enabled`、`--workflow-id`、`--timezone`
- `list human-actions`：`--entry-kind`、`--task-id`、`--status`、`--expires-before`
- `list deadletters`：`--failure-stage`、`--retryable`
- `list events`：`--event-type`、`--source-kind`、`--trace-id`

默认排序：

- `requests/tasks/schedules/human-actions/deadletters` 使用 `updated_hlc desc`
- `events` 使用 `global_hlc desc`

### 6.4 `resolve/cancel` 族

#### `alice resolve approval`

用途：

- 作为 admin client 关闭 `ApprovalRequest`
- 不是复用 UI token path
- 但也不能直接改 gate 状态

最小参数：

- `--approval-request-id`
- `--task-id`
- `--step-execution-id`
- `--decision approve|reject|confirm|resume-budget`
- 可选 `--actor`
- 可选 `--idempotency-key`
- 可选 `--note`
- 必须带 `--token`

行为：

- 调 admin resolve endpoint
- server 侧写 admin audit
- server 侧再生成新的 `HumanActionSubmitted` 事件
- 事件仍需经过 route、活跃性校验与 dedupe

决策矩阵：

- `gate_type=approval` -> 允许 `approve|reject`
- `gate_type=confirmation` -> 允许 `confirm|reject`
- `gate_type=budget` -> 允许 `resume-budget|reject`

#### `alice resolve wait`

用途：

- 关闭 `HumanWaitRecord`
- 支持 `provide-input`、`resume-recovery`、`rewind`

最小参数：

- `--human-wait-id`
- `--task-id`
- 可选 `--step-execution-id`
- `--decision provide-input|resume-recovery|rewind`
- `--waiting-reason WaitingInput|WaitingRecovery`
- 可选 `--target-step-id`
- 可选 `--actor`
- 可选 `--idempotency-key`
- 可选 `--patch-file`
- 可选 `--note`
- 必须带 `--token`

行为：

- 调 admin resolve wait endpoint
- server 侧写 admin audit
- server 侧再生成新的 `HumanActionSubmitted` 事件
- server 侧校验 wait 是否 active
- 只有仍命中当前等待点时才允许恢复或回退

决策矩阵：

- `waiting_reason=WaitingInput` -> 仅允许 `provide-input`
- `waiting_reason=WaitingRecovery` -> 仅允许 `resume-recovery|rewind`
- `decision=rewind` 时 `target_step_id` 必填

#### `alice cancel task`

用途：

- 人工取消任务
- 用于预算熔断、恢复失败、人工终止

最小参数：

- `--task-id`
- 可选 `--step-execution-id`
- 可选 `--reason`
- 可选 `--actor`
- 可选 `--idempotency-key`
- 必须带 `--token`

行为：

- 调 admin cancel endpoint
- server 侧写 admin audit
- server 侧再生成新的 `HumanActionSubmitted(cancel)` 事件
- 只有 task 仍处于可取消状态时才允许成功

### 6.5 `admin/recovery` 族

这些命令是现有 admin API 的 CLI 门面，不增加新语义：

- `alice admin replay`
- `alice admin reconcile outbox`
- `alice admin reconcile schedules`
- `alice admin rebuild indexes`
- `alice admin redrive deadletter`

约束：

- 这组命令默认不支持 `--wait`
- 返回体至少包含 `admin_action_id`
- 观察效果应通过 `get ops`、目标对象读模型或后续 admin status surface 完成

## 7. 全局 flag

v1 必须冻结以下全局 flag：

- `--config`
- `--server`
- `--token`
- `--output text|json|ndjson`
- `--timeout`
- `--trace-id`

所有会返回 route target 的写命令还必须共享以下 cross-cutting flag：

- `--actor`
- `--idempotency-key`
- `--wait`
- `--wait-timeout`

只有 ingress submit 命令额外支持：

- `--source-ref`

建议补充但不必在 v1 首批冻结的 flag：

- `--no-color`
- `--verbose`
- `--quiet`

### 7.1 flag 优先级

client 模式下优先级固定为：

1. 显式 CLI flag
2. 环境变量
3. 配置文件
4. 默认值

`serve` 模式继续遵循 TDR 02 的配置优先级，不因 CLI client mode 改变。

## 8. 输出格式与退出码

### 8.1 输出格式

`--output` 固定三种：

- `text`
- `json`
- `ndjson`

约束：

- 默认 `text`
- 自动化与 CI 推荐 `json` 或 `ndjson`
- `text` 允许更易读，但字段名必须能稳定映射到 JSON

### 8.2 退出码

建议最低退出码基线：

- `0`：成功
- `1`：本地参数或输入错误
- `2`：server 返回鉴权/权限失败
- `3`：server 返回对象不存在或已终态
- `4`：server 返回冲突或前置条件失败
- `5`：网络或超时错误
- `6`：server 内部错误

不能把所有失败都折叠成 `1`，否则脚本无法区分“命令写错”和“系统状态冲突”。

## 9. 鉴权与审计

### 9.1 鉴权边界

只读命令可按部署策略放宽，但下面这些命令必须要求 admin token 或等价鉴权：

- `submit event`
- `submit fire`
- `resolve approval`
- `resolve wait`
- `cancel task`
- `admin *`

### 9.2 审计规则

CLI 不直接写 audit 记录；它只提交请求。

server 侧必须负责：

- `submit message` 生成 `SourceKind=direct_input`、`TransportKind=cli` 的 `ExternalEvent`
- `submit event` 先写 admin audit，再按 `InputKind` 生成对应 `SourceKind`、`TransportKind=cli_admin_injected` 的 `ExternalEvent`
- `submit fire` 先写 admin audit，再生成 `SourceKind=scheduler`、`TransportKind=cli_admin_fire_replay` 且 provenance 完整的 `ScheduleTriggered`
- `resolve approval`、`resolve wait`、`cancel task` 先写 admin audit，再生成 `SourceKind=human_action`、`TransportKind=cli_admin` 的 `HumanActionSubmitted`
- 所有会推进业务状态的 admin 命令都必须先变成新的事件，再进入 BUS
- 所有 admin 合成事件的 `causation_id` 都必须等于该次请求返回的 `admin_action_id`

## 10. 与现有 HTTP/API 的映射

CLI 不是新协议，只是现有协议的门面。推荐映射如下：

| CLI 命令 | server endpoint | 语义 |
| --- | --- | --- |
| `submit message` | `POST /v1/ingress/cli/messages` | 生成消息型 `ExternalEvent` |
| `submit event` | `POST /v1/admin/submit/events` | 受控注入结构化事件 |
| `submit fire` | `POST /v1/admin/submit/fires` | 受控注入 `ScheduleTriggered` |
| `get request` | `GET /v1/requests/{request_id}` | request read model |
| `get task` | `GET /v1/tasks/{task_id}` | task read model |
| `get schedule` | `GET /v1/schedules/{scheduled_task_id}` | schedule read model |
| `get approval` | `GET /v1/approvals/{approval_request_id}` | approval read model |
| `get human-wait` | `GET /v1/human-waits/{human_wait_id}` | human wait read model |
| `get deadletter` | `GET /v1/deadletters/{deadletter_id}` | deadletter read model |
| `get event` | `GET /v1/events/{event_id}` | generic event view |
| `get ops` | `GET /v1/ops/overview` | ops overview |
| `list requests` | `GET /v1/requests` | request list |
| `list tasks` | `GET /v1/tasks` | task list |
| `list schedules` | `GET /v1/schedules` | schedule list |
| `list human-actions` | `GET /v1/human-actions` | open approvals + waits |
| `list deadletters` | `GET /v1/deadletters` | deadletter list |
| `list events` | `GET /v1/events` | event list |
| `resolve approval` | `POST /v1/admin/resolve/approval` | privileged approval resolution |
| `resolve wait` | `POST /v1/admin/resolve/wait` | privileged human wait resolution |
| `cancel task` | `POST /v1/admin/tasks/{task_id}/cancel` | task cancel |
| `admin replay` | `POST /v1/admin/replay/from/{hlc}` | replay |
| `admin reconcile outbox` | `POST /v1/admin/reconcile/outbox` | reconcile outbox |
| `admin reconcile schedules` | `POST /v1/admin/reconcile/schedules` | reconcile schedule |
| `admin rebuild indexes` | `POST /v1/admin/rebuild/indexes` | rebuild indexes |
| `admin redrive deadletter` | `POST /v1/admin/deadletters/{id}/redrive` | deadletter redrive |

### 10.1 通用写响应

所有写接口都必须返回稳定 envelope：

```go
type WriteAcceptedResponse struct {
    Accepted        bool   `json:"accepted"`
    AdminActionID   string `json:"admin_action_id,omitempty"`
    EventID         string `json:"event_id,omitempty"`
    RequestID       string `json:"request_id,omitempty"`
    TaskID          string `json:"task_id,omitempty"`
    RouteTargetKind string `json:"route_target_kind,omitempty"`
    RouteTargetID   string `json:"route_target_id,omitempty"`
    CommitHLC       string `json:"commit_hlc"`
}
```

约束：

- `CommitHLC` 等于该请求同步承诺范围内最后一个已提交 `EventEnvelope.global_hlc`
- `CommitHLC` 是 CLI `--wait` 的唯一一致性锚点
- `AdminActionID` 只用于 admin write path
- `EventID` 由 server 生成，不允许客户端自带
- `submit message`、`submit event`、`submit fire`、`resolve approval`、`resolve wait`、`cancel task` 成功时必须返回 `route_target_kind + route_target_id`
- 若 promote 已发生，`task_id` 必须返回；若仍停留在 request，`request_id` 必须返回
- `admin replay/reconcile/rebuild/redrive` 可只返回 `admin_action_id`，不承诺 route target

所有单对象 `GET` 与 list 响应都必须返回 `visible_hlc`，用于判断当前读模型已经追到哪里。

### 10.2 `submit message` ABI

```go
type CLIMessageSubmitRequest struct {
    Text           string `json:"text"`
    ActorRef       string `json:"actor_ref,omitempty"`
    SourceRef      string `json:"source_ref,omitempty"`
    IdempotencyKey string `json:"idempotency_key,omitempty"`
    ConversationID string `json:"conversation_id,omitempty"`
    ThreadID       string `json:"thread_id,omitempty"`
    RepoRef        string `json:"repo_ref,omitempty"`
    ReplyToEventID string `json:"reply_to_event_id,omitempty"`
    TraceID        string `json:"trace_id,omitempty"`
}
```

server 补齐：

- `event_id`
- `received_at`
- `verified`
- `source_kind=direct_input`
- `transport_kind=cli`
- 缺省 `thread_id=root`

### 10.3 `submit event` ABI

```go
type ExternalEventInput struct {
    InputKind       string         `json:"input_kind"`
    ActorRef        string         `json:"actor_ref,omitempty"`
    SourceRef       string         `json:"source_ref,omitempty"`
    IdempotencyKey  string         `json:"idempotency_key,omitempty"`
    ConversationID  string         `json:"conversation_id,omitempty"`
    ThreadID        string         `json:"thread_id,omitempty"`
    RepoRef         string         `json:"repo_ref,omitempty"`
    IssueRef        string         `json:"issue_ref,omitempty"`
    PRRef           string         `json:"pr_ref,omitempty"`
    ReplyToEventID  string         `json:"reply_to_event_id,omitempty"`
    ScheduledTaskID string         `json:"scheduled_task_id,omitempty"`
    ControlObjectRef string        `json:"control_object_ref,omitempty"`
    WorkflowObjectRef string       `json:"workflow_object_ref,omitempty"`
    BodySchemaID    string         `json:"body_schema_id"`
    Body            json.RawMessage `json:"body,omitempty"`
    TraceID         string         `json:"trace_id,omitempty"`
}
```

约束：

- `InputKind` 只能取 `web_form_message`、`repo_issue_comment`、`repo_pr_comment`、`control_plane_message`
- 不允许出现 `event_id`、`received_at`、`verified`
- 不允许出现 `fire_id`、`source_schedule_revision`
- 不允许直接出现 `approval_request_id`、`human_wait_id`
- server 负责把 `InputKind` 映射成正式 `event_type`
- server 必须按 `BodySchemaID` 校验 `Body`，不能只做黑名单过滤
- route-critical 字段只能出现在顶层：`reply_to_event_id`、`scheduled_task_id`、`control_object_ref`、`workflow_object_ref`
- body 与顶层重复声明 route-critical 字段时，server 必须返回 `422`

v1 冻结的 `InputKind -> BodySchemaID` 基线：

- `web_form_message` -> `web-form-message.v1`
- `repo_issue_comment` -> `repo-issue-comment.v1`
- `repo_pr_comment` -> `repo-pr-comment.v1`
- `control_plane_message` -> `control-plane-message.v1`

v1 冻结的 `InputKind -> SourceKind` 基线：

- `web_form_message` -> `direct_input`
- `repo_issue_comment` -> `repo_comment`
- `repo_pr_comment` -> `repo_comment`
- `control_plane_message` -> `control_plane`

各 schema 的最小字段：

- `web-form-message.v1`：`text` 必填；`form_id`、`labels` 可选
- `repo-issue-comment.v1`：`comment_text` 必填；`comment_ref` 可选，若存在则抽取到 `NormalizedEvent.CommentRef`
- `repo-pr-comment.v1`：`comment_text` 必填；`comment_ref` 可选，若存在则抽取到 `NormalizedEvent.CommentRef`
- `control-plane-message.v1`：`text` 必填；body 内不得重复声明 `scheduled_task_id`、`control_object_ref`、`workflow_object_ref`

### 10.4 `submit fire` ABI

```go
type ScheduleFireReplayRequest struct {
    ScheduledTaskID   string    `json:"scheduled_task_id"`
    ScheduledForWindow time.Time `json:"scheduled_for_window"`
    ActorRef          string    `json:"actor_ref,omitempty"`
    IdempotencyKey    string    `json:"idempotency_key,omitempty"`
    Reason            string    `json:"reason,omitempty"`
    TraceID           string    `json:"trace_id,omitempty"`
}
```

server 派生：

- `fire_id`
- `source_schedule_revision`
- `target_workflow_id`
- `target_workflow_source`
- `target_workflow_rev`
- `source_kind=scheduler`
- `transport_kind=cli_admin_fire_replay`
- 正式 `ScheduleTriggered` 事件

### 10.5 `resolve approval` ABI

```go
type ResolveApprovalRequest struct {
    ApprovalRequestID string `json:"approval_request_id"`
    TaskID            string `json:"task_id"`
    StepExecutionID   string `json:"step_execution_id"`
    Decision          string `json:"decision"`
    ActorRef          string `json:"actor_ref,omitempty"`
    IdempotencyKey    string `json:"idempotency_key,omitempty"`
    Note              string `json:"note,omitempty"`
    TraceID           string `json:"trace_id,omitempty"`
}
```

server 约束：

- 必须先写 admin audit
- 必须校验 approval 仍为 active
- 必须按 `gate_type` 校验 `decision`
- 必须生成新的 `HumanActionSubmitted`

### 10.6 `resolve wait` ABI

```go
type ResolveHumanWaitRequest struct {
    HumanWaitID     string         `json:"human_wait_id"`
    TaskID          string         `json:"task_id"`
    StepExecutionID string         `json:"step_execution_id,omitempty"`
    WaitingReason   string         `json:"waiting_reason"`
    Decision        string         `json:"decision"`
    TargetStepID    string         `json:"target_step_id,omitempty"`
    ActorRef        string         `json:"actor_ref,omitempty"`
    IdempotencyKey  string         `json:"idempotency_key,omitempty"`
    InputPatch      json.RawMessage `json:"input_patch,omitempty"`
    Note            string         `json:"note,omitempty"`
    TraceID         string         `json:"trace_id,omitempty"`
}
```

server 约束：

- `WaitingInput` 仅允许 `provide-input`
- `WaitingRecovery` 仅允许 `resume-recovery|rewind`
- `rewind` 必须携带 `target_step_id`
- `input_patch` 固定采用 JSON Merge Patch（RFC 7396）
- server 必须先将 patch 应用于当前输入草稿，再按当前 `InputSchemaID` 校验结果文档
- 只有结果文档满足恢复条件时，才允许生成 `HumanActionSubmitted`
- 必须先写 admin audit，再生成新的 `HumanActionSubmitted`

### 10.7 `cancel task` 与 `redrive deadletter` ABI

```go
type CancelTaskRequest struct {
    TaskID          string `json:"task_id"`
    StepExecutionID string `json:"step_execution_id,omitempty"`
    ActorRef        string `json:"actor_ref,omitempty"`
    IdempotencyKey  string `json:"idempotency_key,omitempty"`
    Reason          string `json:"reason,omitempty"`
    TraceID         string `json:"trace_id,omitempty"`
}

type RedriveDeadletterRequest struct {
    DeadletterID    string `json:"deadletter_id"`
    ActorRef        string `json:"actor_ref,omitempty"`
    IdempotencyKey  string `json:"idempotency_key,omitempty"`
    Reason          string `json:"reason,omitempty"`
    TraceID         string `json:"trace_id,omitempty"`
}
```

server 约束：

- `cancel task` 必须先写 admin audit，再生成新的 `HumanActionSubmitted(cancel)`
- `redrive deadletter` 必须校验 `retryable=true`
- `redrive deadletter` 不允许直接改写原事件，只能生成重驱动作或重新投递命令

### 10.8 HTTP status 与 CLI exit code 映射

- `400` / `422` -> exit code `1`
- `401` / `403` -> exit code `2`
- `404` / `410` -> exit code `3`
- `409` / `412` -> exit code `4`
- network timeout / dial error -> exit code `5`
- `500+` -> exit code `6`

## 11. 与现有 TDR 的联动

引入 CLI TDR 后，至少需要同步这些章节：

- `README.md`
  需要把 CLI TDR 纳入目录。
- `00-system-overview.md`
  需要把 CLI 明确成 `cmd/alice` 的 client mode，而不是新内核。
- `02-app-bootstrap-and-runtime.md`
  需要补 serve/client 子命令与 flag 优先级。
- `04-entry-routing-and-promotion.md`
  需要补 CLI message/event/fire 的入口映射。
- `08-ops-scheduler-and-recovery.md`
  需要补 list/read/admin endpoints，保证 CLI 有 discovery surface。

## 12. 验收场景

CLI TDR 至少要支持下面六条验收链路：

1. 直答链路
   - `submit message --wait`
   - 命中 request
   - `get request`
2. promote 链路
   - `submit message --wait`
   - 触发 durable workflow
   - `get task`
3. approval 链路
   - `list human-actions`
   - `resolve approval --wait`
   - `get task`
4. human wait / rewind 链路
   - `list human-actions`
   - `resolve wait --decision provide-input|rewind --wait`
   - `get task`
5. outbox / recovery 链路
   - `get task`
   - `admin reconcile outbox`
   - `get ops`
6. schedule fire 链路
   - `get schedule`
   - `submit fire --wait`
   - `get task`

如果 CLI 不能完整覆盖这六条链路，就还不算“功能完整可用的内核测试面”。

CLI 验收只覆盖 kernel/admin surface，不替代下列适配层测试：

- `/v1/human-actions/{token}` 的验签、过期、decision hash 与去重
- scheduler 自主扫描 `ScheduledTask` 并派生 fire 的真实循环
- Feishu/Web/GitHub/GitLab 入口各自的验签与 actor 识别
