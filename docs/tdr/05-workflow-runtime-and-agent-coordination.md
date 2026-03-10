# TDR 05: Workflow Runtime、Gate 与 Agent 协调

## 1. 目标

本文定义 workflow manifest、`WorkflowBinding`、`StepExecution`、gate、`ContextPack`、`AgentDispatch` 的实现方式。

v1 的原则很简单：

- 业务步骤来自 manifest
- BUS 只负责生命周期、binding、gate 和回退
- agent 是执行器，不是状态真源

## 2. manifest 布局

### 2.1 文件布局

建议把 workflow manifest 放在：

```text
configs/workflows/
  issue-delivery/
    manifest.yaml
    schemas/
  research-exploration/
    manifest.yaml
    schemas/
  schedule-management/
    manifest.yaml
  workflow-management/
    manifest.yaml
```

### 2.2 schema 路径

建议在 `api/` 下维护可校验 schema：

```text
api/workflow/v1alpha1/manifest.schema.json
api/artifact/v1alpha1/*.schema.json
api/mcp/v1alpha1/*.schema.json
```

## 3. manifest 结构

```yaml
workflow_id: issue-delivery
workflow_source: file://configs/workflows/issue-delivery/manifest.yaml
workflow_rev: gitsha-or-release-tag
entry:
  requires: [repo_ref]
  required_refs: [repo_ref]
  allowed_mcp: [github, gitlab]
  allowed_tools: [repo_read, repo_write, test_runner]
  max_risk: high
steps:
  - id: triage
    role: leader
    outputs: [task_brief]
    next: [plan]
gates:
  - id: plan_approval
    type: approval
    attach_after: plan
```

必填字段：

- `workflow_id`
- `workflow_source`
- `workflow_rev`
- `entry`
- `steps`

### 3.1 可执行 schema

v1 必须把 `steps[*]` 和 `gates[*]` 的机器字段定死，最低要求如下：

```yaml
steps:
  - id: code
    runner_kind: agent
    slot: worker
    inputs:
      - family: plan
        schema_id: artifact.plan
        required: true
    outputs:
      - family: candidate_patch
        schema_id: artifact.candidate_patch
        required: true
    allowed_tools: [repo_read, repo_write, test_runner]
    allowed_mcp: [github]
    sandbox_template: coding-default
    timeout_seconds: 1800
    max_retries: 2
    on_success: review
    on_failure: code
gates:
  - id: merge_approval
    type: approval
    attach_after: review
    required_slots: [owner]
    timeout_seconds: 86400
    on_approve: merge
    on_reject: code
    on_expire: WaitingHuman
  - id: eval_gate
    type: evaluation
    attach_after: evaluate
    result_family: evaluation_result
    rules:
      - metric: primary_score
        op: gte
        threshold: 0.85
    aggregate: all
    on_pass: report
    on_fail: code
    on_error: WaitingHuman
```

机器字段基线：

- `steps[*].id`
- `steps[*].runner_kind`
- `steps[*].slot`
- `steps[*].inputs[*].family/schema_id/required`
- `steps[*].outputs[*].family/schema_id/required`
- `steps[*].allowed_tools`
- `steps[*].allowed_mcp`
- `steps[*].sandbox_template`
- `steps[*].timeout_seconds`
- `steps[*].max_retries`
- `steps[*].on_success`
- `steps[*].on_failure`
- `gates[*].id`
- `gates[*].type`
- `gates[*].attach_after` 或 `attach_before`
- `gates[*].required_slots`
- `gates[*].timeout_seconds`
- `gates[*].result_family`
- `gates[*].rules[*].metric/op/threshold`
- `gates[*].aggregate`
- `gates[*].on_approve`
- `gates[*].on_reject`
- `gates[*].on_pass`
- `gates[*].on_fail`
- `gates[*].on_error`
- `gates[*].on_expire`

## 4. manifest 规范化与 digest

### 4.1 规范化流程

1. 用 `yaml.v3` 解析
2. 转成内部结构体
3. 依 schema 做 JSON Schema 校验
4. 归一化排序：
   - step 顺序
   - gate 顺序
   - map key 顺序
5. 编码成 canonical JSON
6. 计算 `sha256`

### 4.2 `manifest_digest`

```text
manifest_digest = sha256(canonical_json(manifest))
```

这个 digest 是 binding 真正指向的内容摘要，而不是“文件名”或“人类可读 revision”。

### 4.3 runtime DAG 构建

manifest 载入后，runtime 必须在内存中构建：

- `step_id -> StepSpec`
- `gate_id -> GateSpec`
- `attach_after/attach_before -> gate list`
- `on_success/on_failure -> next node`

