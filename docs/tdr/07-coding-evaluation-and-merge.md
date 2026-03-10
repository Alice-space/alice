# TDR 07: 编码、评测、代码审核与合并

## 1. 目标

本文定义 `coding`、`evaluation`、`code_audit`、`merge_approval_pending`、`merging` 阶段的实现。

建议目录：

- `internal/workflow/coding.go`
- `internal/workflow/evaluation.go`
- `internal/workflow/merge.go`
- `internal/agent/coding/`
- `internal/agent/evaluation/`
- `internal/agent/code_audit/`

## 2. CodingRequest

```go
type CodingRequest struct {
    TaskID          string
    PlanArtifactID  string
    PlanVersion     uint32
    TargetIssueRef  domain.IssueRef
    ExistingPRRef   *domain.PRRef
}
```

前置条件：

- 任务状态必须是 `coding`
- 当前计划审核已通过
- 当前 `plan_version` 必须等于 `Task.ActivePlan.PlanVersion`

## 3. CodingAgent 输出

`CodingAgent` 必须输出结构化结果，而不是仅返回自然语言。

```go
type CodingResult struct {
    PRAction      string
    PRRef         domain.PRRef
    HeadSHA       string
    ChangeSummary string
    NeedsEval     bool
}
```

约束：

- 同一 `task_id` 同时只允许一个活动 PR
- 若平台要求新开 PR，旧 PR 必须标记为 `superseded`
- 不允许 force push 覆盖远端历史
- `CodingAgent` 不得输出或改写 `DeliveryMode`；它只能遵循当前 `Task.DeliveryMode`

## 4. PR 管理规则

### 4.1 建立或更新 PR

流程：

1. `CodingAgent` 在受限环境中修改代码
2. 通过 Git 平台 MCP 创建或更新 PR
3. BUS 记录 `PRAttached`
4. 根据 `requires_evaluation` 决定进入 `evaluation` 或 `code_audit`

### 4.2 非快进冲突

更新 PR 时：

- 先尝试自动 rebase
- rebase 成功则继续
- rebase 冲突则回到 `coding`
- 禁止 force push

### 4.3 `pr_head_sha` 变化

一旦 `pr_head_sha` 变化：

- 旧代码审核结论失效
- 旧评测结果失效
- 必须重新进入 `evaluation` 或 `code_audit`

## 5. EvalSpec 与 Evaluation

### 5.1 EvalSpec

```go
type EvalSpec struct {
    EvalSpecID       string
    TaskID           string
    Version          uint32
    DatasetVersion   string
    ConfigVersion    string
    BaselineRef      string
    Thresholds       map[string]float64
    MaxIterations    uint32
    BudgetLimit      BudgetLimit
    ResourceSpec     ResourceSpec
    SeedPolicy       string
    ImageDigest      string
}
```

### 5.2 EvalJob

```go
type EvalJob struct {
    EvalJobID        string
    TaskID           string
    EvalSpecID       string
    TargetPRHeadSHA  string
    ExternalJobID    string
    Status           string
    MetricsSummary   map[string]float64
    ArtifactRefs     []string
}
```

### 5.3 评测流程

```text
coding completed
  -> state = evaluation
  -> create outbox(start_cluster_job)
  -> Cluster MCP submit job
  -> poll / callback
  -> record EvalResult
  -> pass -> code_audit
  -> fail -> coding
  -> budget fuse -> budget_exceeded
```

## 6. UsageLedger 与预算熔断

```go
type UsageLedger struct {
    TaskID           string
    TokenCost        decimal.Decimal
    ModelCost        decimal.Decimal
    CPUSeconds       int64
    GPUSeconds       int64
    BudgetRemaining  decimal.Decimal
    HardLimitReached bool
}
```

规则：

- 每次模型调用和评测作业更新都要增量写账本
- `BudgetRemaining < 0` 或命中策略硬上限时，立刻触发 `TripBudgetFuse`
- 熔断后不得再创建新的编码和评测请求

## 7. 代码审核聚合

代码审核与计划审核复用同一套聚合引擎，仅目标版本改为 `pr_head_sha`。

前置条件：

- 当前任务状态是 `code_audit`
- 当前 PR 非 superseded
- 若任务要求评测，则必须已有针对当前 `pr_head_sha` 的有效 `EvalResult`

实现建议：

```go
type CodeAuditOutcome struct {
    Verdict         string
    TargetPRHeadSHA string
    Round           uint32
}
```

说明：

- `CodeAuditOutcome` 只表达“当前代码版本是否被审核接受”
- 它不负责表达是否应该合并，也不负责表达报告型任务是否应直接关闭

