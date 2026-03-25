# R49: Code Army Planning Phase — Reconcile

返回总览：[R49-plan-phase-design.md](./R49-plan-phase-design.md)

### 3.3 reconcilePlanPhase() — 核心新增函数

```go
func reconcilePlanPhase(repo *Repository, now time.Time, leaseDuration time.Duration) (bool, error) {
    planStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)

    switch planStatus {
    case "human_approved":
        // 人类已批准，不再处理计划阶段
        return false, nil

    case "idle", "":
        // campaign 刚创建或未启动计划
        // 自动启动：设置 plan_status = "planning", plan_round = 1
        repo.Campaign.Frontmatter.PlanStatus = "planning"
        repo.Campaign.Frontmatter.PlanRound = 1
        persistCampaignDocument(repo)
        return true, nil

    case "planning":
        // 检查本轮是否已有 submitted 的 proposal
        // 如果没有 → 需要 dispatch planner（通过 buildDispatchSpecs 处理）
        // 如果有 → 转为 plan_review_pending
        if hasSubmittedProposal(repo.PlanProposals, repo.Campaign.Frontmatter.PlanRound) {
            repo.Campaign.Frontmatter.PlanStatus = "plan_review_pending"
            persistCampaignDocument(repo)
            return true, nil
        }
        return false, nil

    case "plan_review_pending":
        // 检查本轮是否已有 review verdict
        // 如果有 → 应用 verdict
        verdict, review := latestPlanReview(repo.PlanReviews, repo.Campaign.Frontmatter.PlanRound)
        if verdict == "" {
            // 还没有 review → 需要 dispatch planner_reviewer（通过 buildDispatchSpecs 处理）
            return false, nil
        }
        return applyPlanVerdict(repo, verdict, review)

    case "plan_reviewing":
        // planner_reviewer 正在工作，检查是否已写入 review
        verdict, review := latestPlanReview(repo.PlanReviews, repo.Campaign.Frontmatter.PlanRound)
        if verdict == "" {
            return false, nil  // 还在审查中
        }
        return applyPlanVerdict(repo, verdict, review)

    case "plan_approved":
        // 等待人类批准，reconcile 不做任何事
        return false, nil
    }

    return false, nil
}
```

### 3.4 applyPlanVerdict()

```go
func applyPlanVerdict(repo *Repository, verdict string, review PlanReviewDocument) (bool, error) {
    switch normalizeReviewVerdict(verdict, review.Frontmatter.Blocking) {
    case "approve":
        // planner_reviewer 批准 → 等待人类
        repo.Campaign.Frontmatter.PlanStatus = "plan_approved"
        // 将最新 proposal 复制/链接到 plans/merged/master-plan.md
        if err := promotePlanToMasterPlan(repo); err != nil {
            return false, err
        }
        persistCampaignDocument(repo)
        return true, nil

    case "concern":
        // 需要修改，开启新一轮
        repo.Campaign.Frontmatter.PlanRound++
        repo.Campaign.Frontmatter.PlanStatus = "planning"
        // 标记当前 proposal 为 superseded
        markCurrentProposalSuperseded(repo)
        persistCampaignDocument(repo)
        return true, nil

    case "blocking", "reject":
        // 严重问题，campaign 暂停
        repo.Campaign.Frontmatter.PlanStatus = "planning"  // blocking 也回到 planning，让 planner 重试
        repo.Campaign.Frontmatter.PlanRound++
        persistCampaignDocument(repo)
        return true, nil
    }
    return false, nil
}
```

### 3.5 buildDispatchSpecs 扩展

在现有 executor/reviewer dispatch 逻辑**之前**，添加 planner/planner_reviewer dispatch：

```go
func buildDispatchSpecs(repo Repository, now time.Time) ([]DispatchTaskSpec, error) {
    var specs []DispatchTaskSpec

    // ===== 计划阶段 dispatch =====
    planStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)

    if planStatus == "planning" {
        // 检查本轮没有 submitted proposal → 需要 dispatch planner
        if !hasSubmittedProposal(repo.PlanProposals, repo.Campaign.Frontmatter.PlanRound) {
            role := resolvePlannerRole(repo)
            prompt, err := buildPlannerDispatchPrompt(repo, role)
            if err != nil {
                return nil, err
            }
            specs = append(specs, DispatchTaskSpec{
                StateKey: fmt.Sprintf("campaign_dispatch:%s:planner:r%d", campaignID, repo.Campaign.Frontmatter.PlanRound),
                Kind:     DispatchKindPlanner,
                TaskID:   fmt.Sprintf("plan-r%d", repo.Campaign.Frontmatter.PlanRound),
                Title:    fmt.Sprintf("campaign planner %s r%d", campaignID, planRound),
                RunAt:    now,
                Prompt:   prompt,
                Role:     role,
            })
        }
    }

    if planStatus == "plan_review_pending" {
        // 检查本轮没有 review → 需要 dispatch planner_reviewer
        if !hasPlanReview(repo.PlanReviews, repo.Campaign.Frontmatter.PlanRound) {
            role := resolvePlannerReviewerRole(repo)
            prompt, err := buildPlannerReviewerDispatchPrompt(repo, role)
            if err != nil {
                return nil, err
            }
            specs = append(specs, DispatchTaskSpec{
                StateKey: fmt.Sprintf("campaign_dispatch:%s:planner_reviewer:r%d", campaignID, planRound),
                Kind:     DispatchKindPlannerReviewer,
                TaskID:   fmt.Sprintf("plan-review-r%d", planRound),
                Title:    fmt.Sprintf("campaign planner reviewer %s r%d", campaignID, planRound),
                RunAt:    now,
                Prompt:   prompt,
                Role:     role,
            })
        }
    }

    // ===== 执行阶段 dispatch（现有逻辑不变）=====
    for _, task := range repo.Tasks {
        // ... 现有 executor/reviewer 逻辑 ...
    }

    return specs, nil
}
```