如果发现：

- 缺失 next step
- gate 指向不存在的 step
- inputs/outputs schema 不可解析
- `on_*` 形成非法死循环

则 manifest 载入失败，不能进入 registry 或 binding。

## 5. workflow 候选裁决

### 5.1 输入

候选裁决输入至少包括：

- `PromotionDecision`
- route 命中结果
- manifest `entry.requires/forbids/required_refs`
- manifest `entry.allowed_mcp` / `entry.allowed_tools`
- manifest `entry.max_risk`
- 显式控制面对象键

### 5.2 唯一化规则

1. 先按显式控制面对象键裁掉不相关 workflow
2. 再按 `PromotionDecision` 的主成功条件做筛选
3. 再按 manifest 约束校验
4. 若剩余候选恰好一个，则可绑定
5. 若为零或多个，则进入 followup / human 路径

其中“manifest 约束校验”至少包括：

- `entry.requires` 是否成立
- `required_refs` 是否齐全
- `PromotionDecision.RiskLevel` 是否超过 `entry.max_risk`
- 决策要求的 MCP/tool 是否超出 workflow 允许范围
- 控制面键是否与 workflow 类型匹配
- `entry.forbids` 是否命中当前来源或对象状态

### 5.3 绑定接口

```go
type WorkflowRegistry interface {
    Load(ctx context.Context, workflowID string, rev string) (*Manifest, error)
    ResolveCandidate(ctx context.Context, decision *domain.PromotionDecision, evt *domain.ExternalEvent) ([]ManifestRef, error)
}
```

## 6. `WorkflowBinding`

### 6.1 绑定记录

绑定时必须持久化：

- `workflow_id`
- `workflow_source`
- `workflow_rev`
- `manifest_digest`
- `entry_step_id`
- manifest 快照引用

### 6.2 重规划

如果策略允许重规划：

- 只能在显式审批点发生
- 只能追加新的 `WorkflowBinding`
- 旧 binding 标记 `superseded`
- 旧 step/gate/outbox 记录保持可追踪

## 7. `StepExecution`

### 7.1 状态

- `ready`
- `running`
- `succeeded`
- `failed`
- `superseded`
- `cancelled`

### 7.2 执行租约

每个 running step 必须带 lease：

- `lease_owner`
- `lease_expires_at`

lease 过期后可以由恢复流程回收并重新派发。

### 7.3 attempt、checkpoint 与 resume

v1 不把长跑 step 的恢复留给“内存里的 runner 自己记住”，而是要求：

- 每次 step 重试都递增 `StepExecution.Attempt`
- 任何长于一个 lease 周期的 step，必须周期性写 `StepExecutionCheckpointed`
- checkpoint 至少要包含 `CheckpointRef`、`ResumeToken`、`RemoteExecutionRef`、`LastHeartbeatAt`
- `Resume` 必须基于 `ExecutionID + Attempt + ResumeToken` 做幂等恢复，不能重新生成一套新的外部执行上下文
- `Cancel` 必须优先使用 `RemoteExecutionRef` 或等价远端句柄，而不是仅靠本地标记取消

推荐 lease 约束：

- heartbeat 周期不超过 `lease_ttl / 2`
- 连续两个 heartbeat 窗口未更新 checkpoint，则可判定 lease 失活

### 7.4 运行时接口

```go
type StepRunner interface {
    Start(ctx context.Context, exec *domain.StepExecution) (*DispatchResult, error)
    Resume(ctx context.Context, exec *domain.StepExecution) (*DispatchResult, error)
    Cancel(ctx context.Context, exec *domain.StepExecution) (*DispatchResult, error)
}
```

建议把 runner 的持久回写结果冻结成：

```go
type DispatchResult struct {
    Status             domain.StepStatus `json:"status"`
    CheckpointRef      string            `json:"checkpoint_ref"`
    ResumeToken        string            `json:"resume_token"`
    RemoteExecutionRef string            `json:"remote_execution_ref"`
    OutputArtifactRefs []string          `json:"output_artifact_refs"`
    FailureCode        string            `json:"failure_code"`
    FailureMessage     string            `json:"failure_message"`
    LastHeartbeatAt    time.Time         `json:"last_heartbeat_at"`
    LeaseExpiresAt     time.Time         `json:"lease_expires_at"`
}
```

runner 只允许返回以下可持久结果：

- `running`：刷新 lease 并写 checkpoint
- `succeeded`：写输出 artifact 并关闭 step
- `failed`：写失败原因，并由 runtime 决定重试、回退或进入 gate
- `cancelled`：只允许出现在显式 cancel 路径

