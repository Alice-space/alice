package campaignrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanFromPath(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() {
		time.Local = oldLocal
	})

	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
---

# Campaign
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Baseline"
phase: P01
status: done
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T002", `---
task_id: T002
title: "Ready task"
phase: P01
status: ready
depends_on: [T001]
target_repos: [repo-a]
write_scope: [src/core]
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T003", `---
task_id: T003
title: "Conflicting task"
phase: P01
status: ready
depends_on: [T001]
target_repos: [repo-a]
write_scope: [src/core/api]
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T004", `---
task_id: T004
title: "Running task"
phase: P01
status: in_progress
owner_agent: executor
lease_until: "2026-03-24T12:00:00+08:00"
target_repos: [repo-b]
write_scope: [src/train]
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T005", `---
task_id: T005
title: "Wake me"
phase: P01
status: waiting_external
wake_at: "2026-03-24T10:00:00+08:00"
wake_prompt: "resume the cluster job"
---
`)

	_, summary, err := ScanFromPath(root, time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600)), 2)
	if err != nil {
		t.Fatalf("scan campaign repo failed: %v", err)
	}
	if summary.TaskCount != 5 {
		t.Fatalf("unexpected task count: %d", summary.TaskCount)
	}
	if summary.ActiveCount != 1 {
		t.Fatalf("unexpected active count: %d", summary.ActiveCount)
	}
	if summary.ReadyCount != 1 {
		t.Fatalf("unexpected ready count: %d", summary.ReadyCount)
	}
	if len(summary.SelectedReady) != 1 || summary.SelectedReady[0].TaskID != "T002" {
		t.Fatalf("unexpected selected ready tasks: %+v", summary.SelectedReady)
	}
	if len(summary.BlockedTasks) != 1 || summary.BlockedTasks[0].TaskID != "T003" {
		t.Fatalf("unexpected blocked tasks: %+v", summary.BlockedTasks)
	}
	if len(summary.WakeDue) != 1 || summary.WakeDue[0].TaskID != "T005" {
		t.Fatalf("unexpected wake due tasks: %+v", summary.WakeDue)
	}
	if len(summary.WakeTasks) != 1 || !strings.Contains(summary.WakeTasks[0].Prompt, "resume the cluster job") {
		t.Fatalf("unexpected wake task specs: %+v", summary.WakeTasks)
	}
	if !strings.Contains(summary.WakeTasks[0].Prompt, "Scheduled wake_at: 2026-03-24T10:00:00+08:00") {
		t.Fatalf("expected wake prompt to come from template, got %+v", summary.WakeTasks[0])
	}

	reportPath, err := WriteLiveReport(root, summary)
	if err != nil {
		t.Fatalf("write live report failed: %v", err)
	}
	reportContent, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read live report failed: %v", err)
	}
	if !strings.Contains(string(reportContent), "dispatch executor for `T002`") {
		t.Fatalf("expected live report to mention selected task, got %s", string(reportContent))
	}
}

func TestNormalizeTaskDocumentPreservesExplicitTimeOffset(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() {
		time.Local = oldLocal
	})

	raw := "2026-03-24T10:00:00+08:00"
	parsed, err := parseFlexibleTime(raw)
	if err != nil {
		t.Fatalf("parse flexible time failed: %v", err)
	}

	task := normalizeTaskDocument(TaskDocument{
		Path:       "phases/P01/tasks/T001/task.md",
		Dir:        "phases/P01/tasks/T001",
		LeaseUntil: parsed,
		WakeAt:     parsed,
		Frontmatter: TaskFrontmatter{
			TaskID: "T001",
			Phase:  "P01",
		},
	})

	if task.Frontmatter.LeaseUntilRaw != raw {
		t.Fatalf("expected lease_until to preserve offset, got %q", task.Frontmatter.LeaseUntilRaw)
	}
	if task.Frontmatter.WakeAtRaw != raw {
		t.Fatalf("expected wake_at to preserve offset, got %q", task.Frontmatter.WakeAtRaw)
	}
}

func TestReconcileFromPathClaimsExecutorAndReviewer(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600))

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
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
title: "Ready task"
phase: P01
status: ready
target_repos: [repo-a]
write_scope: [src/core]
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T002", `---
task_id: T002
title: "Needs review"
phase: P01
status: review_pending
head_commit: "abc123"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T002", "results", "summary.md"), "# Summary\n")

	repo, summary, err := ReconcileFromPath(root, now, 2)
	if err != nil {
		t.Fatalf("reconcile campaign repo failed: %v", err)
	}
	if summary.ActiveCount != 2 {
		t.Fatalf("unexpected active count after reconcile: %d", summary.ActiveCount)
	}
	if len(summary.SelectedReady) != 0 {
		t.Fatalf("expected ready queue to be claimed, got %+v", summary.SelectedReady)
	}
	if len(summary.SelectedReview) != 0 {
		t.Fatalf("expected review queue to be claimed, got %+v", summary.SelectedReview)
	}

	byID := map[string]TaskDocument{}
	for _, task := range repo.Tasks {
		byID[task.Frontmatter.TaskID] = task
	}
	if got := normalizeTaskStatus(byID["T001"].Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected T001 executing, got %s", got)
	}
	if byID["T001"].Frontmatter.ExecutionRound != 1 {
		t.Fatalf("expected T001 execution round 1, got %d", byID["T001"].Frontmatter.ExecutionRound)
	}
	if got := normalizeTaskStatus(byID["T002"].Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected T002 reviewing, got %s", got)
	}
	if byID["T002"].Frontmatter.ReviewRound != 1 {
		t.Fatalf("expected T002 review round 1, got %d", byID["T002"].Frontmatter.ReviewRound)
	}
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 dispatch specs")
	}
}

