# R49: Code Army Planning Phase — Integration

返回总览：[R49-plan-phase-design.md](./R49-plan-phase-design.md)

### 6.1 /alice approve-plan 命令

**文件**: `skills/alice-code-army/scripts/alice-code-army.sh` — `apply_command()`

新增命令分支：

```bash
elif [[ "$command_text" == "/alice approve-plan" ]]; then
    # 读取 campaign repo，检查 plan_status == plan_approved
    # 调用 expandMasterPlanToTasks (见 6.2)
    # 设置 plan_status = human_approved
    # 设置 campaign status = running
    summary="Plan approved by human, expanding tasks and starting execution"
    patch_campaign "$campaign_id" "$(jq -cn --arg status running --arg summary "$summary" '{status:$status,summary:$summary}')"
```

### 6.2 Master Plan → Tasks 展开

当人类 approve-plan 后，需要将 `plans/merged/master-plan.md` 中的任务列表展开为 `phases/Pxx/tasks/Txxx.md` 文件。

**实现方式**：这是 planner 的职责之一。在 planner_dispatch prompt 中要求 planner **同时**：
- 写 proposal markdown（供 reviewer 审查）
- 预生成 phase 和 task markdown 文件（status=draft）

当 human approve 后，reconcile 将所有 draft task 自动转为 ready（如依赖允许）。这样无需额外的"展开"逻辑。

**替代方案**（更简洁）：planner 只写 proposal，approve-plan 时由一个单独的 "plan expander" dispatch 将 proposal 展开为 task 文件。但这增加了一个额外的 dispatch 轮次。

**推荐方案**：planner 直接写 task 文件（status=draft），approve-plan 将 campaign plan_status 设为 human_approved，下一轮 reconcile 自动将 draft → ready。理由：
1. 减少一轮 dispatch 延迟
2. Planner reviewer 可以直接审查 task 文件的 write_scope 和依赖关系
3. 与现有 reconcile 流程无缝衔接

### 6.3 Draft → Ready 转换

在 `reconcilePlanPhase()` 中，当检测到 `plan_status == "human_approved"` 时：

```go
case "human_approved":
    // 一次性将所有 draft task 转为 ready
    promoted := promoteDraftTasksToReady(repo)
    if promoted > 0 {
        // 已展开，后续 reconcile 的执行阶段会自动接管
        return true, nil
    }
    return false, nil
```

`promoteDraftTasksToReady()` 遍历所有 task，将 status=draft 转为 status=ready 并持久化。

---

## 七、Campaign Repo 模板改造

### 7.1 campaign.md 模板

**文件**: `skills/alice-code-army/templates/campaign-repo/campaign.md`

新增 frontmatter 字段：

```yaml
default_planner:
  role: planner
  provider: claude
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: analytical
default_planner_reviewer:
  role: planner_reviewer
  provider: claude
  model: ""
  profile: ""
  workflow: code_army
  reasoning_effort: high
  personality: analytical
plan_round: 0
plan_status: idle
```

### 7.2 plans/ 模板简化

删除旧的多模型 proposal 模板（`claude-code.md`、`gpt.md`、`gemini-code.md`），替换为按轮次命名的模式：

```
plans/
├── proposals/
│   └── README.md          ← 说明命名规则：round-NNN-plan.md
├── reviews/
│   └── README.md          ← 说明命名规则：round-NNN-review.md
└── merged/
    └── master-plan.md     ← 最终批准的计划
```

### 7.3 Proposal 和 Review 模板

**新文件**: `skills/alice-code-army/templates/campaign-repo/_templates/plan-proposal.md`

```yaml
---
proposal_id: ""
plan_round: 1
status: draft
---

# Plan Proposal

## Analysis
- 待补充

## Phases
- 待补充

## Task Breakdown
- 待补充

## Risks
- 待补充
```

**新文件**: `skills/alice-code-army/templates/campaign-repo/_templates/plan-review.md`

```yaml
---
review_id: ""
plan_round: 1
reviewer:
  role: planner_reviewer
verdict: ""
blocking: false
created_at: ""
---

# Plan Review

## Summary
- 待补充

## Findings
- 待补充

## Verdict
- 待补充
```

---

## 八、Bootstrap 集成

### 8.1 Dispatch Task Sync

**文件**: `internal/bootstrap/campaign_repo_runtime.go`

`syncCampaignDispatchTasks()` 已经是通用的——它遍历所有 `DispatchTaskSpec` 并同步到 automation engine。由于 planner/planner_reviewer 的 dispatch spec 格式与 executor/reviewer 完全相同（都是 `DispatchTaskSpec`），**无需修改 sync 逻辑**。

Planner dispatch 的 `StateKey` 复用 `campaign_dispatch:` 前缀（因为它也是 dispatch），只是 Kind 字段不同。这样现有的 sync 逻辑完全不用改。对应地，section 三中 buildDispatchSpecs 的 StateKey 也应使用 `campaign_dispatch:` 而非 `campaign_plan:`。