## 8. gate runtime

### 8.1 gate 挂点

gate 不是 step，而是 step 之间的治理对象。

v1 通用 gate 类型：

- `approval`
- `confirmation`
- `budget`
- `evaluation`

### 8.2 gate 触发规则

manifest 通过 `attach_after` / `attach_before` 声明 gate。

运行时行为：

1. step 完成
2. runtime 检查是否需要打开 gate
3. 如果是 `approval` / `confirmation` / `budget`，生成 `ApprovalRequest` 并进入 `WaitingHuman`
4. 如果是 `evaluation`，先基于 `evaluation_result` 自动判定继续 / 回退 / 结束
5. 只有 manifest 明确要求人工介入时，`evaluation` 才升级为新的人工 gate

`evaluation` gate 的自动判定不能写死在 Go 代码里，必须来自 manifest：

- 读取 `result_family`
- 逐条执行 `rules[*].metric/op/threshold`
- 按 `aggregate=all|any` 聚合
- 命中成功走 `on_pass`
- 未命中走 `on_fail`
- `evaluation_result` 缺失或 schema 不兼容时走 `on_error`

### 8.3 非 gate 型人类等待

`WaitingInput` 和 `WaitingRecovery` 不是 `ApprovalRequest`，而是独立的 durable 等待锚点：

- runtime 创建 `HumanWaitRecord`
- task 进入 `WaitingHuman`
- 新的人类输入先形成 `ExternalEvent`
- 只有在 `HumanWaitRecord` 仍 active 且 `waiting_reason` 仍匹配时，runtime 才允许恢复或回退

推荐规则：

- `WaitingInput` 至少绑定 `task_id + waiting_reason`，并尽量绑定 `step_execution_id`
- `WaitingRecovery` 必须绑定 `task_id + step_execution_id + waiting_reason`
- `HumanWaitRecord` 关闭后，对应回流事件只能留审计，不得再次恢复

## 9. `ContextPack` 与 `AgentDispatch`

### 9.1 `ContextPack`

`ContextPack` 是裁剪后的上下文快照，不允许把全量对话和全量内部状态直接塞给子 agent。

最小内容：

- request/task 摘要
- 当前 binding / step 引用
- 必要 artifact 引用
- 外部对象引用快照
- 工作状态引用
- digest

### 9.2 `AgentDispatch`

`AgentDispatch` 必须冻结：

- 谁发起
- 目标是什么
- 能用哪些 tool / MCP
- 写权限到哪里
- 预算和截止时间
- 结果回传到哪里

`AgentDispatch` 的生命周期必须独立可恢复：

- `created`
- `dispatched`
- `running`
- `completed`
- `failed`
- `cancelled`
- `expired`

如果 step 依赖外部 agent 平台长跑执行，则 `AgentDispatch.RemoteExecutionRef` 和 `ResumeToken` 必须与 `StepExecution` 同步写回。

### 9.3 `AgentRegistry`

```go
type AgentProfile struct {
    AgentID          string
    Labels           []string
    SupportedRoles   []domain.Role
    AllowedTools     []string
    AllowedMCP       []string
    SandboxTemplates []string
    CostClass        string
    Healthy          bool
}
```

运行时按槽位选型，不按固定 agent 名字写死。

## 10. 主写权限

默认规则：

- `reception` 在 request path 内没有 durable 写权限
- workflow 当前主写实例可以提交 step 结果
- 子 agent 只能返回候选 artifact
- merge / publish / control-plane apply 这类副作用，仍然通过 `outbox + MCP`

## 11. supersede 规则

以下情况会触发 supersede：

- 新 binding 替换旧 binding
- 新 artifact 替换旧 artifact
- 新 gate 替换旧 gate
- 新 step execution 取代过期 running step

superseded 对象只能保留审计价值，不能继续推进主路径。

## 12. 关键接口

```go
type Runtime interface {
    Bind(ctx context.Context, taskID string, manifest *Manifest, cause string) (*domain.WorkflowBinding, error)
    MarkReady(ctx context.Context, taskID string) error
    OpenGate(ctx context.Context, taskID string, gate GateSpec) (*domain.ApprovalRequest, error)
}

type AgentRuntime interface {
    RunDispatch(ctx context.Context, dispatch *domain.AgentDispatch) (*DispatchResult, error)
}
```

## 13. 测试建议

必须覆盖：

- manifest 规范化后 digest 稳定
- 候选 workflow 为 0 / 1 / 多个的裁决
- gate 作为对象而不是 step
- 子 agent 越权写 scope 被拒绝
- rebind 只追加新 binding，不覆盖旧 binding
