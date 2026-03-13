# TDR 04: 入口路由、Promotion 与人类动作

## 1. 目标

本文把入口阶段落成实现方案，覆盖：

- 多来源输入的标准化
- `ExternalEvent` 路由键与命中算法
- `PromotionDecision` 生成与裁决
- direct-answer path
- promote 到 `DurableTask` 的原子命令流
- request 级审计
- 人类确认/补充/审批的回流接口

## 2. 输入适配器

### 2.1 入口类型

v1 入口统一分为五类语义输入，再加一个 CLI 传输门面：

- IM 消息
- Web 表单 / 管理页动作
- GitHub webhook
- GitLab webhook
- scheduler 触发

CLI 不是第六种语义输入。`alice submit message` 只是“直接输入”这一类的 CLI transport；`submit event` 和 `submit fire` 则分别走受控 admin/test surface。

这里的 “admin/test surface” 不是“原样追加任意 `ExternalEvent`”。它们只接受受控输入，由 server 侧补齐并生成新的规范事件。

它们最终都必须落成 `ExternalEvent`。

### 2.2 HTTP 接口

建议的核心入口：

| 接口 | 用途 |
| --- | --- |
| `POST /v1/ingress/im/feishu` | 接收 IM 消息 |
| `POST /v1/ingress/im/feishu/cards` | 接收 Feishu 卡片动作 |
| `POST /v1/ingress/cli/messages` | 接收 CLI 消息输入 |
| `POST /v1/ingress/web/messages` | 接收 Web 管理页输入 |
| `POST /v1/webhooks/github` | 接收 GitHub webhook |
| `POST /v1/webhooks/gitlab` | 接收 GitLab webhook |
| `POST /v1/human-actions/{token}` | 接收 Web/CLI 直连的人类动作回流 |
| `POST /v1/scheduler/fires` | scheduler 内部投递 fire 事件 |

这些接口都只能提交 `IngestExternalEvent` 命令，不能直接修改 request/task。

结构化事件注入和手工 fire 回放不属于普通 ingress，它们必须走 admin/test endpoint，并且保留更强鉴权和 admin audit。

附加约束：

- `POST /v1/admin/submit/events` 接受的是 `ExternalEventInput`，不是完整 `ExternalEvent`
- `event_id`、`received_at`、`verified`、`normalized route keys` 一律由 server 生成
- `POST /v1/admin/submit/fires` 只能接受 `scheduled_task_id + scheduled_for_window + reason`，`fire_id` 与 `source_schedule_revision` 只能由 server 从权威 `ScheduledTask` 派生
- 以上两个 endpoint 默认只在测试/演练/受控恢复模式开启，生产默认关闭

## 3. 标准化

### 3.1 标准化结果

```go
type NormalizedEvent struct {
    EventType         domain.EventType
    SourceKind        string
    TransportKind     string
    SourceRef         string
    ActorRef          string
    RequestID         string
    TaskID            string
    ReplyToEventID    string
    ConversationID    string
    ThreadID          string
    RepoRef           string
    IssueRef          string
    PRRef             string
    CommentRef        string
    ScheduledTaskID   string
    ControlObjectRef  string
    WorkflowObjectRef string
    CoalescingKey     string
    PayloadRef        string
    Verified          bool
}
```

其中：

- `SourceKind` 表示语义输入类型，用于路由、coalescing 与策略判定
- `TransportKind` 表示接入传输方式，用于审计与 adapter 诊断

### 3.2 标准化原则

- 入口适配层负责验签和 actor 识别
- 大 payload 先入 `blobs/`，只把引用塞进事件
- 不在适配层做 business routing
- scheduler fire 也必须走同一套标准化
- `request_id` / `task_id` 这类显式对象键，只允许来自已鉴权的人类动作、内部 scheduler 或 admin 通道，不能直接信任第三方 webhook 自报
- `submit message` 允许客户端提供 `idempotency_key`、`source_ref`、`actor_ref`，但不允许客户端提供最终 `event_id`
- CLI / Web / IM 这类 transport 差异只落到 `TransportKind`，不应额外制造新的 `SourceKind` 路由域
- 当多个 transport 共享同一 `SourceKind` 时，adapter 必须先把 `conversation_id` 做 transport 级命名空间化，例如 `cli:<id>`、`feishu:<id>`、`web:<id>`

## 4. 路由算法

### 4.1 命中顺序

命中顺序固定为：

1. `task_id` / `request_id`
2. `reply_to_event_id`
3. `repo_ref + issue_ref` 或 `repo_ref + pr_ref`
4. `scheduled_task_id` 或 `control_object_ref`
5. `workflow_object_ref`
6. `conversation_id + thread_id`
7. `coalescing_key`
8. 未命中则创建新的 `EphemeralRequest`