---

## 四、角色解析

### 4.1 新增默认角色

**文件**: `internal/campaignrepo/reconcile.go`

```go
func defaultPlannerRoleConfig() RoleConfig {
    return RoleConfig{
        Role:            "planner",
        Provider:        "claude",
        Workflow:        "code_army",
        ReasoningEffort: "high",
        Personality:     "analytical",
    }
}

func defaultPlannerReviewerRoleConfig() RoleConfig {
    return RoleConfig{
        Role:            "planner_reviewer",
        Provider:        "claude",
        Workflow:        "code_army",
        ReasoningEffort: "high",
        Personality:     "analytical",
    }
}

func resolvePlannerRole(repo Repository) RoleConfig {
    return resolveRoleConfig(defaultPlannerRoleConfig(), repo.Campaign.Frontmatter.DefaultPlanner, RoleConfig{}, "planner")
}

func resolvePlannerReviewerRole(repo Repository) RoleConfig {
    return resolveRoleConfig(defaultPlannerReviewerRoleConfig(), repo.Campaign.Frontmatter.DefaultPlannerReviewer, RoleConfig{}, "planner_reviewer")
}
```

注意：planner 角色只有两级级联（系统默认 → campaign 级），不像 executor/reviewer 那样有 task 级覆盖，因为 plan 阶段没有 task 粒度的配置。

---

## 五、Prompt 模板

### 5.1 Planner Dispatch Prompt

**新文件**: `prompts/campaignrepo/planner_dispatch.md.tmpl`

模板变量：

| 变量 | 来源 |
|------|------|
| `CampaignRepo` | repo.Root |
| `CampaignFile` | campaign.md path |
| `Objective` | campaign frontmatter |
| `SourceRepos` | campaign frontmatter |
| `PlanRound` | campaign frontmatter |
| `PlannerRole` | resolved role label |
| `PlannerReviewerRole` | resolved role label |
| `PreviousProposalPath` | 上一轮 proposal（如有）|
| `PreviousReviewPath` | 上一轮 review（如有）|
| `ProposalOutputPath` | 建议写入路径 |

核心指令：
1. 读取 campaign.md 了解目标和约束
2. 读取 source_repos 了解仓库结构（目录结构、主要模块、关键文件）
3. 如果是 round > 1，读取上一轮 review 的反馈
4. 输出 proposal markdown 到指定路径，包含：
   - 分析（仓库现状、瓶颈、风险）
   - 分阶段计划（每阶段有目标和验收标准）
   - 任务列表（每任务有 task_id、title、depends_on、target_repos、write_scope）
   - 验收标准
5. 设置 proposal frontmatter status 为 `submitted`
6. 不修改 source repo，不修改 campaign.md

### 5.2 Planner Reviewer Dispatch Prompt

**新文件**: `prompts/campaignrepo/planner_reviewer_dispatch.md.tmpl`

模板变量：

| 变量 | 来源 |
|------|------|
| `CampaignRepo` | repo.Root |
| `CampaignFile` | campaign.md path |
| `Objective` | campaign frontmatter |
| `SourceRepos` | campaign frontmatter |
| `PlanRound` | campaign frontmatter |
| `ProposalPath` | 当前轮 proposal 路径 |
| `ReviewerRole` | resolved role label |
| `ReviewOutputPath` | 建议写入路径 |

核心指令：
1. 读取 campaign.md 了解目标和约束
2. 读取 proposal 审查可行性
3. 可选读取 source repo 验证 proposal 对仓库结构的理解是否正确
4. 输出 review markdown 到指定路径，包含：
   - 对 proposal 每个阶段/任务的评价
   - write_scope 是否合理（不遗漏、不冲突）
   - 依赖关系是否正确
   - 是否遗漏重要模块
   - verdict: approve / concern / blocking
5. 不修改 source repo，不修改 campaign.md，不修改 proposal

---

## 六、Human Gate 与 Plan Approval

