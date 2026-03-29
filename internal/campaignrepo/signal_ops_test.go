package campaignrepo

import (
	"path/filepath"
	"testing"
)

func TestHandleTaskBlocked_RoutesToReviewerGuidanceFirst(t *testing.T) {
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
title: "Retry blocked executor"
phase: P01
status: executing
dispatch_state: executor_dispatched
review_status: pending
execution_round: 1
owner_agent: executor.codex
lease_until: "2026-03-30T10:00:00+08:00"
---
`)

	outcome, err := HandleTaskBlocked(root, "T001", "missing remote environment")
	if err != nil {
		t.Fatalf("handle task blocked failed: %v", err)
	}
	if !outcome.GuidanceRequested || outcome.GuidanceAttempt != 1 {
		t.Fatalf("expected first blocked attempt to request guidance, got %+v", outcome)
	}

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	task := repo.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewPending {
		t.Fatalf("expected review_pending after blocked guidance handoff, got %s", got)
	}
	if task.Frontmatter.DispatchState != "blocked_guidance_requested" {
		t.Fatalf("expected blocked_guidance_requested, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.BlockGuidanceCount != 1 {
		t.Fatalf("expected block guidance count 1, got %d", task.Frontmatter.BlockGuidanceCount)
	}
	if task.Frontmatter.OwnerAgent != "" || !task.LeaseUntil.IsZero() {
		t.Fatalf("expected active lease to be cleared, got owner=%q lease=%v", task.Frontmatter.OwnerAgent, task.LeaseUntil)
	}
}
