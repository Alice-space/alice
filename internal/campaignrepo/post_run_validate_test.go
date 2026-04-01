package campaignrepo

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidateTaskPostRun_ExecutorRejectsDanglingExecutingState(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Dangling executor"
phase: P01
status: executing
dispatch_state: executor_dispatched
review_status: pending
execution_round: 1
review_round: 1
owner_agent: ""
lease_until: ""
last_run_path: "results/summary.md"
write_scope:
  - campaign:phases/P01/tasks/T001/**
target_repos: []
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	validation, err := ValidateTaskPostRun(root, "T001", DispatchKindExecutor)
	if err != nil {
		t.Fatalf("validate task post run failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected dangling executing state to fail post-run validation")
	}
	found := false
	for _, issue := range validation.Issues {
		if issue.Code == "task_executor_post_run_active_state" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected active-state issue, got %+v", validation.Issues)
	}
}

func TestValidateTaskPostRun_ReviewerRejectsExecutorAuthoredReview(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Reviewer contract"
phase: P01
status: reviewing
dispatch_state: reviewer_dispatched
review_status: reviewing
execution_round: 1
review_round: 1
owner_agent: reviewer.codex
lease_until: "2026-03-30T10:00:00+08:00"
write_scope:
  - campaign:phases/P01/tasks/T001/**
reviewer:
  role: reviewer
  workflow: code_army
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
reviewer:
  role: executor
  workflow: code_army
verdict: concern
blocking: false
created_at: "2026-03-30T09:00:00+08:00"
---
`)

	validation, err := ValidateTaskPostRun(root, "T001", DispatchKindReviewer)
	if err != nil {
		t.Fatalf("validate task post run failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected executor-authored review to fail reviewer post-run validation")
	}
	found := false
	for _, issue := range validation.Issues {
		if issue.Code == "review_role_mismatch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected review_role_mismatch, got %+v", validation.Issues)
	}
}

func TestValidateTaskPostRun_ExecutorRequiresRecordedPassedSelfCheck(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Needs self-check proof"
phase: P01
status: review_pending
dispatch_state: executor_completed
review_status: pending
execution_round: 1
write_scope:
  - campaign:phases/P01/tasks/T001/**
target_repos: []
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	validation, err := ValidateTaskPostRun(root, "T001", DispatchKindExecutor)
	if err != nil {
		t.Fatalf("validate task post run failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected missing self-check proof to fail post-run validation")
	}
	found := false
	for _, issue := range validation.Issues {
		if issue.Code == "task_post_run_self_check_missing" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected self-check proof issue, got %+v", validation.Issues)
	}
}

func TestRunTaskSelfCheck_PersistsExecutorProofAndSatisfiesRuntimeGate(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Executor self-check"
phase: P01
status: review_pending
dispatch_state: executor_completed
review_status: pending
execution_round: 2
write_scope:
  - campaign:phases/P01/tasks/T001/**
target_repos: []
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	checkedAt := time.Date(2026, 3, 31, 18, 0, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunTaskSelfCheck(root, "T001", DispatchKindExecutor, checkedAt)
	if err != nil {
		t.Fatalf("run task self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected executor self-check to pass, got %+v", validation.Issues)
	}

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("reload repo failed: %v", err)
	}
	task := repo.Tasks[0]
	if task.Frontmatter.SelfCheckKind != string(DispatchKindExecutor) {
		t.Fatalf("unexpected self_check_kind: %q", task.Frontmatter.SelfCheckKind)
	}
	if task.Frontmatter.SelfCheckRound != 2 {
		t.Fatalf("unexpected self_check_round: %d", task.Frontmatter.SelfCheckRound)
	}
	if task.Frontmatter.SelfCheckStatus != taskSelfCheckStatusPassed {
		t.Fatalf("unexpected self_check_status: %q", task.Frontmatter.SelfCheckStatus)
	}
	if task.Frontmatter.SelfCheckAtRaw != checkedAt.Format(time.RFC3339) {
		t.Fatalf("unexpected self_check_at: %q", task.Frontmatter.SelfCheckAtRaw)
	}

	runtimeValidation, err := ValidateTaskPostRun(root, "T001", DispatchKindExecutor)
	if err != nil {
		t.Fatalf("validate task post run failed: %v", err)
	}
	if !runtimeValidation.Valid {
		t.Fatalf("expected runtime gate to accept executor self-check proof, got %+v", runtimeValidation.Issues)
	}
}

func TestRunTaskSelfCheck_PersistsReviewerProofAndSatisfiesRuntimeGate(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Reviewer self-check"
phase: P01
status: reviewing
dispatch_state: reviewer_dispatched
review_status: reviewing
execution_round: 1
review_round: 2
owner_agent: reviewer.codex
lease_until: "2026-03-31T18:30:00+08:00"
write_scope:
  - campaign:phases/P01/tasks/T001/**
reviewer:
  role: reviewer
  workflow: code_army
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R002.md"), `---
review_id: R002
target_task: T001
review_round: 2
reviewer:
  role: reviewer
  workflow: code_army
verdict: approve
blocking: false
created_at: "2026-03-31T18:05:00+08:00"
---
`)

	checkedAt := time.Date(2026, 3, 31, 18, 6, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunTaskSelfCheck(root, "T001", DispatchKindReviewer, checkedAt)
	if err != nil {
		t.Fatalf("run task self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected reviewer self-check to pass, got %+v", validation.Issues)
	}

	runtimeValidation, err := ValidateTaskPostRun(root, "T001", DispatchKindReviewer)
	if err != nil {
		t.Fatalf("validate task post run failed: %v", err)
	}
	if !runtimeValidation.Valid {
		t.Fatalf("expected runtime gate to accept reviewer self-check proof, got %+v", runtimeValidation.Issues)
	}
}

func TestValidateTaskPostRun_ReviewerAcceptsJudgeAppliedReworkAfterConcern(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Reviewer concern becomes rework"
phase: P01
status: reviewing
dispatch_state: reviewer_dispatched
review_status: reviewing
execution_round: 1
review_round: 1
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T18:06:00+08:00"
owner_agent: reviewer.codex
lease_until: "2026-03-31T18:30:00+08:00"
write_scope:
  - campaign:phases/P01/tasks/T001/**
reviewer:
  role: reviewer
  workflow: code_army
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
reviewer:
  role: reviewer
  workflow: code_army
verdict: concern
blocking: false
created_at: "2026-03-31T18:05:00+08:00"
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	applied, _, err := applyReviewVerdicts(&repo, "camp_demo")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected one applied review, got %d", applied)
	}

	runtimeValidation, err := ValidateTaskPostRun(root, "T001", DispatchKindReviewer)
	if err != nil {
		t.Fatalf("validate task post run failed: %v", err)
	}
	if !runtimeValidation.Valid {
		t.Fatalf("expected runtime gate to accept judge-applied rework after concern, got %+v", runtimeValidation.Issues)
	}
}

func TestLatestRelevantReview_IgnoresNonReviewerArtifacts(t *testing.T) {
	task := TaskDocument{
		Frontmatter: TaskFrontmatter{
			TaskID:      "T001",
			ReviewRound: 1,
			Reviewer: RoleConfig{
				Role: "reviewer",
			},
		},
	}
	valid := ReviewDocument{
		Path:      "phases/P01/tasks/T001/reviews/R001.md",
		CreatedAt: time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC),
		Frontmatter: ReviewFrontmatter{
			TargetTask:  "T001",
			ReviewRound: 1,
			Verdict:     "concern",
			Reviewer: RoleConfig{
				Role: "reviewer",
			},
		},
	}
	invalid := ReviewDocument{
		Path:      "phases/P01/tasks/T001/reviews/R002.md",
		CreatedAt: time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC),
		Frontmatter: ReviewFrontmatter{
			TargetTask:  "T001",
			ReviewRound: 1,
			Verdict:     "approve",
			Reviewer: RoleConfig{
				Role: "executor",
			},
		},
	}

	chosen, ok := latestRelevantReview(task, []ReviewDocument{valid, invalid})
	if !ok {
		t.Fatal("expected a valid reviewer-authored review to be selected")
	}
	if chosen.Path != valid.Path {
		t.Fatalf("expected valid reviewer review to win, got %s", chosen.Path)
	}
}

func TestRunPlanSelfCheck_PersistsPlannerReviewerFailureProofForApproveWithConcerns(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_round: 2
plan_status: plan_review_pending
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship planning"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Planner task"
phase: P01
status: draft
target_repos: []
write_scope:
  - campaign:phases/P01/tasks/T001/**
executor:
  role: executor
  workflow: code_army
reviewer:
  role: reviewer
  workflow: code_army
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-002-plan.md"), `---
proposal_id: "plan-r2"
plan_round: 2
status: submitted
---

# Plan Proposal

## Analysis
- concrete

## Phases
- P01

## Task Breakdown
- T001

## Risks
- tracked
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: draft
human_approved: false
---

# Master Plan

## Analysis
- concrete

## Phases
- P01

## Task Breakdown
- T001

## Risks
- tracked
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "reviews", "round-002-review.md"), `---
review_id: "plan-review-r2"
plan_round: 2
reviewer:
  role: planner_reviewer
  workflow: code_army
verdict: approve
blocking: false
created_at: "2026-04-01T10:30:00+08:00"
---

# Plan Review

## Summary
- mostly ready

## Findings
- one remaining issue

## Concerns
- still needs a human boundary check

## Conclusion
- approve
`)

	checkedAt := time.Date(2026, 4, 1, 10, 35, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunPlanSelfCheck(root, DispatchKindPlannerReviewer, 2, checkedAt)
	if err != nil {
		t.Fatalf("run plan self-check failed: %v", err)
	}
	if validation.Valid {
		t.Fatal("expected approve-with-concerns review to fail planner reviewer self-check")
	}
	found := false
	for _, issue := range validation.Issues {
		if issue.Code == "plan_review_approve_has_concerns" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected strict approve issue, got %+v", validation.Issues)
	}

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("reload repo failed: %v", err)
	}
	if repo.Campaign.Frontmatter.PlannerReviewerSelfCheckRound != 2 {
		t.Fatalf("unexpected planner reviewer self-check round: %d", repo.Campaign.Frontmatter.PlannerReviewerSelfCheckRound)
	}
	if repo.Campaign.Frontmatter.PlannerReviewerSelfCheckStatus != taskSelfCheckStatusFailed {
		t.Fatalf("unexpected planner reviewer self-check status: %q", repo.Campaign.Frontmatter.PlannerReviewerSelfCheckStatus)
	}
	if repo.Campaign.Frontmatter.PlannerReviewerSelfCheckAtRaw != checkedAt.Format(time.RFC3339) {
		t.Fatalf("unexpected planner reviewer self-check timestamp: %q", repo.Campaign.Frontmatter.PlannerReviewerSelfCheckAtRaw)
	}
}
