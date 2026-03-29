package campaignrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateRepositoryForApproval_RequiresRefinedTaskPackages(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: plan_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: dev
base_commit: ""
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Incomplete task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope: [src/core]
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "context.md"), "")
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-001-plan.md"), `---
proposal_id: "plan-r1"
plan_round: 1
status: submitted
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "reviews", "round-001-review.md"), `---
review_id: "plan-review-r1"
plan_round: 1
verdict: approve
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: draft
human_approved: false
---

# Master Plan

## Merge Summary
- ready
`)

	_, validation, err := ValidateForApproval(root)
	if err != nil {
		t.Fatalf("validate for approval failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected incomplete task package to fail approval lint")
	}
	var found bool
	for _, issue := range validation.Issues {
		if strings.Contains(issue.Code, "task_context") || strings.Contains(issue.Code, "task_goal") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected refined task package issue, got %+v", validation.Issues)
	}
}

func TestApprovePlan_RequiresReviewAndMarksMasterPlanApproved(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	headCommit := gitHeadCommit(t, root)
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: plan_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Refined task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope: [src/core]
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-001-plan.md"), `---
proposal_id: "plan-r1"
plan_round: 1
status: submitted
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "reviews", "round-001-review.md"), `---
review_id: "plan-review-r1"
plan_round: 1
verdict: approve
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: draft
human_approved: false
---

# Master Plan

## Merge Summary
- refined and ready

## Phases
- P01
`)

	repo, validation, err := ApprovePlan(root)
	if err != nil {
		t.Fatalf("approve plan failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected validation success, got %+v", validation.Issues)
	}
	if repo.Campaign.Frontmatter.PlanStatus != PlanStatusHumanApproved {
		t.Fatalf("expected human_approved, got %s", repo.Campaign.Frontmatter.PlanStatus)
	}
	raw, err := os.ReadFile(filepath.Join(root, "plans", "merged", "master-plan.md"))
	if err != nil {
		t.Fatalf("read master plan failed: %v", err)
	}
	if !strings.Contains(string(raw), "human_approved: true") {
		t.Fatalf("expected master plan to be marked human approved, got %s", string(raw))
	}
}

func TestValidateRepositoryForApproval_RejectsDeprecatedPlannedTaskStatus(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	headCommit := gitHeadCommit(t, root)
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: plan_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Refined task"
phase: P01
status: planned
target_repos: [repo-a]
write_scope: [src/core]
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-001-plan.md"), `---
proposal_id: "plan-r1"
plan_round: 1
status: submitted
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "reviews", "round-001-review.md"), `---
review_id: "plan-review-r1"
plan_round: 1
verdict: approve
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: draft
human_approved: false
---

# Master Plan

## Merge Summary
- refined and ready

## Phases
- P01
`)

	_, validation, err := ValidateForApproval(root)
	if err != nil {
		t.Fatalf("validate for approval failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected deprecated planned task status to fail approval lint")
	}
	found := false
	for _, issue := range validation.Issues {
		if issue.Code == "task_status_planned_deprecated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected planned-status issue, got %+v", validation.Issues)
	}
}

func TestRejectPlan_ResetsPlanningRoundAndMarksMasterPlanRejected(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 3
plan_status: plan_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-003-plan.md"), `---
proposal_id: "plan-r3"
plan_round: 3
status: submitted
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: draft
human_approved: false
---

# Master Plan

- ready
`)

	repo, err := RejectPlan(root)
	if err != nil {
		t.Fatalf("reject plan failed: %v", err)
	}
	if repo.Campaign.Frontmatter.PlanStatus != PlanStatusPlanning {
		t.Fatalf("expected planning, got %s", repo.Campaign.Frontmatter.PlanStatus)
	}
	if repo.Campaign.Frontmatter.PlanRound != 4 {
		t.Fatalf("expected plan_round=4, got %d", repo.Campaign.Frontmatter.PlanRound)
	}
	if repo.PlanProposals[0].Frontmatter.Status != "superseded" {
		t.Fatalf("expected proposal superseded, got %s", repo.PlanProposals[0].Frontmatter.Status)
	}
	raw, err := os.ReadFile(filepath.Join(root, "plans", "merged", "master-plan.md"))
	if err != nil {
		t.Fatalf("read master plan failed: %v", err)
	}
	if !strings.Contains(string(raw), "status: rejected") {
		t.Fatalf("expected master plan rejected, got %s", string(raw))
	}
	if !strings.Contains(string(raw), "human_rejected: true") {
		t.Fatalf("expected human_rejected flag, got %s", string(raw))
	}
}

func TestValidateRepositoryForApproval_MasterPlanMayMentionHistoricalPlaceholder(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	headCommit := gitHeadCommit(t, root)
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 2
plan_status: plan_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Refined task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope: [src/core]
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-002-plan.md"), `---
proposal_id: "plan-r2"
plan_round: 2
status: submitted
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "reviews", "round-002-review.md"), `---
review_id: "plan-review-r2"
plan_round: 2
verdict: approve
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: submitted
human_approved: false
---

# Master Plan

## Changes from Round 1
> previous draft called this section 待补充

## Merge Summary
- refined and ready

## Phases
- P01
`)

	_, validation, err := ValidateForApproval(root)
	if err != nil {
		t.Fatalf("validate for approval failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected approval validation success, got %+v", validation.Issues)
	}
}