聚合结果：

- `approve` -> 进入后续策略判断
- `reject` -> `coding`
- 第 3 轮冲突 -> `pending_arbitration`
- `pending_arbitration` 对代码阶段给出 `approve_code` 时，视为当前版本得到 `approve`

## 8. 真实合并门禁

门禁至少包括：

- CI 状态通过
- 分支保护满足
- 仓库策略允许合并
- 是否需要人工审批
- 若已经处于 `merge_approval_pending`，则确认令牌是否已回流

实现建议：

```go
type MergeReadiness struct {
    CIState             string
    BranchProtected     bool
    RepoPolicySatisfied bool
    NeedsApproval       bool
    ApprovalConfirmed   bool
}
```

状态推进规则：

- 只有 `Task.DeliveryMode=merge` 且 `CodeAuditOutcome.Verdict=approve` 时，才评估 `MergeReadiness`
- `MergeReadiness` 不携带 `DeliveryMode`；是否合并由 `Task.DeliveryMode` 单独决定
- `NeedsApproval=true` 且 `ApprovalConfirmed=false` 时，不得停留在“可直接合并”的分支，必须进入 `merge_approval_pending`
- `ApprovalConfirmed=true` 只影响 `merge_approval_pending -> merging`，不应成为进入 `merge_approval_pending` 的前置条件
- 若 `CodeAuditOutcome.Verdict=approve` 但 CI、分支保护或仓库策略未满足，则保持 `code_audit`
- 若 `pending_arbitration` 对代码审核给出 `approve_code`，则对当前版本重新评估 `MergeReadiness`，推进到 `merge_approval_pending` 或 `merging`

## 9. 报告型关闭策略

如果任务策略明确为“不合主干，只产出报告”：

- 代码审核或评测收口后直接生成阶段报告
- 不进入 `merge_approval_pending` / `merging`
- 任务直接 `closed`

关闭规则：

- 只有 `Task.DeliveryMode=report_only` 且 `CodeAuditOutcome.Verdict=approve` 时，才允许 `code_audit -> closed`
- `approve_code` 仲裁结果也复用同一关闭规则
- 报告型关闭策略不读取 `MergeReadiness`，因为它不驱动真实合并

实现上必须由 `Task.DeliveryMode=report_only` 显式标识，不靠 `Task.Type` 或自然语言猜测。
`DeliveryMode` 应在计划阶段由策略层或人类确认写入 `Task`，并随当前 `plan_version` 一起生效；如果模式发生变化，应视为重新规划而不是在 `code_audit` 临时改口。

## 10. CancellationToken 传播

以下情况必须撤销活动 `CancellationToken`：

- 任务回退到 `planning`
- 任务进入 `waiting_human`
- 任务进入 `merge_approval_pending`
- 任务进入 `pending_arbitration`
- 任务进入 `failed`
- 任务进入 `cancelled`
- 任务进入 `budget_exceeded`
- 活动 PR 被 superseded

原则：

- 只要任务进入“等待人类、等待仲裁、终态失败、预算熔断”这类不应继续后台执行的状态，就必须先撤销 token，再做状态推进
- merge 审批等待期间不应继续保留旧的 Coding / Evaluation 后台执行

传播目标：

- 正在运行的 CodingAgent
- 正在运行的 EvaluationAgent
- 相关 Cluster 作业取消动作
- 可能还在排队的 outbox 动作

## 11. 人工恢复

`waiting_human` 后的恢复规则：

- 只追加预算且代码未变：回 `evaluation`
- 要求继续修改实现：回 `coding`
- 改目标、指标或数据集：回 `planning`
- 合并审批拒绝：回 `coding` 或 `cancelled`

## 12. 最小测试矩阵

- 同一任务只保留一个活动 PR 测试
- `pr_head_sha` 变化导致旧评测和旧审核失效测试
- 预算熔断阻止新评测测试
- `NeedsApproval=true` 且门禁满足时进入 `merge_approval_pending` 测试
- `merge_approval_pending` 在确认回流后进入 `merging` 测试
- CI 未满足时审核通过但不进入合并测试
- `CodingAgent` 输出不影响 `Task.DeliveryMode` 测试
- 仅 `DeliveryMode=report_only` 的任务允许 `code_audit -> closed` 测试
- `report_only` 任务审核通过后不评估 `MergeReadiness` 测试
- 进入 `waiting_human` / `pending_arbitration` / `failed` 时撤销 `CancellationToken` 测试
- 取消任务时传播 cluster job cancel 测试