### 4.2 冲突规则

如果 `reply_to_event_id` 命中普通业务 task，但同一事件又显式携带：

- `scheduled_task_id`
- `control_object_ref`
- `workflow_object_ref`

且它们指向不同治理域，则不能继续复用原 task；必须：

- 拆出新的控制面 request/task
- 或进入澄清路径

### 4.3 活跃对象规则

route index 只返回可命中的活跃对象：

- `RequestStatus=Open`
- `TaskStatus in (NewTask, Active, WaitingHuman)`

终态对象默认不复用。

### 4.4 canonical route key

route key 不能由不同适配器各自拼字符串。v1 必须统一使用 `RouteKeyEncoder`：

```go
type RouteKeyEncoder interface {
    ReplyTo(eventID string) string
    RepoIssue(repoRef string, issueRef string) string
    RepoPR(repoRef string, prRef string) string
    Conversation(sourceKind string, conversationID string, threadID string) string
    ScheduledTask(id string) string
    ControlObject(ref string) string
    WorkflowObject(ref string) string
    Coalescing(sourceKind string, actorRef string, intentKind string, target string, bucket time.Time) string
}
```

规范化规则：

- `repo_ref` 统一编码为 `<provider>:<owner>/<repo>`，全部小写
- `issue_ref` / `pr_ref` 统一编码为十进制数字字符串，不保留前导零
- `conversation_id + thread_id` 统一编码为 `<source_kind>:<conversation_id>:<thread_id|root>`
- `scheduled_task_id` 统一编码为 `schedule:<id>`
- `control_object_ref` 统一编码为 `control:<ref>`
- `workflow_object_ref` 统一编码为 `workflow:<ref>`
- `coalescing_key` 统一编码为 `sha256(<source_kind>|<actor_ref>|<intent_kind>|<normalized_target>|<5m-bucket>)`

### 4.5 索引写入/撤销规则

| 对象变化 | 需要写入的 route key | 需要撤销的 route key |
| --- | --- | --- |
| 新 request `Open` | `conversation`、可选 `coalescing` | 无 |
| request `Answered/Expired/Promoted` | 无 | 该 request 持有的全部 active route key |
| 新 task `NewTask/Active` | `task_id`、repo/PR/issue、控制面对象键、可选 `reply_to_event_id` | 无 |
| task 进入终态 | 无 | 该 task 持有的全部 active route key |
| artifact / 外部引用 supersede | 新引用对应的 key | 旧引用对应的 key |

## 5. `Reception` 与 `PromotionDecision`

### 5.1 运行时接口

```go
type Reception interface {
    Assess(ctx context.Context, in ReceptionInput) (*domain.PromotionDecision, error)
}
```

### 5.2 输入

`ReceptionInput` 至少包含：

- 当前 `ExternalEvent`
- 已命中对象快照
- request 上下文摘要
- 外部引用摘要
- 允许使用的只读工具与只读 MCP

### 5.3 输出

`PromotionDecision` 必须至少带：

- `intent_kind`
- `required_refs`
- `risk_level`
- `external_write`
- `create_persistent_object`
- `async`
- `multi_step`
- `multi_agent`
- `approval_required`
- `budget_required`
- `recovery_required`
- `proposed_workflow_ids`
- `result`
- `reason_codes`
- `confidence`

### 5.4 策略裁决

策略层不是再“理解自然语言”，而是按结构化字段做硬裁决：

- 只读 allowlist -> direct answer
- 命中 promote 硬规则 -> promote
- 缺少必要引用 -> ask followup / human
- 候选 workflow 不唯一 -> ask followup / human

## 6. direct-answer path

### 6.1 执行条件

同时满足以下条件才允许直答：

- `external_write=false`
- `create_persistent_object=false`
- `async=false`
- `multi_step=false`
- `multi_agent=false` 或仅限只读 helper
- `approval_required=false`
- `budget_required=false`
- `recovery_required=false`
- 命中 allowlist

### 6.2 执行步骤

1. 打开或复用 `EphemeralRequest`
2. 写 `PromotionDecision`
3. 必要时创建 `ContextPack`
4. 必要时创建只读 `AgentDispatch`
5. 记录 `ToolCallRecord`
6. 生成 `ReplyRecord`
7. 生成 `TerminalResult`
8. request 进入 `Answered`

### 6.3 只读 helper 约束

request 内 helper 默认只允许：

- 条件抽取
- 查询摘要
- 结果格式化

不允许：

- promote workflow
- 创建持久对象
- 直接触发 MCP 写动作

## 7. promote 流程

### 7.1 原子命令流

```text
IngestExternalEvent
  -> route existing object or create EphemeralRequest
  -> AssessPromotion
  -> if result=promote:
       ResolveWorkflowCandidate
       PromoteAndBindWorkflow
```