func TestReconcileFromPathAppliesReviewVerdict(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600))

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
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
title: "Needs review verdict"
phase: P01
status: reviewing
review_round: 1
owner_agent: reviewer.claude
lease_until: "2026-03-24T12:00:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: concern
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	repo, summary, err := ReconcileFromPath(root, now, 2)
	if err != nil {
		t.Fatalf("reconcile campaign repo failed: %v", err)
	}
	if summary.ActiveCount != 1 {
		t.Fatalf("expected rework task to be re-claimed immediately, got active=%d", summary.ActiveCount)
	}
	if summary.ReworkCount != 0 || summary.ReadyCount != 0 {
		t.Fatalf("expected rework queue to be empty after claim, got ready=%d rework=%d", summary.ReadyCount, summary.ReworkCount)
	}
	task := repo.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected executing status after re-claim, got %s", got)
	}
	if task.Frontmatter.ReviewStatus != "changes_requested" {
		t.Fatalf("expected changes_requested review status, got %s", task.Frontmatter.ReviewStatus)
	}
	if task.Frontmatter.LastReviewPath == "" {
		t.Fatalf("expected last review path to be recorded")
	}
	if task.Frontmatter.OwnerAgent == "" || task.LeaseUntil.IsZero() {
		t.Fatalf("expected executor lease to be claimed: %+v", task)
	}
}

func mustWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s failed: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s failed: %v", path, err)
	}
}

func mustWriteTestTaskPackage(t *testing.T, root, phase, taskID, taskMarkdown string) string {
	t.Helper()
	taskDir := filepath.Join(root, "phases", phase, "tasks", taskID)
	mustWriteTestFile(t, filepath.Join(taskDir, "task.md"), taskMarkdown)
	mustWriteTestFile(t, filepath.Join(taskDir, "context.md"), `# Context

## Context
- concrete task context

## Relevant Repos
- repo-a

## Relevant Files
- src/example.go

## Dependencies
- none
`)
	mustWriteTestFile(t, filepath.Join(taskDir, "plan.md"), `# Execution Plan

## Execution Steps
1. do the work

## Validation
- run focused checks

## Handoff
- hand off to reviewer
`)
	mustWriteTestFile(t, filepath.Join(taskDir, "progress.md"), `# Progress

## Timeline
- initialized

## Updates
- none

## Blockers
- none
`)
	if err := os.MkdirAll(filepath.Join(taskDir, "results"), 0o755); err != nil {
		t.Fatalf("mkdir results dir failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(taskDir, "reviews"), 0o755); err != nil {
		t.Fatalf("mkdir reviews dir failed: %v", err)
	}
	return taskDir
}