func TestValidateRepository_RejectsNonCodeArmyTaskWorkflow(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	headCommit := gitHeadCommit(t, root)
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: planning
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Refined task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope: [src/core]
executor:
  role: executor
  workflow: code
reviewer:
  role: reviewer
  workflow: code-review
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected invalid task workflows to fail validation")
	}
	var found bool
	for _, issue := range validation.Issues {
		if issue.Code == "task_role_workflow_invalid" && strings.Contains(issue.Message, "workflow=code_army") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task workflow validation issue, got %+v", validation.Issues)
	}
}

func TestValidateRepository_RejectsTaskFileRefsOutsideWriteScope(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	headCommit := gitHeadCommit(t, root)
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: planning
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Refined task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope:
  - src/token.rs
  - src/lib.rs
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- src/token.rs returns structured tokens

## Deliverables
- src/token.rs
- src/lib.rs
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "plan.md"), `# Execution Plan

## Execution Steps
1. Update src/token.rs.
2. Also change src/ast.rs if needed.

## Validation
- cargo build

## Handoff
- done
`)

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected missing write_scope coverage to fail validation")
	}
	var found bool
	for _, issue := range validation.Issues {
		if issue.Code == "task_write_scope_incomplete" && strings.Contains(issue.Message, "src/ast.rs") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task_write_scope_incomplete issue, got %+v", validation.Issues)
	}
}

func TestValidateRepository_RejectsReviewPendingTaskWithUnknownExecutionArtifacts(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source root failed: %v", err)
	}
	initGitRepo(t, sourceRoot)
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Broken execution"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
head_commit: "deadbeef"
last_run_path: "results/report-snippet.md"
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "report-snippet.md"), "# Summary\n")

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected invalid execution artifacts to fail validation")
	}
	var foundHeadCommit bool
	for _, issue := range validation.Issues {
		if issue.Code == "task_head_commit_unknown" {
			foundHeadCommit = true
		}
	}
	if !foundHeadCommit {
		t.Fatalf("expected execution artifact issues, got %+v", validation.Issues)
	}
}

