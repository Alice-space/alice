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
    PRAction       string
    PRRef          domain.PRRef
    HeadSHA        string
    ChangeSummary  string
    NeedsEval      bool
    ReportOnly     bool
}
```

约束：

- 同一 `task_id` 同时只允许一个活动 PR
- 若平台要求新开 PR，旧 PR 必须标记为 `superseded`
- 不允许 force push 覆盖远端历史

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

## 7. 代码审核

代码审核与计划审核复用同一套聚合引擎，仅目标版本改为 `pr_head_sha`。

前置条件：

- 当前任务状态是 `code_audit`
- 当前 PR 非 superseded
- 若任务要求评测，则必须已有针对当前 `pr_head_sha` 的有效 `EvalResult`

聚合结果：

- `approve` + 门禁满足 -> `merge_approval_pending` 或 `merging`
- `approve` + 门禁未满足 -> 保持 `code_audit`
- `reject` -> `coding`
- 第 3 轮冲突 -> `pending_arbitration`

## 8. 合并门禁

门禁至少包括：

- 代码审核聚合为通过
- CI 状态通过
- 分支保护满足
- 仓库策略允许合并
- 若需要人工审批，则确认令牌已回流

实现建议：

```go
type MergeGate struct {
    AuditApproved     bool
    EvalSatisfied     bool
    CIState           string
    BranchProtected   bool
    NeedsApproval     bool
    ApprovalConfirmed bool
}
```

## 9. 报告型任务

如果任务策略明确为“不合主干，只产出报告”：

- 代码审核或评测收口后直接生成阶段报告
- 不进入 `merge_approval_pending` / `merging`
- 任务直接 `closed`

实现上应由 `Task.Type` 或策略字段显式标识，不靠自然语言猜测。

## 10. CancellationToken 传播

以下情况必须撤销活动 `CancellationToken`：

- 任务回退到 `planning`
- 任务进入 `cancelled`
- 任务进入 `budget_exceeded`
- 活动 PR 被 superseded

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
- CI 未满足时审核通过但不进入合并测试
- 报告型任务绕过合并直接关闭测试
- 取消任务时传播 cluster job cancel 测试
