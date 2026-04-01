package campaignrepo

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommitReconcileSnapshot_SplitsTaskAndLiveReportCommits(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
---
`)
	mustWriteTestFile(t, filepath.Join(root, "reports", "live-report.md"), "# old report\n")
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Commit me"
phase: P01
status: review_pending
write_scope: [campaign:phases/P01/tasks/T001/**]
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# summary\n")
	runGitOrFail(t, root, "add", ".")
	runGitOrFail(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")

	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "task.md"), `---
task_id: T001
title: "Commit me"
phase: P01
status: review_pending
write_scope: [campaign:phases/P01/tasks/T001/**]
last_run_path: "results/summary.md"
review_status: pending
---

# Task
`)

	summary := Summary{
		GeneratedAt:   time.Date(2026, 4, 1, 18, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		CampaignID:    "camp_demo",
		CampaignTitle: "Demo Campaign",
		CurrentPhase:  "P01",
		MaxParallel:   1,
	}
	result, err := CommitReconcileSnapshot(root, &summary)
	if err != nil {
		t.Fatalf("commit reconcile snapshot failed: %v", err)
	}
	if !result.RepoCommitted {
		t.Fatal("expected reconcile state commit")
	}
	if !result.LiveReportCommitted {
		t.Fatal("expected live report commit")
	}

	headMessageOutput, err := runGit(root, "show", "-s", "--format=%s", "HEAD")
	if err != nil {
		t.Fatalf("git show HEAD message failed: %v", err)
	}
	headMessage := strings.TrimSpace(headMessageOutput)
	if headMessage != "chore(campaign): refresh live report" {
		t.Fatalf("unexpected HEAD message: %q", headMessage)
	}
	headFilesOutput, err := runGit(root, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatalf("git show HEAD files failed: %v", err)
	}
	headFiles := strings.TrimSpace(headFilesOutput)
	if headFiles != "reports/live-report.md" {
		t.Fatalf("expected live report only in HEAD, got %q", headFiles)
	}

	prevMessageOutput, err := runGit(root, "show", "-s", "--format=%s", "HEAD^")
	if err != nil {
		t.Fatalf("git show HEAD^ message failed: %v", err)
	}
	prevMessage := strings.TrimSpace(prevMessageOutput)
	if prevMessage != "chore(campaign): reconcile repo state" {
		t.Fatalf("unexpected previous message: %q", prevMessage)
	}
	prevFilesOutput, err := runGit(root, "show", "--name-only", "--format=", "HEAD^")
	if err != nil {
		t.Fatalf("git show HEAD^ files failed: %v", err)
	}
	prevFiles := strings.TrimSpace(prevFilesOutput)
	if prevFiles != "phases/P01/tasks/T001/task.md" {
		t.Fatalf("expected task-only reconcile commit, got %q", prevFiles)
	}
}