### 8.2 Campaign Status Sync

`runCampaignRepoReconcile()` 已有逻辑将 `result.Summary` 写回 runtime campaign。计划阶段的 summary 会包含 plan 相关计数（plan_round, plan_status），通过 `SummaryLine()` 自然传播。

---

## 九、Summary 扩展

### 9.1 Summary 新增字段

**文件**: `internal/campaignrepo/repository.go` — `Summary` struct

```go
PlanRound  int    `json:"plan_round,omitempty"`
PlanStatus string `json:"plan_status,omitempty"`
```

### 9.2 SummaryLine 扩展

```go
func (s Summary) SummaryLine() string {
    parts := []string{
        fmt.Sprintf("plan=%s", blankForSummary(s.PlanStatus)),      // NEW
        fmt.Sprintf("plan_round=%d", s.PlanRound),                  // NEW
        fmt.Sprintf("phase=%s", blankForSummary(s.CurrentPhase)),
        // ... 现有字段 ...
    }
    return strings.Join(parts, " ")
}
```

### 9.3 LiveReport 扩展

在 `LiveReportMarkdown()` 的 Summary 部分添加计划状态：

```go
if s.PlanStatus != "" && s.PlanStatus != "human_approved" {
    lines = append(lines,
        fmt.Sprintf("- plan status: `%s` (round %d)", s.PlanStatus, s.PlanRound),
    )
}
```

---

## 十、Skill Script 扩展

### 10.1 新增 CLI 命令

**文件**: `skills/alice-code-army/scripts/alice-code-army.sh`

```
approve-plan    CAMPAIGN_ID              批准计划，进入执行阶段
plan-status     CAMPAIGN_ID              查看当前计划状态
```

### 10.2 apply-command 扩展

新增 `/alice approve-plan` 指令处理。

---

## 十一、文件变更清单

| 文件 | 变更类型 | 描述 |
|------|----------|------|
| `internal/campaign/model.go` | 修改 | 新增 StatusPlanning 等状态常量 |
| `internal/campaignrepo/repository.go` | 修改 | 新增 PlanProposalDocument、PlanReviewDocument 结构体；CampaignFrontmatter 新增 plan 字段；Repository 新增 plan 集合；Load() 扫描 plans/；Summary 新增 plan 字段 |
| `internal/campaignrepo/reconcile.go` | 修改 | 新增 DispatchKindPlanner/PlannerReviewer；新增 reconcilePlanPhase()、applyPlanVerdict() 等函数；buildDispatchSpecs() 添加 planner dispatch 逻辑 |
| `internal/campaignrepo/reconcile_plan.go` | **新建** | 计划阶段 reconcile 独立文件，包含 reconcilePlanPhase、applyPlanVerdict、promoteDraftTasksToReady、promotePlanToMasterPlan 等函数 |
| `prompts/campaignrepo/planner_dispatch.md.tmpl` | **新建** | Planner dispatch prompt 模板 |
| `prompts/campaignrepo/planner_reviewer_dispatch.md.tmpl` | **新建** | Planner reviewer dispatch prompt 模板 |
| `skills/alice-code-army/templates/campaign-repo/campaign.md` | 修改 | 添加 default_planner、default_planner_reviewer、plan_round、plan_status |
| `skills/alice-code-army/templates/campaign-repo/plans/proposals/` | 修改 | 删除旧的多模型模板，替换为 README |
| `skills/alice-code-army/templates/campaign-repo/_templates/plan-proposal.md` | **新建** | Proposal 模板 |
| `skills/alice-code-army/templates/campaign-repo/_templates/plan-review.md` | **新建** | Plan review 模板 |
| `skills/alice-code-army/scripts/alice-code-army.sh` | 修改 | 新增 approve-plan 命令和 /alice approve-plan 指令 |

---

## 十二、验证方案

1. **单元测试**：`internal/campaignrepo/` 新增测试文件，覆盖：
   - reconcilePlanPhase 各状态转换
   - applyPlanVerdict 各 verdict 路径
   - promoteDraftTasksToReady
   - resolvePlannerRole / resolvePlannerReviewerRole 级联
   - buildDispatchSpecs 包含 planner dispatch

2. **集成验证**（手动）：
   - 创建 campaign → 验证 plan_status=idle
   - 触发 reconcile → 验证自动转为 planning + dispatch planner
   - 模拟 planner 写入 proposal → 触发 reconcile → 验证转为 plan_review_pending + dispatch planner_reviewer
   - 模拟 reviewer 写入 concern → 验证 plan_round++ + 回到 planning
   - 模拟 reviewer 写入 approve → 验证 plan_status=plan_approved
   - 执行 /alice approve-plan → 验证 draft tasks 转为 ready + campaign status=running
   - 后续 reconcile → 验证执行阶段正常 dispatch

3. **Bash 语法检查**：`bash -n scripts/alice-code-army.sh`

4. **编译验证**：`go build ./...`、`go test ./internal/campaignrepo/...`