这里的 `PromoteAndBindWorkflow` 是单个原子命令，必须在同一事件批次里同时完成：

- request 标记 `Promoted`
- 创建 `DurableTask`
- 创建 active `WorkflowBinding`
- 必要时创建首个 `StepExecution`

v1 不允许存在单独的 `PromoteTask` 命令，也不允许出现“已 promote 但未绑定 workflow”的 durable 中间态。

### 7.2 workflow 候选唯一化

BUS 只在以下条件同时满足时绑定 workflow：

- `PromotionDecision` 命中 promote
- manifest 机器约束校验通过
- 候选 workflow 恰好一个

否则不允许“猜一个先跑”。

### 7.3 scheduler 触发特例

scheduler 产生的是系统事件，不是旁路：

- 先产生 `ExternalEvent`
- 先按 `scheduled_task_id` 查调度来源对象，而不是普通活跃 task route index
- 再走统一命中逻辑
- 由于必然命中 promote，只有在来源 schedule 已解析且目标 workflow 唯一时，才在同一事务里创建短生命周期 request 包络并立即 `PromoteAndBindWorkflow`

如果来源 schedule 不存在、已禁用或 revision 信息缺失，则不得新建 task，必须转入恢复/人工处理路径。

## 8. request 级审计

### 8.1 必须落盘的记录

- `ExternalEvent`
- `PromotionDecision`
- 必要的 `ContextPack`
- 必要的 `AgentDispatch`
- `ToolCallRecord`
- `ReplyRecord`
- `TerminalResult`

### 8.2 API 读取接口

建议提供：

- `GET /v1/requests/{request_id}`
- `GET /v1/requests/{request_id}/events`
- `GET /v1/requests/{request_id}/toolcalls`
- `GET /v1/requests/{request_id}/reply`

## 9. 人类动作回流

### 9.1 token 设计

按钮/卡片 token 不是只给 `approval` gate 用的。建议统一 claims：

```go
type HumanActionTokenClaims struct {
    ActionKind        string    `json:"action_kind"`
    RequestID         string    `json:"request_id"`
    TaskID            string    `json:"task_id"`
    ReplyToEventID    string    `json:"reply_to_event_id"`
    ApprovalRequestID string    `json:"approval_request_id"`
    HumanWaitID       string    `json:"human_wait_id"`
    StepExecutionID   string    `json:"step_execution_id"`
    TargetStepID      string    `json:"target_step_id"`
    WaitingReason     string    `json:"waiting_reason"`
    ScheduledTaskID   string    `json:"scheduled_task_id"`
    ControlObjectRef  string    `json:"control_object_ref"`
    WorkflowObjectRef string    `json:"workflow_object_ref"`
    DecisionHash      string    `json:"decision_hash"`
    ExpiresAt         time.Time `json:"expires_at"`
    Nonce             string    `json:"nonce"`
}
```

规则：

- `approve` / `reject` / `confirm` 必须带 `approval_request_id + task_id + step_execution_id`
- `provide_input` 必须带 `waiting_reason=WaitingInput`，以及 `task_id` 或 `reply_to_event_id`；durable workflow 路径下应额外带 `human_wait_id`
- `resume_budget` 必须带 `approval_request_id + task_id + step_execution_id + waiting_reason=WaitingBudget`
- `resume_recovery` 必须带 `human_wait_id + task_id + step_execution_id + waiting_reason=WaitingRecovery`
- `rewind` 必须带 `task_id + step_execution_id + target_step_id`
- `cancel` 必须带 `task_id`；若针对当前执行点取消，还必须带 `step_execution_id`
- 控制面回流可带 `scheduled_task_id`、`control_object_ref` 或 `workflow_object_ref`
- 所有 token 都必须带 `decision_hash + expires_at + nonce`，并使用 HMAC-SHA256 签名

### 9.1a Feishu 卡片回流约定

Feishu 卡片点击不应绕开 `human_actions` 语义。实现约束如下：

- HTTP 入口固定为 `POST /v1/ingress/im/feishu/cards`
- 传输层由独立 `internal/feishu` 组件负责 SDK dispatcher、验签、解密和回调响应
- 业务层仍统一落成新的 `ExternalEvent`
- `SourceKind` 固定为 `human_action`
- `TransportKind` 固定为 `im_feishu_card`
- `ActorRef` 来自 Feishu operator 标识，命名空间化为 `feishu:open_id:<id>` 或 `feishu:user_id:<id>`

卡片 `action.value` 至少必须包含：

- `human_action_token`：已签名的 `HumanActionTokenClaims`
- 可选 `action_kind`：显式覆盖 UI 动作名，最终仍必须和 token claims 一致
- 可选 `decision_hash`：若提供，必须和 token claims 一致
- 可选 `trace_id`
- 可选 `input_schema_id`

