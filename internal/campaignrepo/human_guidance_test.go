package campaignrepo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyTaskHumanGuidanceAcceptCompletesCampaignOnlyTask(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 2, 15, 30, 0, 0, time.FixedZone("CST", 8*3600))

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
goal: "Close the campaign-only task"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T306", `---
task_id: T306
title: "Mode B result handoff"
phase: P01
status: blocked
dispatch_state: needs_human
review_status: blocked
execution_round: 19
last_blocked_reason: "review rounds 18 and 17 both returned non-approve verdicts on the same target"
last_run_path: "results/report-snippet.md"
write_scope: [campaign:phases/P01/tasks/T306/**]
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T306", "results", "report-snippet.md"), "# Report\n")

	task, err := ApplyTaskHumanGuidance(root, "T306", "accept", "Accept current Mode B handoff without compare-metrics rework.", now)
	if err != nil {
		t.Fatalf("apply task guidance failed: %v", err)
	}
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusAccepted {
		t.Fatalf("expected accepted status before reconcile, got %s", got)
	}
	if task.Frontmatter.DispatchState != dispatchStateHumanGuidanceApplied {
		t.Fatalf("expected human_guidance_applied dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.ReviewStatus != "approved" {
		t.Fatalf("expected review_status=approved, got %q", task.Frontmatter.ReviewStatus)
	}
	if task.Frontmatter.LastBlockedReason != "" {
		t.Fatalf("expected blocked reason to be cleared, got %q", task.Frontmatter.LastBlockedReason)
	}
	if task.Frontmatter.LastHumanGuidancePath == "" {
		t.Fatal("expected last_human_guidance_path to be populated")
	}
	guidanceRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(task.Frontmatter.LastHumanGuidancePath)))
	if err != nil {
		t.Fatalf("read guidance file failed: %v", err)
	}
	if !containsAll(string(guidanceRaw), "guidance_id: G001", "action: accept", "Accept current Mode B handoff without compare-metrics rework.", "Previous Blocker", "review rounds 18 and 17") {
		t.Fatalf("unexpected guidance file: %s", string(guidanceRaw))
	}

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	finalTask := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(finalTask.Frontmatter.Status); got != TaskStatusDone {
		t.Fatalf("expected campaign-only accepted task to integrate to done, got %s", got)
	}
	if finalTask.Frontmatter.DispatchState != "integration_not_required" {
		t.Fatalf("expected integration_not_required dispatch state, got %q", finalTask.Frontmatter.DispatchState)
	}
	if result.Summary.DoneCount != 1 {
		t.Fatalf("expected done_count=1, got %d", result.Summary.DoneCount)
	}
}

func TestApplyTaskHumanGuidanceResumeRedispatchesExecutorWithGuidancePrompt(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 2, 15, 35, 0, 0, time.FixedZone("CST", 8*3600))

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
goal: "Resume the blocked task"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T306", `---
task_id: T306
title: "Mode B result handoff"
phase: P01
status: blocked
dispatch_state: needs_human
review_status: blocked
execution_round: 19
last_blocked_reason: "review rounds 18 and 17 both returned non-approve verdicts on the same target"
last_run_path: "results/report-snippet.md"
write_scope: [campaign:phases/P01/tasks/T306/**]
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T306", "results", "report-snippet.md"), "# Report\n")

	task, err := ApplyTaskHumanGuidance(root, "T306", "resume", "Accept current Mode B result as-is; do not rerun remote jobs; only normalize task-local evidence.", now)
	if err != nil {
		t.Fatalf("apply task guidance failed: %v", err)
	}
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusRework {
		t.Fatalf("expected rework status before reconcile, got %s", got)
	}
	if task.Frontmatter.DispatchState != dispatchStateHumanGuidanceRequested {
		t.Fatalf("expected human_guidance_requested dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.ReviewStatus != "changes_requested" {
		t.Fatalf("expected review_status=changes_requested, got %q", task.Frontmatter.ReviewStatus)
	}

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.ClaimedExecutors != 1 {
		t.Fatalf("expected one executor claim after resume, got %d", result.ClaimedExecutors)
	}
	finalTask := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(finalTask.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected resumed task to be executing, got %s", got)
	}
	if finalTask.Frontmatter.DispatchState != "executor_dispatched" {
		t.Fatalf("expected executor_dispatched after claim, got %q", finalTask.Frontmatter.DispatchState)
	}
	if finalTask.Frontmatter.ExecutionRound != 20 {
		t.Fatalf("expected execution round to advance to 20, got %d", finalTask.Frontmatter.ExecutionRound)
	}
	if finalTask.Frontmatter.LastBlockedReason != "" {
		t.Fatalf("expected blocked reason to be cleared on redispatch, got %q", finalTask.Frontmatter.LastBlockedReason)
	}
	if len(result.DispatchTasks) != 1 {
		t.Fatalf("expected one dispatch task, got %d", len(result.DispatchTasks))
	}
	prompt := result.DispatchTasks[0].Prompt
	if !containsAll(prompt,
		"最近一次人工指导动作: resume",
		"最近一次人工指导摘要: Accept current Mode B result as-is; do not rerun remote jobs; only normalize task-local evidence.",
		"最近一次人工指导文件: phases/P01/tasks/T306/guidance/G001.md",
		"这是一次人工脱困后的恢复轮。开始执行前先读 guidance 文件；它记录了上一次卡住的原因和人类裁决。",
	) {
		t.Fatalf("unexpected executor prompt after human guidance resume: %q", prompt)
	}
}
