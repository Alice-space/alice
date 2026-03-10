# TDR 06: 计划生成、计划审核与仲裁

## 1. 目标

本文定义 `planning` 与 `plan_audit` 阶段的实现，包括计划对象、审核请求、租约、聚合与人工仲裁。

建议目录：

- `internal/workflow/planning.go`
- `internal/workflow/audit.go`
- `internal/agent/planner/`
- `internal/agent/plan_audit/`

## 2. PlanArtifact 模型

```go
type PlanArtifact struct {
    PlanArtifactID   string
    TaskID           string
    PlanVersion      uint32
    Summary          string
    Scope            []string
    NonGoals         []string
    Implementation   []PlanStep
    Risks            []RiskItem
    RequiresEval     bool
    EvalSpecID       string
    CreatedBy        string
    CreatedAt        time.Time
}

type PlanStep struct {
    StepID          string
    Title           string
    Description     string
    AcceptanceCheck []string
}
```

要求：

- `PlanVersion` 从 1 开始递增
- 每次计划边界变化都必须产出新版本
- `Summary` 必须适合直接回帖到 issue

## 3. Planner 输入输出

输入：

- 当前 issue 主体
- 最新 issue 评论摘要
- 必要代码上下文引用
- 当前任务预算和风险约束

输出：

- `PlanArtifact`
- 可选 `EvalSpec`
- 建议的审核席位配置

建议接口：

```go
type Planner interface {
    BuildPlan(ctx context.Context, req PlanBuildRequest) (PlanArtifact, *domain.EvalSpec, error)
}
```

## 4. 计划发布流程

```text
task in planning
  -> gather issue context
  -> planner generates PlanArtifact
  -> create outbox action(comment_issue)
  -> emit PlanPublished
  -> emit StartAuditRound(target=plan_version)
  -> state -> plan_audit
```

说明：

- issue 回帖不是状态推进前提；评论补发失败可由 outbox 恢复
- 真正触发 `plan_audit` 的是 BUS 中的 `PlanPublished` 事件

## 5. AuditRequest 模型

```go
type AuditRequest struct {
    AuditRequestID  string
    TaskID          string
    TargetType      domain.AuditTargetType
    TargetVersion   string
    Round           uint32
    ExpectedAgents  []string
    DeadlineAt      time.Time
    LeaseDuration   time.Duration
    AggregationMode string
    Status          string
}
```

第一版固定：

- `TargetType = plan`
- `TargetVersion = strconv(planVersion)`
- `AggregationMode = unanimity_or_reject`

## 6. 审核 Agent 协议

审核 Agent 必须遵循两阶段提交式回写：

1. `accepted`
2. `approve` / `reject` / `abstain`

建议 verdict 结构：

```go
type AuditVerdict struct {
    AuditRequestID string
    TaskID         string
    TargetType     domain.AuditTargetType
    TargetVersion  string
    Round          uint32
    AgentID        string
    Verdict        domain.AuditVerdictStatus
    Summary        string
    EvidenceRefs   []string
    SubmittedAt    time.Time
}
```

## 7. 租约机制

审核 Agent 一旦接单，必须周期续租。

建议实现：

- 接单即写 `accepted_at`
- 每 `lease_duration / 2` 续租一次
- `audit_lease_checker` 每 5 秒检查过期席位

过期规则：

- 过期席位视为 `missing`
- `missing` 参与本轮聚合，但不等价于自动通过

## 8. 聚合算法

聚合触发条件：

- 收齐全部 verdict
- 所有未完成席位都过期
- 到达 deadline

聚合伪代码：

```text
if any reject:
    result = reject
else if all approve:
    result = approve
else if round < 3:
    result = reject_conflict
else:
    result = arbitration
```

结果映射：

- `approve` -> `coding`
- `reject` / `reject_conflict` -> `planning`
- `arbitration` -> `pending_arbitration`

## 9. 人类补充与计划失效

如果 `plan_audit` 期间发生以下事件，当前审核必须失效：

- issue 新评论改变需求边界
- 风险、预算或指标发生变化
- Planner 重新发布新计划

实现动作：

1. 标记当前 `AuditRequest` 为 `superseded`
2. 旧 verdict 保留审计
3. Task 回到 `planning`

## 10. 仲裁请求

连续 3 轮冲突时生成 `ArbitrationRequest`：

```go
type ArbitrationRequest struct {
    ArbitrationRequestID string
    TaskID               string
    TargetType           domain.AuditTargetType
    TargetVersion        string
    TriggerRound         uint32
    DiffSummary          string
    ResolutionAction     string
    ExpiresAt            time.Time
    Status               string
}
```

仲裁结果按目标类型约束：

- 当 `TargetType=plan`：
  - `back_to_planning`
  - `continue_to_coding`
  - `cancel_task`
- 当 `TargetType=code`：
  - `continue_to_coding`
  - `resume_code_audit`
  - `approve_code`
  - `cancel_task`

其中：

- `approve_code` 不是直接等价于“立刻 merge”，而是表示人工仲裁认定代码审核已通过；后续仍由 merge gate 决定进入 `merge_approval_pending` 还是 `merging`
- `resume_code_audit` 用于人类要求保持当前 `pr_head_sha`，但回到代码审核阶段继续等待补充材料或后续门禁信号

## 11. issue 评论集成

计划阶段的 issue 评论按三类处理：

- 需求补充：更新上下文，不改状态或回到 `planning`
- 批准/驳回：如果来自人类卡片回流，转为结构化命令
- 非结构化讨论：仅写审计和上下文摘要

不要让 issue 评论直接越过 BUS 改 `plan_audit` 结果。

## 12. 最小测试矩阵

- 新 `plan_version` 自动使旧审核失效测试
- verdict 未收齐但首个 reject 到达不立即推进测试
- 两轮冲突回 `planning`，第三轮进仲裁测试
- 代码审核仲裁可产生 `resume_code_audit` 或 `approve_code` 测试
- Agent 失联后按 `missing` 聚合测试
- 仲裁目标版本 supersede 后旧仲裁失效测试