卡片表单输入必须进入 `ExternalEvent.InputPatch`，供审计和后续 `provide_input` / `resume_*` 场景复用。

### 9.2 回流校验顺序

1. 验签
2. 检查过期
3. 查重
4. 将 transport payload 标准化为新的 `ExternalEvent`
5. 路由到当前 request/task
6. 校验 gate/task 是否仍活跃
7. 只在条件满足时恢复执行

### 9.2a admin/CLI 合成回流

CLI admin 命令不是直接推进状态，而是 server 侧先写 admin audit，再合成新的 `ExternalEvent`：

- `resolve approval` -> `HumanActionSubmitted`
- `resolve wait` -> `HumanActionSubmitted`
- `cancel task` -> `HumanActionSubmitted`

这些事件的公共约束：

- `SourceKind` 固定为 `human_action`
- `TransportKind` 固定为 `cli_admin`
- `SourceRef` 固定为 `cli:<actor_ref>`
- `CausationID` 必须等于对应 `admin_action_id`
- 事件体只允许包含该动作所需最小字段
- 仍然必须经过 route、活跃性校验、dedupe 和事件落盘

这里的 `HumanActionSubmitted` 与上游 draft 中的 `HumanDecisionReceived` 属于同一类外部回流事件；TDR 在实现层统一采用 `HumanActionSubmitted` 作为 ingress 名称。

### 9.3 回流动作类型

- `approve`
- `reject`
- `confirm`
- `cancel`
- `provide_input`
- `resume_budget`
- `resume_recovery`
- `rewind`

## 10. 关键接口

```go
type Router interface {
    Route(ctx context.Context, evt *domain.ExternalEvent) (*RouteTarget, error)
}

type PromotionEngine interface {
    Assess(ctx context.Context, evt *domain.ExternalEvent, target *RouteTarget) (*domain.PromotionDecision, error)
    ResolveWorkflow(ctx context.Context, decision *domain.PromotionDecision, evt *domain.ExternalEvent) (*WorkflowCandidate, error)
}
```

## 11. 入口阶段日志规范

### 11.1 关键日志点

| 操作 | 日志键 | 级别 | 必需字段 |
|-----|-------|-----|---------|
| 事件接收 | `event_received` | INFO | `event_id`, `source_kind`, `transport_kind`, `trace_id` |
| 事件标准化 | `event_normalized` | DEBUG | `event_id`, `route_keys` |
| 去重检查 | `dedupe_checked` | DEBUG | `event_id`, `idempotency_key`, `seen` |
| 路由解析 | `route_resolved` | INFO | `event_id`, `route_target_kind`, `route_target_id`, `route_key`, `conflict` |
| 意图识别开始 | `reception_started` | DEBUG | `request_id`, `event_id` |
| 意图识别完成 | `reception_completed` | INFO | `request_id`, `decision_id`, `intent_kind`, `risk_level`, `confidence` |
| 策略评估 | `policy_evaluated` | INFO | `request_id`, `decision_id`, `result`, `reason_codes` |
| 直接回答 | `direct_answer` | INFO | `request_id`, `reply_id`, `duration_ms` |
| 提升决策 | `task_promoted` | INFO | `request_id`, `task_id`, `workflow_id`, `binding_id` |
| 请求打开 | `request_opened` | INFO | `request_id`, `event_id`, `expires_at` |
| 请求终态 | `request_closed` | INFO | `request_id`, `final_status`, `terminal_result_id` |

### 11.2 日志示例

```json
{
  "timestamp": "2026-03-10T18:00:00.123Z",
  "level": "INFO",
  "logger": "bus",
  "msg": "event_received",
  "trace_id": "trace_01HQMZ5J8XYC5QNPJ",
  "event_id": "evt_01HQMZ5J8XYC5QNPJ1234",
  "source_kind": "cli",
  "transport_kind": "http",
  "source_ref": "北京天气"
}
```

```json
{
  "timestamp": "2026-03-10T18:00:01.456Z",
  "level": "INFO",
  "logger": "reception",
  "msg": "reception_completed",
  "trace_id": "trace_01HQMZ5J8XYC5QNPJ",
  "request_id": "req_01HQMZ5J8XYC5QNPJ5678",
  "decision_id": "dec_01HQMZ5J8XYC5QNPJ9012",
  "intent_kind": "weather_query",
  "risk_level": "low",
  "external_write": false,
  "confidence": 0.95
}
```

## 12. 测试建议

必须覆盖：

- reply chain 与控制面对象键冲突
- request 终态后不复用
- scheduler fire 仍走统一入口
- direct-answer path 的最小审计不缺项
- workflow 候选为 0 / 多个 / 低置信度时不会偷偷绑定
- 日志字段完整性（trace_id贯穿、duration_ms准确）
