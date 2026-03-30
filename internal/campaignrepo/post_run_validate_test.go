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