func TestValidateRepository_AllowsCampaignOnlyReviewPendingTaskWithoutHeadCommit(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source root failed: %v", err)
	}
	initGitRepo(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: main
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Campaign-only review"
phase: P01
status: review_pending
target_repos: [repo-a]
write_scope: [campaign:phases/P01/tasks/T001/**]
last_run_path: "results/report-snippet.md"
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the campaign artifacts
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "report-snippet.md"), "# Summary\n")

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected campaign-only review_pending task to validate, got %+v", validation.Issues)
	}
}

func TestValidateRepository_AllowsReviewPendingTaskWithRepoScopedWorkingBranch(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source root failed: %v", err)
	}
	initGitRepo(t, sourceRoot)
	runGitOrFail(t, sourceRoot, "checkout", "-b", "codearmy/t001")
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "pub fn v1() {}\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "t1")
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Repo-scoped working branch"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [repo-a:codearmy/t001]
write_scope: [repo-a:src/lib.rs]
base_commit: "`+headCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/report-snippet.md"
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "report-snippet.md"), "# Summary\n")

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected repo-scoped working branch to validate, got %+v", validation.Issues)
	}
}

func TestValidateRepository_AllowsReviewPendingTaskWhenRepoScopedWriteScopeCoversDiff(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source root failed: %v", err)
	}
	initGitRepo(t, sourceRoot)
	runGitOrFail(t, sourceRoot, "checkout", "-b", "codearmy/t001")
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "pub fn v1() {}\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "t1")
	baseCommit := gitHeadCommit(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "pub fn v2() {}\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "t2")
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: main
base_commit: "`+baseCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Repo-scoped diff"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [repo-a:codearmy/t001]
write_scope: [repo-a:src/lib.rs]
base_commit: "`+baseCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/report-snippet.md"
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "report-snippet.md"), "# Summary\n")

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected repo-scoped write_scope to cover diff, got %+v", validation.Issues)
	}
}

func TestValidateRepository_RejectsReviewPendingTaskWhenDiffEscapesWriteScope(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source root failed: %v", err)
	}
	initGitRepo(t, sourceRoot)
	runGitOrFail(t, sourceRoot, "checkout", "-b", "codearmy/t001")
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "pub fn v1() {}\n")
	mustWriteTestFile(t, filepath.Join(sourceRoot, "Cargo.lock"), "version = 3\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs", "Cargo.lock")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "t1")
	baseCommit := gitHeadCommit(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "pub fn v2() {}\n")
	mustWriteTestFile(t, filepath.Join(sourceRoot, "Cargo.lock"), "version = 4\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs", "Cargo.lock")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "t2")
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: main
base_commit: "`+baseCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Diff escapes scope"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
base_commit: "`+baseCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/report-snippet.md"
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "report-snippet.md"), "# Summary\n")

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected diff outside write_scope to fail validation")
	}
	var found bool
	for _, issue := range validation.Issues {
		if issue.Code == "task_head_diff_outside_write_scope" && strings.Contains(issue.Message, "Cargo.lock") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected diff outside write_scope issue, got %+v", validation.Issues)
	}
}

func TestValidateRepository_RejectsReviewPendingTaskWithLingeringLease(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir source root failed: %v", err)
	}
	initGitRepo(t, sourceRoot)
	runGitOrFail(t, sourceRoot, "checkout", "-b", "codearmy/t001")
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "pub fn v1() {}\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "t1")
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
source_repos: [repo-a]
plan_round: 1
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: main
base_commit: "`+headCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Lease not cleared"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
owner_agent: executor
lease_until: "2026-03-28T17:18:35+08:00"
base_commit: "`+headCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/report-snippet.md"
---

# Task

## Goal
- complete the work

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the code
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "report-snippet.md"), "# Summary\n")

	_, validation, err := Validate(root)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected lingering lease to fail validation")
	}
	var found bool
	for _, issue := range validation.Issues {
		if issue.Code == "task_review_pending_lease_not_cleared" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected lingering lease issue, got %+v", validation.Issues)
	}
}

func TestResumeWakeTask_RestoresExecutingState(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship the workflow"
current_phase: P01
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Wake me"
phase: P01
status: waiting_external
execution_round: 1
target_repos: [repo-a]
write_scope: [src/core]
wake_at: "2026-03-24T10:00:00+08:00"
wake_prompt: "resume the cluster job"
---
`)

	task, err := ResumeWakeTask(root, "T001", time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600)), time.Hour)
	if err != nil {
		t.Fatalf("resume wake task failed: %v", err)
	}
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected executing after wake resume, got %s", got)
	}
	if task.Frontmatter.OwnerAgent == "" || task.LeaseUntil.IsZero() {
		t.Fatalf("expected owner and lease to be restored, got %+v", task)
	}
	if task.Frontmatter.WakePrompt != "" || !task.WakeAt.IsZero() {
		t.Fatalf("expected wake fields cleared, got %+v", task)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	mustWriteTestFile(t, filepath.Join(root, "README.md"), "seed\n")
	runGitOrFail(t, root, "init")
	runGitOrFail(t, root, "add", "README.md")
	runGitOrFail(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	runGitOrFail(t, root, "branch", "-M", "main")
	runGitOrFail(t, root, "checkout", "-b", "dev")
}

func gitHeadCommit(t *testing.T, root string) string {
	t.Helper()
	output, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	return strings.TrimSpace(output)
}

func runGitOrFail(t *testing.T, root string, args ...string) {
	t.Helper()
	if _, err := runGit(root, args...); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}
