# R49: Code Army Planning Phase — 详细设计

## Context

当前 Code Army 只有 executor → reviewer 两阶段。SKILL.md 描述的规划阶段（planner 出方案 → reviewer 审查 → 多轮迭代 → 人类批准 → 展开执行）没有运行时代码支撑。这意味着用户必须手动编写计划和任务文件，skill 描述的自动化承诺落空。

本设计引入 **planner + planner_reviewer** 两个角色，形成三阶段流程。角色与模型完全解耦——planner/planner_reviewer 只是角色标签，具体绑定哪个 provider/model 由 runtime `config.yaml` 的 `campaign_role_defaults` + `llm_profiles` 决定。

## 设计原则

1. **最小侵入**：复用现有 reconcile → dispatch → automation 管线，不新建引擎
2. **Repo 仍是真相源**：计划文档、审查文档都是 campaign repo 里的 markdown 文件
3. **对称结构**：planner/planner_reviewer 与 executor/reviewer 共享相同的 claim → dispatch → verdict 模式
4. **Human gate 是硬门槛**：planner_reviewer approve 后必须等人类明确批准，不自动进入执行

---

## 一、Campaign 级别状态扩展

### 1.1 Runtime Campaign 状态

**文件**: `internal/campaign/model.go`

新增状态常量：

```go
StatusPlanning              CampaignStatus = "planning"
StatusPlanReviewPending     CampaignStatus = "plan_review_pending"      // planner 完成，等待 reviewer dispatch
StatusPlanApproved          CampaignStatus = "plan_approved"            // planner_reviewer approve，等人类批准
```

完整状态机：

```
planned → planning → plan_review_pending → planning (concern 循环)
                                         → plan_approved (approve)
                                         → plan_approved → running (人类 /alice approve-plan)
                                         → hold / canceled (随时可中断)
```

### 1.2 Campaign Repo Frontmatter 扩展

**文件**: `internal/campaignrepo/repository.go` — `CampaignFrontmatter` struct

新增字段：

```go
PlanRound              int        `yaml:"plan_round" json:"plan_round,omitempty"`
PlanStatus             string     `yaml:"plan_status" json:"plan_status,omitempty"`
// plan_status: "idle" | "planning" | "plan_review_pending" | "plan_reviewing" | "plan_approved" | "human_approved"
```

说明：早期方案曾把 planner / planner_reviewer 默认模型写进 `campaign.md` frontmatter；当前实现已经收敛为只保留 `plan_round` / `plan_status`，角色到模型的映射统一由 `config.yaml` 管理。

### 1.3 shouldAutoReconcileCampaign 扩展

**文件**: `internal/bootstrap/campaign_repo_runtime.go`

`shouldAutoReconcileCampaign()` 已经对 `planned`、`running`、`hold` 返回 true。新增的 `planning`、`plan_review_pending`、`plan_approved` 同属非终态，自动被 default 分支覆盖，**无需额外改动**。

---

## 二、Plan 文档模型

### 2.1 新结构体

**文件**: `internal/campaignrepo/repository.go`

```go
type PlanProposalDocument struct {
    Path        string                  `json:"path"`
    Body        string                  `json:"body,omitempty"`
    Frontmatter PlanProposalFrontmatter `json:"frontmatter"`
}

type PlanProposalFrontmatter struct {
    ProposalID  string `yaml:"proposal_id" json:"proposal_id,omitempty"`
    PlanRound   int    `yaml:"plan_round" json:"plan_round,omitempty"`
    Status      string `yaml:"status" json:"status,omitempty"`
    // status: "draft" | "submitted" | "review_pending" | "approved" | "rejected" | "superseded"
}

type PlanReviewDocument struct {
    Path        string                 `json:"path"`
    Body        string                 `json:"body,omitempty"`
    Frontmatter PlanReviewFrontmatter  `json:"frontmatter"`
    CreatedAt   time.Time              `json:"created_at,omitempty"`
}

type PlanReviewFrontmatter struct {
    ReviewID     string     `yaml:"review_id" json:"review_id,omitempty"`
    PlanRound    int        `yaml:"plan_round" json:"plan_round,omitempty"`
    Reviewer     RoleConfig `yaml:"reviewer" json:"reviewer,omitempty"`
    Verdict      string     `yaml:"verdict" json:"verdict,omitempty"`
    // verdict: "approve" | "concern" | "blocking" | "reject"
    Blocking     bool       `yaml:"blocking" json:"blocking,omitempty"`
    CreatedAtRaw string     `yaml:"created_at" json:"created_at,omitempty"`
}
```

### 2.2 Repository 扩展

```go
type Repository struct {
    Root           string                 `json:"root"`
    Campaign       CampaignDocument       `json:"campaign"`
    Phases         []PhaseDocument        `json:"phases,omitempty"`
    Tasks          []TaskDocument         `json:"tasks,omitempty"`
    Reviews        []ReviewDocument       `json:"reviews,omitempty"`
    PlanProposals  []PlanProposalDocument  `json:"plan_proposals,omitempty"`   // NEW
    PlanReviews    []PlanReviewDocument    `json:"plan_reviews,omitempty"`     // NEW
}
```

### 2.3 Load() 扩展

`Load()` 新增两个扫描步骤：

```go
repo.PlanProposals, err = loadPlanProposalDocuments(absRoot)  // 扫描 plans/proposals/*.md
repo.PlanReviews, err = loadPlanReviewDocuments(absRoot)      // 扫描 plans/reviews/*.md
```

---

## 三、计划阶段 Reconcile 逻辑

### 3.1 DispatchKind 扩展

**文件**: `internal/campaignrepo/reconcile.go`

```go
const (
    DispatchKindExecutor        DispatchKind = "executor"
    DispatchKindReviewer        DispatchKind = "reviewer"
    DispatchKindPlanner         DispatchKind = "planner"          // NEW
    DispatchKindPlannerReviewer DispatchKind = "planner_reviewer" // NEW
)
```

### 3.2 ReconcileAndPrepare 扩展

在现有流程**之前**插入计划阶段 reconcile：

```go
func ReconcileAndPrepare(root string, now time.Time, maxParallel int, leaseDuration time.Duration) (ReconcileResult, error) {
    // ... 现有 load 逻辑 ...

    // ===== 计划阶段 reconcile（在执行阶段之前）=====
    planChanged, err := reconcilePlanPhase(&repo, now, leaseDuration)
    if err != nil {
        return ReconcileResult{}, err
    }
    if planChanged {
        changed = true
    }

    // 如果 campaign 还在计划阶段，跳过执行阶段 reconcile
    if isPlanningPhase(repo.Campaign.Frontmatter.PlanStatus) {
        summary := Summarize(repo, now, maxParallel)
        dispatchTasks, err := buildDispatchSpecs(repo, now)
        // ... return ...
    }

    // ===== 执行阶段 reconcile（现有逻辑不变）=====
    appliedReviews, err := applyReviewVerdicts(&repo)
    // ...
}
```
