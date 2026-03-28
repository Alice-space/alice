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
