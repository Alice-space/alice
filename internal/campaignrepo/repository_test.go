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
	if summary.WakeTasks[0].Title != "Demo Campaign · T005 · 唤醒" {
		t.Fatalf("unexpected wake task title: %q", summary.WakeTasks[0].Title)
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

func TestSummarize_AcceptedDependencyUnblocksReadyTask(t *testing.T) {
	root := t.TempDir()
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
title: "Accepted dependency"
phase: P01
status: accepted
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T002", `---
task_id: T002
title: "Follow-up task"
phase: P01
status: ready
depends_on: [T001]
target_repos: [repo-a]
write_scope: [src/main.rs]
---
`)

	_, summary, err := ScanFromPath(root, time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC), 2)
	if err != nil {
		t.Fatalf("scan campaign repo failed: %v", err)
	}
	if summary.ReadyCount != 1 {
		t.Fatalf("expected ready_count=1, got %d", summary.ReadyCount)
	}
	if len(summary.BlockedTasks) != 0 {
		t.Fatalf("expected no blocked tasks, got %+v", summary.BlockedTasks)
	}
	if len(summary.SelectedReady) != 1 || summary.SelectedReady[0].TaskID != "T002" {
		t.Fatalf("unexpected selected ready tasks: %+v", summary.SelectedReady)
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

func TestParseFlexibleTimeAcceptsCompactTimezoneOffset(t *testing.T) {
	parsed, err := parseFlexibleTime("2026-03-28T14:57:39+0800")
	if err != nil {
		t.Fatalf("parse flexible time failed: %v", err)
	}
	if got := parsed.Format(time.RFC3339); got != "2026-03-28T14:57:39+08:00" {
		t.Fatalf("unexpected parsed timestamp: %s", got)
	}
}

func TestLoadAcceptsReviewCreatedAtWithCompactTimezoneOffset(t *testing.T) {
	root := t.TempDir()
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
review_round: 5
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R005.md"), `---
review_id: R005
target_task: T001
review_round: 5
verdict: approve
blocking: false
created_at: "2026-03-28T14:57:39+0800"
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	if len(repo.Reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(repo.Reviews))
	}
	if got := repo.Reviews[0].CreatedAt.Format(time.RFC3339); got != "2026-03-28T14:57:39+08:00" {
		t.Fatalf("unexpected review created_at: %s", got)
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

func TestSummarize_SkipsReviewDispatchWhenExecutionArtifactsDoNotResolve(t *testing.T) {
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
current_phase: P01
source_repos: [repo-a]
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
title: "Needs review"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
head_commit: "deadbeef"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	summary := Summarize(repo, time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC), 2)
	if len(summary.SelectedReview) != 0 {
		t.Fatalf("expected no selected review tasks, got %+v", summary.SelectedReview)
	}
	var found bool
	for _, task := range summary.BlockedTasks {
		if task.TaskID == "T001" && strings.Contains(task.BlockedReason, "head_commit") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected blocked reason about execution artifacts, got %+v", summary.BlockedTasks)
	}
}

func TestSummarize_SelectsCampaignOnlyReviewPendingTaskWithoutSourceCommit(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	summary := Summarize(repo, time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC), 2)
	if len(summary.SelectedReview) != 1 || summary.SelectedReview[0].TaskID != "T001" {
		t.Fatalf("expected campaign-only review task to be selected, got %+v", summary.SelectedReview)
	}
}

func TestSummarize_BlocksDanglingExecutingTaskWithoutOwnerLease(t *testing.T) {
	root := t.TempDir()
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
title: "Dangling execute"
phase: P01
status: executing
execution_round: 2
write_scope: [campaign:phases/P01/tasks/T001/**]
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	summary := Summarize(repo, time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC), 2)
	if summary.ActiveCount != 0 {
		t.Fatalf("expected dangling task not to count as active, got %d", summary.ActiveCount)
	}
	var found bool
	for _, task := range summary.BlockedTasks {
		if task.TaskID == "T001" && strings.Contains(task.BlockedReason, "owner_agent/lease_until are empty") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dangling executing task to be blocked, got %+v", summary.BlockedTasks)
	}
}

func TestSummarize_SkipsReviewDispatchWhenDiffEscapesWriteScope(t *testing.T) {
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
current_phase: P01
source_repos: [repo-a]
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
title: "Needs review"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
base_commit: "`+baseCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	summary := Summarize(repo, time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC), 2)
	if len(summary.SelectedReview) != 0 {
		t.Fatalf("expected no selected review tasks, got %+v", summary.SelectedReview)
	}
	var found bool
	for _, task := range summary.BlockedTasks {
		if task.TaskID == "T001" && strings.Contains(task.BlockedReason, "Cargo.lock") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected blocked reason about write_scope escape, got %+v", summary.BlockedTasks)
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
