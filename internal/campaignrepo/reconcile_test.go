package campaignrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyReviewVerdicts_VerdictMatrix(t *testing.T) {
	tests := []struct {
		name             string
		verdict          string
		blocking         bool
		wantApplied      int
		wantStatus       string
		wantReviewStatus string
	}{
		{
			name:             "approve",
			verdict:          "approve",
			wantApplied:      1,
			wantStatus:       TaskStatusAccepted,
			wantReviewStatus: "approved",
		},
		{
			name:             "blocking",
			verdict:          "blocking",
			blocking:         true,
			wantApplied:      1,
			wantStatus:       TaskStatusBlocked,
			wantReviewStatus: "blocked",
		},
		{
			name:             "reject",
			verdict:          "reject",
			wantApplied:      1,
			wantStatus:       TaskStatusRejected,
			wantReviewStatus: "blocked",
		},
		{
			name:             "empty verdict ignored",
			verdict:          "",
			wantApplied:      0,
			wantStatus:       TaskStatusReviewing,
			wantReviewStatus: "pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
title: "Needs verdict"
phase: P01
status: reviewing
review_round: 1
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-24T10:31:00+08:00"
owner_agent: reviewer.claude
lease_until: "2026-03-24T12:00:00+08:00"
target_repos: [repo-a]
write_scope: [repo-a:src/lib.rs]
---
`)
			mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: "`+tt.verdict+`"
blocking: `+boolYAML(tt.blocking)+`
target_commit: "abc123"
created_at: "2026-03-24T10:30:00+08:00"
---
`)

			repo, err := Load(root)
			if err != nil {
				t.Fatalf("load repo failed: %v", err)
			}
			applied, _, err := applyReviewVerdicts(&repo, "")
			if err != nil {
				t.Fatalf("apply review verdicts failed: %v", err)
			}
			if applied != tt.wantApplied {
				t.Fatalf("unexpected applied count: got=%d want=%d", applied, tt.wantApplied)
			}

			task := repo.Tasks[0]
			if got := normalizeTaskStatus(task.Frontmatter.Status); got != tt.wantStatus {
				t.Fatalf("unexpected task status: got=%s want=%s", got, tt.wantStatus)
			}
			if got := task.Frontmatter.ReviewStatus; got != tt.wantReviewStatus {
				t.Fatalf("unexpected review status: got=%s want=%s", got, tt.wantReviewStatus)
			}
			if tt.wantApplied == 0 {
				if task.Frontmatter.LastReviewPath != "" {
					t.Fatalf("expected empty last review path, got %q", task.Frontmatter.LastReviewPath)
				}
				return
			}
			if task.Frontmatter.DispatchState != "judge_applied" {
				t.Fatalf("expected dispatch_state=judge_applied, got %q", task.Frontmatter.DispatchState)
			}
			if task.Frontmatter.LastReviewPath == "" {
				t.Fatal("expected last review path to be recorded")
			}
			if task.Frontmatter.OwnerAgent != "" {
				t.Fatalf("expected owner_agent cleared, got %q", task.Frontmatter.OwnerAgent)
			}
			if !task.LeaseUntil.IsZero() {
				t.Fatalf("expected lease_until cleared, got %s", task.LeaseUntil.Format(time.RFC3339))
			}
			if task.Frontmatter.HeadCommit != "abc123" {
				t.Fatalf("expected head_commit from review, got %q", task.Frontmatter.HeadCommit)
			}
		})
	}
}

func TestApplyReviewVerdicts_CampaignOnlyClearsHeadCommit(t *testing.T) {
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
title: "Campaign-only review"
phase: P01
status: reviewing
review_round: 1
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-24T10:31:00+08:00"
owner_agent: reviewer
lease_until: "2026-03-24T12:00:00+08:00"
target_repos: [repo-a]
write_scope: [campaign:phases/P01/tasks/T001/**]
head_commit: "stale-commit"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: "concern"
blocking: false
target_commit: "campaign-head"
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	applied, _, err := applyReviewVerdicts(&repo, "")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 1 {
		t.Fatalf("unexpected applied count: got=%d want=1", applied)
	}
	if got := repo.Tasks[0].Frontmatter.HeadCommit; got != "" {
		t.Fatalf("expected campaign-only review to clear head_commit, got %q", got)
	}
}

func TestApplyReviewVerdicts_AppliesSamePathReviewAfterReviewerSelfCheck(t *testing.T) {
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
title: "Needs same-path review verdict"
phase: P01
status: reviewing
review_status: reviewing
review_round: 1
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
write_scope: [campaign:reports/live-report.md]
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-24T10:31:00+08:00"
owner_agent: reviewer.codex
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

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	applied, _, err := applyReviewVerdicts(&repo, "")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected same-path review to be applied after reviewer self-check, got %d", applied)
	}
	task := repo.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusRework {
		t.Fatalf("expected task to move to rework, got %s", got)
	}
	if task.Frontmatter.DispatchState != "judge_applied" {
		t.Fatalf("expected judge_applied dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.OwnerAgent != "" {
		t.Fatalf("expected owner_agent cleared, got %q", task.Frontmatter.OwnerAgent)
	}
}

func TestApplyReviewVerdicts_SkipsSamePathReviewWithoutReviewerSelfCheck(t *testing.T) {
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
title: "Old same-path review should stay ignored"
phase: P01
status: reviewing
review_status: reviewing
review_round: 1
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
owner_agent: reviewer.codex
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

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	applied, _, err := applyReviewVerdicts(&repo, "")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 0 {
		t.Fatalf("expected old same-path review to stay ignored without reviewer self-check, got %d", applied)
	}
	task := repo.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected task to remain reviewing, got %s", got)
	}
}

func TestApplyReviewVerdicts_DefersFreshReviewWithoutReviewerSelfCheck(t *testing.T) {
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
title: "Fresh review waits for self-check"
phase: P01
status: reviewing
review_status: reviewing
review_round: 1
owner_agent: reviewer.codex
lease_until: "2026-03-24T12:00:00+08:00"
write_scope: [campaign:reports/live-report.md]
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

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	applied, events, err := applyReviewVerdicts(&repo, "camp_demo")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 0 {
		t.Fatalf("expected judge to defer review without reviewer self-check, got %d", applied)
	}
	task := repo.Tasks[0]
	if task.Frontmatter.DispatchState != dispatchStateJudgeWaitingReviewer {
		t.Fatalf("expected judge waiting dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "self-check proof") {
		t.Fatalf("expected blocked reason to explain missing reviewer self-check, got %q", task.Frontmatter.LastBlockedReason)
	}
	if len(events) != 1 || events[0].Kind != EventJudgeDeferred {
		t.Fatalf("expected judge deferred event, got %+v", events)
	}

	summary := Summarize(repo, time.Date(2026, 3, 24, 10, 35, 0, 0, time.FixedZone("CST", 8*3600)), 1)
	report := summary.LiveReportMarkdown()
	if !strings.Contains(report, "judge is waiting for reviewer self-check proof") {
		t.Fatalf("expected live report to include judge defer reason, got %s", report)
	}
}

func TestApplyReviewVerdicts_SkipsSamePathReviewAfterStateMutation(t *testing.T) {
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
title: "State-bound same-path review"
phase: P01
status: reviewing
review_status: reviewing
review_round: 1
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-24T10:31:00+08:00"
owner_agent: reviewer.codex
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

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	repo.Tasks[0].Frontmatter.SelfCheckDigest = taskSelfCheckSubjectDigest(repo.Tasks[0], DispatchKindReviewer)
	if err := persistTaskDocument(&repo, 0); err != nil {
		t.Fatalf("persist initial self-check digest failed: %v", err)
	}

	repo, err = Load(root)
	if err != nil {
		t.Fatalf("reload repo failed: %v", err)
	}
	repo.Tasks[0].Frontmatter.WriteScope = []string{"campaign:reports/final-report.md"}
	if err := persistTaskDocument(&repo, 0); err != nil {
		t.Fatalf("persist mutated reviewer task failed: %v", err)
	}

	repo, err = Load(root)
	if err != nil {
		t.Fatalf("reload mutated repo failed: %v", err)
	}
	applied, _, err := applyReviewVerdicts(&repo, "")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 0 {
		t.Fatalf("expected stale reviewer proof to be ignored, got %d applied verdict(s)", applied)
	}
}

func TestApplyReviewVerdicts_DowngradesBlockingDuringBlockedGuidanceLoop(t *testing.T) {
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
title: "Needs blocked guidance"
phase: P01
status: reviewing
dispatch_state: blocked_guidance_requested
review_status: reviewing
review_round: 1
block_guidance_count: 2
last_blocked_reason: "missing IHEP cluster handoff"
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-24T10:31:00+08:00"
owner_agent: reviewer.claude
lease_until: "2026-03-24T12:00:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: "blocking"
blocking: true
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	applied, events, err := applyReviewVerdicts(&repo, "camp_demo")
	if err != nil {
		t.Fatalf("apply review verdicts failed: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected one applied review, got %d", applied)
	}
	task := repo.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusRework {
		t.Fatalf("expected blocked guidance verdict to return task to rework, got %s", got)
	}
	if task.Frontmatter.ReviewStatus != "changes_requested" {
		t.Fatalf("expected changes_requested review status, got %q", task.Frontmatter.ReviewStatus)
	}
	if task.Frontmatter.DispatchState != "blocked_guidance_applied" {
		t.Fatalf("expected blocked_guidance_applied dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if len(events) != 1 || events[0].Title != "阻塞指导返回执行" {
		t.Fatalf("expected blocked guidance event, got %+v", events)
	}
}

func TestReconcileAndPrepare_RequeuesDirectBlockedTasksForReviewerGuidance(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 30, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))

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
goal: "Ship the first phase"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Replay on remote host"
phase: P01
status: blocked
dispatch_state: executor_dispatched
review_status: pending
execution_round: 1
last_blocked_reason: "missing IHEP handoff"
last_run_path: "results/README.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "README.md"), "# Result\n")

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected blocked task to be handed to reviewer, got %s", got)
	}
	if task.Frontmatter.BlockGuidanceCount != 1 {
		t.Fatalf("expected block guidance count 1, got %d", task.Frontmatter.BlockGuidanceCount)
	}
	if task.Frontmatter.ReviewRound != 1 {
		t.Fatalf("expected review round 1, got %d", task.Frontmatter.ReviewRound)
	}
	if len(result.Events) == 0 || result.Events[0].Kind != EventTaskRetrying {
		t.Fatalf("expected retrying notification event, got %+v", result.Events)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected reviewer dispatch after blocked handoff, got %+v", result.DispatchTasks)
	}
}

func TestReconcileAndPrepare_IntegratesAcceptedTaskIntoDefaultBranch(t *testing.T) {
	sourceRoot := t.TempDir()
	initGitRepo(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "base\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed source")
	baseCommit := gitHeadCommit(t, sourceRoot)

	root := t.TempDir()
	branchName := "codearmy/camp_demo/t001/repo-a"
	worktreePath := filepath.Join(root, ".worktrees", "repo-a", "t001")
	if err := ensureGitTaskWorktree(sourceRoot, worktreePath, branchName, baseCommit); err != nil {
		t.Fatalf("create task worktree failed: %v", err)
	}
	mustWriteTestFile(t, filepath.Join(worktreePath, "src", "lib.rs"), "integrated change\n")
	runGitOrFail(t, worktreePath, "add", "src/lib.rs")
	runGitOrFail(t, worktreePath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "task change")
	taskHead := gitHeadCommit(t, worktreePath)

	now := time.Date(2026, 3, 31, 17, 0, 0, 0, time.FixedZone("CST", 8*3600))
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
default_branch: dev
base_commit: "`+baseCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Ready to integrate"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [repo-a:`+branchName+`]
worktree_paths: [repo-a:`+worktreePath+`]
write_scope: [repo-a:src/lib.rs]
review_round: 1
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T16:51:00+08:00"
base_commit: "`+baseCommit+`"
head_commit: "`+taskHead+`"
last_run_path: "results/summary.md"
review_status: pending
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: approve
blocking: false
target_commit: "`+taskHead+`"
created_at: "2026-03-31T16:50:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusDone {
		t.Fatalf("expected task done after integration, got %s", got)
	}
	if task.Frontmatter.DispatchState != "integrated" {
		t.Fatalf("expected dispatch_state=integrated, got %q", task.Frontmatter.DispatchState)
	}
	mainHead := gitHeadCommit(t, sourceRoot)
	if task.Frontmatter.HeadCommit != mainHead {
		t.Fatalf("expected integrated head_commit=%s, got %s", mainHead, task.Frontmatter.HeadCommit)
	}
	if task.Frontmatter.HeadCommit == taskHead {
		t.Fatalf("expected merge commit on default branch, got task head %s", taskHead)
	}
	if !gitBranchContainsCommit(sourceRoot, "dev", taskHead) {
		t.Fatalf("expected default branch to contain task head %s after merge", taskHead)
	}
	raw, err := os.ReadFile(filepath.Join(sourceRoot, "src", "lib.rs"))
	if err != nil {
		t.Fatalf("read integrated file failed: %v", err)
	}
	if string(raw) != "integrated change\n" {
		t.Fatalf("unexpected integrated file content: %q", string(raw))
	}
	if len(result.DispatchTasks) != 0 {
		t.Fatalf("expected no dispatch tasks after integration, got %+v", result.DispatchTasks)
	}
	if len(result.Events) == 0 || result.Events[len(result.Events)-1].Kind != EventTaskIntegrated {
		t.Fatalf("expected integration event, got %+v", result.Events)
	}
}

func TestReconcileAndPrepare_BlocksAcceptedTaskWhenIntegrationWorktreeIsDirty(t *testing.T) {
	sourceRoot := t.TempDir()
	initGitRepo(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "base\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed source")
	baseCommit := gitHeadCommit(t, sourceRoot)

	root := t.TempDir()
	branchName := "codearmy/camp_demo/t001/repo-a"
	worktreePath := filepath.Join(root, ".worktrees", "repo-a", "t001")
	if err := ensureGitTaskWorktree(sourceRoot, worktreePath, branchName, baseCommit); err != nil {
		t.Fatalf("create task worktree failed: %v", err)
	}
	mustWriteTestFile(t, filepath.Join(worktreePath, "src", "lib.rs"), "task change\n")
	runGitOrFail(t, worktreePath, "add", "src/lib.rs")
	runGitOrFail(t, worktreePath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "task change")
	taskHead := gitHeadCommit(t, worktreePath)

	mustWriteTestFile(t, filepath.Join(sourceRoot, "README.md"), "dirty main worktree\n")

	now := time.Date(2026, 3, 31, 17, 10, 0, 0, time.FixedZone("CST", 8*3600))
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
default_branch: dev
base_commit: "`+baseCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Dirty integration"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [repo-a:`+branchName+`]
worktree_paths: [repo-a:`+worktreePath+`]
write_scope: [repo-a:src/lib.rs]
review_round: 1
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T17:06:00+08:00"
base_commit: "`+baseCommit+`"
head_commit: "`+taskHead+`"
last_run_path: "results/summary.md"
review_status: pending
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: approve
blocking: false
target_commit: "`+taskHead+`"
created_at: "2026-03-31T17:05:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusBlocked {
		t.Fatalf("expected task blocked after failed integration, got %s", got)
	}
	if task.Frontmatter.DispatchState != "integration_blocked" {
		t.Fatalf("expected dispatch_state=integration_blocked, got %q", task.Frontmatter.DispatchState)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "uncommitted changes") {
		t.Fatalf("expected dirty-worktree blocker, got %q", task.Frontmatter.LastBlockedReason)
	}
	if gitBranchContainsCommit(sourceRoot, "dev", taskHead) {
		t.Fatalf("expected default branch to exclude task head %s after blocked integration", taskHead)
	}
}

func TestReconcileAndPrepare_RetriesResolvedIntegrationBlockAfterRepoFactCorrection(t *testing.T) {
	sourceRoot := t.TempDir()
	initGitRepo(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "base\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed source")
	baseCommit := gitHeadCommit(t, sourceRoot)

	root := t.TempDir()
	branchName := "codearmy/camp_demo/t001/repo-a"
	worktreePath := filepath.Join(root, ".worktrees", "repo-a", "t001")
	if err := ensureGitTaskWorktree(sourceRoot, worktreePath, branchName, baseCommit); err != nil {
		t.Fatalf("create task worktree failed: %v", err)
	}
	mustWriteTestFile(t, filepath.Join(worktreePath, "src", "lib.rs"), "integrated change\n")
	runGitOrFail(t, worktreePath, "add", "src/lib.rs")
	runGitOrFail(t, worktreePath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "task change")
	taskHead := gitHeadCommit(t, worktreePath)

	now := time.Date(2026, 3, 31, 17, 20, 0, 0, time.FixedZone("CST", 8*3600))
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
title: "Retry integration after branch fact fix"
phase: P01
status: accepted
target_repos: [repo-a]
working_branches: [repo-a:`+branchName+`]
worktree_paths: [repo-a:`+worktreePath+`]
write_scope: [repo-a:src/lib.rs]
review_round: 1
base_commit: "`+baseCommit+`"
head_commit: "`+taskHead+`"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
review_status: approved
dispatch_state: judge_applied
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T17:18:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: approve
blocking: false
target_commit: "`+taskHead+`"
created_at: "2026-03-31T17:15:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusBlocked {
		t.Fatalf("expected initial integration to block, got %s", got)
	}
	if task.Frontmatter.DispatchState != "integration_blocked" {
		t.Fatalf("expected dispatch_state=integration_blocked, got %q", task.Frontmatter.DispatchState)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "default branch main") {
		t.Fatalf("expected stale main-branch blocker, got %q", task.Frontmatter.LastBlockedReason)
	}

	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+sourceRoot+`"
default_branch: dev
base_commit: "`+baseCommit+`"
role: source
---
`)

	result, err = ReconcileAndPrepare(root, now.Add(time.Minute), 1, time.Hour)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}
	task = result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusDone {
		t.Fatalf("expected corrected repo fact to unblock and integrate task, got %s", got)
	}
	if task.Frontmatter.DispatchState != "integrated" {
		t.Fatalf("expected dispatch_state=integrated after retry, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.LastBlockedReason != "" {
		t.Fatalf("expected blocker cleared after retry, got %q", task.Frontmatter.LastBlockedReason)
	}
	if !gitBranchContainsCommit(sourceRoot, "dev", taskHead) {
		t.Fatalf("expected default branch to contain task head %s after retry integration", taskHead)
	}
}

func TestReconcileAndPrepare_RetriesIntegrationBlockBySkippingReadContextRepo(t *testing.T) {
	writeRepo := t.TempDir()
	initGitRepo(t, writeRepo)
	mustWriteTestFile(t, filepath.Join(writeRepo, "README.md"), "write repo base\n")
	runGitOrFail(t, writeRepo, "add", "README.md")
	runGitOrFail(t, writeRepo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed write repo")
	writeBase := gitHeadCommit(t, writeRepo)

	readRepo := t.TempDir()
	initGitRepo(t, readRepo)
	mustWriteTestFile(t, filepath.Join(readRepo, "README.md"), "read repo base\n")
	runGitOrFail(t, readRepo, "add", "README.md")
	runGitOrFail(t, readRepo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed read repo")
	readBase := gitHeadCommit(t, readRepo)

	root := t.TempDir()
	writeBranch := "codearmy/camp_demo/t001/repo-write"
	writeWorktree := filepath.Join(root, ".worktrees", "repo-write", "t001")
	if err := ensureGitTaskWorktree(writeRepo, writeWorktree, writeBranch, writeBase); err != nil {
		t.Fatalf("create write repo worktree failed: %v", err)
	}
	readBranch := "codearmy/camp_demo/t001/repo-read"
	readWorktree := filepath.Join(root, ".worktrees", "repo-read", "t001")
	if err := ensureGitTaskWorktree(readRepo, readWorktree, readBranch, readBase); err != nil {
		t.Fatalf("create read repo worktree failed: %v", err)
	}

	mustWriteTestFile(t, filepath.Join(writeWorktree, "README.md"), "write repo task change\n")
	runGitOrFail(t, writeWorktree, "add", "README.md")
	runGitOrFail(t, writeWorktree, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "task change")
	taskHead := gitHeadCommit(t, writeWorktree)

	now := time.Date(2026, 3, 31, 17, 22, 0, 0, time.FixedZone("CST", 8*3600))
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-write, repo-read]
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-write.md"), `---
repo_id: repo-write
local_path: "`+writeRepo+`"
default_branch: dev
base_commit: "`+writeBase+`"
role: source
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-read.md"), `---
repo_id: repo-read
local_path: "`+readRepo+`"
default_branch: dev
base_commit: "`+readBase+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Skip read-context repo during integration retry"
phase: P01
status: blocked
target_repos: [repo-write, repo-read]
working_branches:
  - repo-write:`+writeBranch+`
  - repo-read:`+readBranch+`
worktree_paths:
  - repo-write:`+writeWorktree+`
  - repo-read:`+readWorktree+`
write_scope:
  - campaign:phases/P01/tasks/T001/**
  - repo-write:README.md
review_round: 1
base_commit: "`+writeBase+`"
head_commit: "`+taskHead+`"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
review_status: approved
dispatch_state: integration_blocked
last_blocked_reason: task T001 repo repo-read working_branch `+readBranch+` does not contain reviewed head_commit `+taskHead+`
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T17:21:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: approve
blocking: false
target_commit: "`+taskHead+`"
created_at: "2026-03-31T17:20:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusDone {
		t.Fatalf("expected task done after retrying with writable repo only, got %s", got)
	}
	if task.Frontmatter.DispatchState != "integrated" {
		t.Fatalf("expected dispatch_state=integrated, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.LastBlockedReason != "" {
		t.Fatalf("expected blocker cleared, got %q", task.Frontmatter.LastBlockedReason)
	}
	if !gitBranchContainsCommit(writeRepo, "dev", taskHead) {
		t.Fatalf("expected writable default branch to contain task head %s", taskHead)
	}
	if got := gitHeadCommit(t, readRepo); got != readBase {
		t.Fatalf("expected read-context repo head to stay at %s, got %s", readBase, got)
	}
}

func TestReconcileAndPrepare_RequeuesMergeConflictIntoExecutorRecovery(t *testing.T) {
	sourceRoot := t.TempDir()
	initGitRepo(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "base\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed source")
	baseCommit := gitHeadCommit(t, sourceRoot)

	root := t.TempDir()
	branchName := "codearmy/camp_demo/t001/repo-a"
	worktreePath := filepath.Join(root, ".worktrees", "repo-a", "t001")
	if err := ensureGitTaskWorktree(sourceRoot, worktreePath, branchName, baseCommit); err != nil {
		t.Fatalf("create task worktree failed: %v", err)
	}
	mustWriteTestFile(t, filepath.Join(worktreePath, "src", "lib.rs"), "task branch change\n")
	runGitOrFail(t, worktreePath, "add", "src/lib.rs")
	runGitOrFail(t, worktreePath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "task change")
	taskHead := gitHeadCommit(t, worktreePath)

	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "default branch change\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "conflicting default branch change")

	now := time.Date(2026, 3, 31, 17, 25, 0, 0, time.FixedZone("CST", 8*3600))
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
default_branch: dev
base_commit: "`+baseCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Recover merge conflict"
phase: P01
status: accepted
target_repos: [repo-a]
working_branches: [repo-a:`+branchName+`]
worktree_paths: [repo-a:`+worktreePath+`]
write_scope: [repo-a:src/lib.rs]
execution_round: 1
review_round: 1
base_commit: "`+baseCommit+`"
head_commit: "`+taskHead+`"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
review_status: approved
dispatch_state: judge_applied
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T17:24:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: approve
blocking: false
target_commit: "`+taskHead+`"
created_at: "2026-03-31T17:23:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected merge conflict to requeue executor, got %s", got)
	}
	if task.Frontmatter.DispatchState != "executor_dispatched" {
		t.Fatalf("expected executor to be redispatched, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.ReviewStatus != "changes_requested" {
		t.Fatalf("expected changes_requested after conflict recovery, got %q", task.Frontmatter.ReviewStatus)
	}
	if task.Frontmatter.ExecutionRound != 2 {
		t.Fatalf("expected execution_round=2 after redispatch, got %d", task.Frontmatter.ExecutionRound)
	}
	if task.Frontmatter.IntegrationRetryCount != 1 {
		t.Fatalf("expected integration_retry_count=1, got %d", task.Frontmatter.IntegrationRetryCount)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "CONFLICT") {
		t.Fatalf("expected conflict reason to be preserved, got %q", task.Frontmatter.LastBlockedReason)
	}
	if !containsEventKind(result.Events, EventTaskRetrying) {
		t.Fatalf("expected retrying event, got %+v", result.Events)
	}
	if !containsEventKind(result.Events, EventTaskDispatched) {
		t.Fatalf("expected dispatch event, got %+v", result.Events)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindExecutor {
		t.Fatalf("expected one executor dispatch task, got %+v", result.DispatchTasks)
	}
	if !containsAll(result.DispatchTasks[0].Prompt, "这是一次集成冲突恢复轮", "把默认分支的新变化合入或等价重放到 task branch") {
		t.Fatalf("expected integration recovery guidance in prompt, got %q", result.DispatchTasks[0].Prompt)
	}
}

func TestReconcileAndPrepare_RequeuesExistingIntegrationConflictBlock(t *testing.T) {
	sourceRoot := t.TempDir()
	initGitRepo(t, sourceRoot)
	mustWriteTestFile(t, filepath.Join(sourceRoot, "src", "lib.rs"), "base\n")
	runGitOrFail(t, sourceRoot, "add", "src/lib.rs")
	runGitOrFail(t, sourceRoot, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "seed source")
	baseCommit := gitHeadCommit(t, sourceRoot)

	root := t.TempDir()
	branchName := "codearmy/camp_demo/t001/repo-a"
	worktreePath := filepath.Join(root, ".worktrees", "repo-a", "t001")
	if err := ensureGitTaskWorktree(sourceRoot, worktreePath, branchName, baseCommit); err != nil {
		t.Fatalf("create task worktree failed: %v", err)
	}
	mustWriteTestFile(t, filepath.Join(worktreePath, "src", "lib.rs"), "task branch change\n")
	runGitOrFail(t, worktreePath, "add", "src/lib.rs")
	runGitOrFail(t, worktreePath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "task change")
	taskHead := gitHeadCommit(t, worktreePath)

	now := time.Date(2026, 3, 31, 17, 30, 0, 0, time.FixedZone("CST", 8*3600))
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
default_branch: dev
base_commit: "`+baseCommit+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Resume blocked integration conflict"
phase: P01
status: blocked
target_repos: [repo-a]
working_branches: [repo-a:`+branchName+`]
worktree_paths: [repo-a:`+worktreePath+`]
write_scope: [repo-a:src/lib.rs]
execution_round: 1
review_round: 1
base_commit: "`+baseCommit+`"
head_commit: "`+taskHead+`"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
review_status: approved
dispatch_state: integration_blocked
last_blocked_reason: |-
  task T001 repo repo-a merge `+branchName+` -> dev failed: exit status 1: Auto-merging src/lib.rs
  CONFLICT (content): Merge conflict in src/lib.rs
  Automatic merge failed; fix conflicts and then commit the result.
self_check_kind: reviewer
self_check_round: 1
self_check_status: passed
self_check_at: "2026-03-31T17:29:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: approve
blocking: false
target_commit: "`+taskHead+`"
created_at: "2026-03-31T17:28:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected blocked conflict to requeue executor, got %s", got)
	}
	if task.Frontmatter.DispatchState != "executor_dispatched" {
		t.Fatalf("expected executor dispatch after resuming blocked conflict, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.IntegrationRetryCount != 1 {
		t.Fatalf("expected integration_retry_count=1, got %d", task.Frontmatter.IntegrationRetryCount)
	}
}

func TestBuildDispatchSpecs_Content(t *testing.T) {
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
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Execute me"
phase: P01
status: executing
execution_round: 2
owner_agent: executor
lease_until: "2026-03-24T13:00:00+08:00"
target_repos: [repo-a]
working_branches: [feat/t001]
write_scope: [src/core]
review_status: changes_requested
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T002", `---
task_id: T002
title: "Review me"
phase: P01
status: reviewing
review_round: 1
owner_agent: reviewer
lease_until: "2026-03-24T13:00:00+08:00"
target_repos: [repo-a]
head_commit: "abc123"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T002", "results", "summary.md"), "# Summary\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	repo.ConfigRoleDefaults = CampaignRoleDefaults{
		Executor: RoleConfig{
			Role:            "executor.gemini",
			Provider:        "gemini",
			Model:           "gemini-2.5-pro",
			Profile:         "exec-profile",
			Workflow:        "code_army",
			ReasoningEffort: "medium",
			Personality:     "pragmatic",
		},
		Reviewer: RoleConfig{
			Role:            "reviewer.kimi",
			Provider:        "kimi",
			Model:           "kimi-k2",
			Profile:         "review-profile",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
		},
	}
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 dispatch specs, got %d", len(specs))
	}

	byKind := map[DispatchKind]DispatchTaskSpec{}
	for _, spec := range specs {
		byKind[spec.Kind] = spec
	}

	executorSpec, ok := byKind[DispatchKindExecutor]
	if !ok {
		t.Fatal("missing executor dispatch spec")
	}
	if executorSpec.StateKey != "campaign_dispatch:camp_demo:executor:T001:x2" {
		t.Fatalf("unexpected executor state key: %q", executorSpec.StateKey)
	}
	if executorSpec.Role.Provider != "gemini" || executorSpec.Role.Model != "gemini-2.5-pro" || executorSpec.Role.Profile != "exec-profile" {
		t.Fatalf("unexpected executor role: %+v", executorSpec.Role)
	}
	if executorSpec.RunAt != now {
		t.Fatalf("unexpected executor run_at: %s", executorSpec.RunAt.Format(time.RFC3339))
	}
	if executorSpec.TaskPath != "phases/P01/tasks/T001" {
		t.Fatalf("unexpected executor task path: %q", executorSpec.TaskPath)
	}
	if executorSpec.Title != "Demo Campaign · T001 · 执行 · 第 2 轮" {
		t.Fatalf("unexpected executor title: %q", executorSpec.Title)
	}
	if !containsAll(executorSpec.Prompt, "Task ID: T001", "执行角色: executor.gemini", "评审角色: reviewer.kimi", "Write scope: src/core", "评审状态: changes_requested", "上次评审路径: phases/P01/tasks/T001/reviews/R001.md", "先读那份 review，再去碰 source repo", "Source repos:", "repo-a: local_path="+root, "收尾自检命令:", "alice-code-army.sh task-self-check camp_demo T001 executor", "必须运行一次收尾自检命令并确认退出码为 0") {
		t.Fatalf("unexpected executor prompt: %q", executorSpec.Prompt)
	}

	reviewerSpec, ok := byKind[DispatchKindReviewer]
	if !ok {
		t.Fatal("missing reviewer dispatch spec")
	}
	if reviewerSpec.StateKey != "campaign_dispatch:camp_demo:reviewer:T002:r1" {
		t.Fatalf("unexpected reviewer state key: %q", reviewerSpec.StateKey)
	}
	if reviewerSpec.Role.Provider != "kimi" || reviewerSpec.Role.Model != "kimi-k2" || reviewerSpec.Role.Profile != "review-profile" {
		t.Fatalf("unexpected reviewer role: %+v", reviewerSpec.Role)
	}
	if reviewerSpec.Title != "Demo Campaign · T002 · 评审 · 第 1 轮" {
		t.Fatalf("unexpected reviewer title: %q", reviewerSpec.Title)
	}
	expectedReviewFile := filepath.Join(root, "phases", "P01", "tasks", "T002", "reviews", "R001.md")
	if !containsAll(reviewerSpec.Prompt, "Task ID: T002", "目标 commit: abc123", "上次运行产物路径: results/summary.md", "是否需要 source repo 改动: yes", "Write scope: -", "建议写入的 review 文件: "+expectedReviewFile, "Source repos:", "repo-a: local_path="+root, "收尾自检命令:", "alice-code-army.sh task-self-check camp_demo T002 reviewer", "先验证 `last_run_path` 可解析", "还要确认列出的本地 repo 中 `target_commit` 和 `working_branches` 真实可解析", "diff 没有跑出 `write_scope`", "`created_at` 使用 RFC3339", "必须运行一次收尾自检命令并确认退出码为 0") {
		t.Fatalf("unexpected reviewer prompt: %q", reviewerSpec.Prompt)
	}
}

func TestBuildDispatchSpecs_IntegrationConflictPrompts(t *testing.T) {
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
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "`+root+`"
default_branch: main
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Conflict recovery executor"
phase: P01
status: executing
execution_round: 2
owner_agent: executor
lease_until: "2026-03-24T13:00:00+08:00"
target_repos: [repo-a]
working_branches: [repo-a:feat/t001]
worktree_paths: [repo-a:`+filepath.Join(root, ".worktrees", "repo-a", "t001")+`]
write_scope: [repo-a:src/core]
review_status: changes_requested
integration_retry_count: 1
last_blocked_reason: "merge failed: CONFLICT (content): Merge conflict in src/core"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T002", `---
task_id: T002
title: "Conflict recovery reviewer"
phase: P01
status: reviewing
review_round: 2
owner_agent: reviewer
lease_until: "2026-03-24T13:00:00+08:00"
target_repos: [repo-a]
head_commit: "abc123"
last_run_path: "results/summary.md"
integration_retry_count: 1
last_blocked_reason: "merge failed: CONFLICT (content): Merge conflict in src/core"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T002", "results", "summary.md"), "# Summary\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 dispatch specs, got %d", len(specs))
	}

	byKind := map[DispatchKind]DispatchTaskSpec{}
	for _, spec := range specs {
		byKind[spec.Kind] = spec
	}
	if !containsAll(byKind[DispatchKindExecutor].Prompt, "集成冲突恢复次数: 1", "这是一次集成冲突恢复轮", "把默认分支的新变化合入或等价重放到 task branch") {
		t.Fatalf("unexpected integration recovery executor prompt: %q", byKind[DispatchKindExecutor].Prompt)
	}
	if !containsAll(byKind[DispatchKindReviewer].Prompt, "集成冲突恢复次数: 1", "这是一次集成冲突恢复后的评审", "既保留了此前已批准改动的意图，也兼容默认分支的新变化") {
		t.Fatalf("unexpected integration recovery reviewer prompt: %q", byKind[DispatchKindReviewer].Prompt)
	}
}

func TestBuildDispatchSpecs_CampaignOnlyReviewerPromptSkipsSourceCommitRequirement(t *testing.T) {
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
status: reviewing
review_round: 1
owner_agent: reviewer
lease_until: "2026-03-24T13:00:00+08:00"
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
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 1 || specs[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected single reviewer dispatch spec, got %+v", specs)
	}
	if !containsAll(specs[0].Prompt, "Task ID: T001", "目标 commit: -", "是否需要 source repo 改动: no（仅 campaign / archive 任务）", "不要要求 source-repo `head_commit`", "直接审 task-local artifact 和 campaign-repo diff", "`target_commit` 保持空字符串") {
		t.Fatalf("unexpected campaign-only reviewer prompt: %q", specs[0].Prompt)
	}
}

func TestReconcileAndPrepare_AssignsTaskWorktreeForSourceRepoExecution(t *testing.T) {
	sourceRoot := t.TempDir()
	initGitRepo(t, sourceRoot)
	sourceHead := gitHeadCommit(t, sourceRoot)

	root := t.TempDir()
	now := time.Date(2026, 3, 31, 16, 30, 0, 0, time.FixedZone("CST", 8*3600))

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
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
default_branch: dev
base_commit: "`+sourceHead+`"
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Isolated source repo task"
phase: P01
status: ready
target_repos: [repo-a]
write_scope: [src/core]
review_status: changes_requested
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: concern
blocking: false
created_at: "2026-03-31T16:00:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile and prepare failed: %v", err)
	}
	if result.ClaimedExecutors != 1 {
		t.Fatalf("expected 1 claimed executor, got %d", result.ClaimedExecutors)
	}
	if len(result.Repository.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(result.Repository.Tasks))
	}

	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected executing task, got %s", got)
	}
	expectedBranch := "codearmy/camp_demo/t001/repo-a"
	expectedBranchSpec := "repo-a:" + expectedBranch
	if len(task.Frontmatter.WorkingBranches) != 1 || task.Frontmatter.WorkingBranches[0] != expectedBranchSpec {
		t.Fatalf("unexpected working_branches: %+v", task.Frontmatter.WorkingBranches)
	}
	expectedWorktree := filepath.Join(root, ".worktrees", "repo-a", "t001")
	expectedWorktreeSpec := "repo-a:" + expectedWorktree
	if len(task.Frontmatter.WorktreePaths) != 1 || task.Frontmatter.WorktreePaths[0] != expectedWorktreeSpec {
		t.Fatalf("unexpected worktree_paths: %+v", task.Frontmatter.WorktreePaths)
	}
	if task.Frontmatter.BaseCommit != sourceHead {
		t.Fatalf("expected base_commit=%s, got %s", sourceHead, task.Frontmatter.BaseCommit)
	}
	if !gitWorktreeExists(expectedWorktree) {
		t.Fatalf("expected git worktree at %s", expectedWorktree)
	}
	currentBranch, err := gitCurrentBranch(expectedWorktree)
	if err != nil {
		t.Fatalf("read worktree branch failed: %v", err)
	}
	if currentBranch != expectedBranch {
		t.Fatalf("unexpected worktree branch: got=%s want=%s", currentBranch, expectedBranch)
	}
	sameRepo, err := gitWorktreesShareCommonDir(sourceRoot, expectedWorktree)
	if err != nil {
		t.Fatalf("compare worktree common dir failed: %v", err)
	}
	if !sameRepo {
		t.Fatalf("expected %s to belong to %s", expectedWorktree, sourceRoot)
	}

	if len(result.DispatchTasks) != 1 {
		t.Fatalf("expected one dispatch task, got %d", len(result.DispatchTasks))
	}
	prompt := result.DispatchTasks[0].Prompt
	if !containsAll(
		prompt,
		"task_branch="+expectedBranch,
		"task_worktree="+expectedWorktree,
		"任务 worktree: "+expectedWorktreeSpec,
		"只允许在那条 worktree 里改 source repo",
		"所有 source-repo 编辑、测试和 git 检查都默认在 `task_worktree` 下执行",
		"开始前先确认 `local_path` 干净",
	) {
		t.Fatalf("unexpected executor prompt: %q", prompt)
	}
}

func TestBuildDispatchSpecs_SkipsDanglingExecutingTaskWithoutOwnerLease(t *testing.T) {
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
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("expected dangling executing task to produce no dispatch specs, got %+v", specs)
	}
}

func TestBuildDispatchSpecs_PlannerPromptIncludesMasterPlanContract(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600))

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship a concrete plan"
source_repos: [repo-a]
current_phase: P01
plan_round: 1
plan_status: planning
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: planned
goal: "Phase placeholder"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "/tmp/repo-a"
default_branch: main
role: source
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 dispatch spec, got %d", len(specs))
	}
	spec := specs[0]
	if spec.Kind != DispatchKindPlanner {
		t.Fatalf("unexpected dispatch kind: %s", spec.Kind)
	}
	if spec.Title != "Demo Campaign · 规划 · 第 1 轮" {
		t.Fatalf("unexpected planner title: %q", spec.Title)
	}
	if !containsAll(
		spec.Prompt,
		"第 1 轮规划，campaign repo 为 `"+root+"`",
		"Campaign ID: camp_demo",
		"下面提到的所有路径都相对于该 repo。",
		"`plans/proposals/round-001-plan.md`",
		"`plans/merged/master-plan.md`",
		"`phases/Pxx/tasks/Txxx/{task.md,context.md,plan.md,progress.md,results/README.md,reviews/README.md}`",
		"`context.md` 的 Context / Relevant Repos / Relevant Files / Dependencies",
		"`alice-code-army repo-lint camp_demo`",
		"$ALICE_RUNTIME_BIN runtime campaigns repo-lint camp_demo",
		"$ALICE_RUNTIME_BIN runtime campaigns plan-self-check camp_demo planner 1",
		"proposal / master-plan / phases / tasks 在 phase 目标、task ID、depends_on、target_repos、write_scope、acceptance、parallelism 上必须一致",
		"必须保持 `status: draft`",
		"不得产出 `planned`、`ready`、`executing` 或任何 `review_*` 状态",
		"executor 只靠 task 文件夹就能开工",
		"优先拆成更窄、更清晰的 task",
		"不要往 `campaign.md` 里加入 `default_*`",
		"必须运行一次收尾自检命令并确认退出码为 0",
		"不要先跑 lint / self-check 再写最终文件",
		"简短中文公开总结",
	) {
		t.Fatalf("unexpected planner prompt: %q", spec.Prompt)
	}
}

func TestReconcileAndPrepare_HumanApprovedPromotesPlannedTasksToDispatch(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 29, 19, 0, 0, 0, time.FixedZone("CST", 8*3600))

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Start execution after approval"
current_phase: P01
source_repos: [repo-a]
plan_round: 2
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "/tmp/repo-a"
default_branch: main
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Legacy planned task"
phase: P01
status: planned
target_repos: [repo-a]
write_scope: [src/**]
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.ClaimedExecutors != 1 {
		t.Fatalf("expected 1 claimed executor, got %d", result.ClaimedExecutors)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindExecutor {
		t.Fatalf("expected one executor dispatch, got %+v", result.DispatchTasks)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected task to move to executing, got %s", got)
	}
	if task.Frontmatter.DispatchState != "executor_dispatched" {
		t.Fatalf("expected executor_dispatched, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.ExecutionRound != 1 {
		t.Fatalf("expected execution round 1, got %d", task.Frontmatter.ExecutionRound)
	}
}

func TestReconcileAndPrepare_AutoRetriesReviewPendingArtifactBlocker(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 29, 20, 0, 0, 0, time.FixedZone("CST", 8*3600))
	sourceRoot := filepath.Join(root, "source")
	initGitRepo(t, sourceRoot)
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
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
title: "Retry me"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
execution_round: 1
review_status: pending
head_commit: "`+headCommit+`"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusExecuting {
		t.Fatalf("expected task to be re-dispatched for execution, got %s", got)
	}
	if task.Frontmatter.ExecutionRound != 1 {
		t.Fatalf("expected artifact repair to keep execution round 1, got %d", task.Frontmatter.ExecutionRound)
	}
	if task.Frontmatter.AutoRetryCount != 1 {
		t.Fatalf("expected auto retry count 1, got %d", task.Frontmatter.AutoRetryCount)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "working_branches") {
		t.Fatalf("expected blocked reason to mention working_branches, got %q", task.Frontmatter.LastBlockedReason)
	}
	if task.Frontmatter.DispatchState != dispatchStateArtifactRepairDispatched {
		t.Fatalf("expected artifact_repair_dispatched after auto retry, got %q", task.Frontmatter.DispatchState)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindExecutor {
		t.Fatalf("expected one executor dispatch task, got %+v", result.DispatchTasks)
	}
	if !strings.Contains(result.DispatchTasks[0].StateKey, ":a1") {
		t.Fatalf("expected artifact repair state key suffix, got %q", result.DispatchTasks[0].StateKey)
	}
}

func TestReconcileAndPrepare_StopsAutoRetryingAfterThreeAttempts(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 29, 20, 0, 0, 0, time.FixedZone("CST", 8*3600))
	sourceRoot := filepath.Join(root, "source")
	initGitRepo(t, sourceRoot)
	headCommit := gitHeadCommit(t, sourceRoot)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
source_repos: [repo-a]
plan_status: human_approved
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship phase one"
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
title: "Retry me"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
execution_round: 4
auto_retry_count: 3
review_status: pending
head_commit: "`+headCommit+`"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusBlocked {
		t.Fatalf("expected task to escalate to blocked after retry budget is exhausted, got %s", got)
	}
	if task.Frontmatter.AutoRetryCount != 3 {
		t.Fatalf("expected auto retry count to stay at 3, got %d", task.Frontmatter.AutoRetryCount)
	}
	if task.Frontmatter.DispatchState != dispatchStateNeedsHuman {
		t.Fatalf("expected needs_human dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if len(result.DispatchTasks) != 0 {
		t.Fatalf("expected no dispatch tasks once retry budget is exhausted, got %+v", result.DispatchTasks)
	}
	if len(result.Summary.BlockedTasks) != 1 || result.Summary.BlockedTasks[0].TaskID != "T001" {
		t.Fatalf("expected exhausted task to stay blocked in summary, got %+v", result.Summary.BlockedTasks)
	}
}

func TestReconcileAndPrepare_EscalatesRepeatedReviewTargetLoopToNeedsHuman(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 29, 20, 0, 0, 0, time.FixedZone("CST", 8*3600))
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
title: "Repeated artifact review loop"
phase: P01
status: rework
write_scope: [campaign:phases/P01/tasks/T001/**]
execution_round: 4
review_round: 2
review_status: changes_requested
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R002.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: concern
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R002.md"), `---
review_id: R002
target_task: T001
review_round: 2
verdict: concern
blocking: false
created_at: "2026-03-24T11:30:00+08:00"
---
`)

	result, err := ReconcileAndPrepare(root, now, 1, time.Hour)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusBlocked {
		t.Fatalf("expected repeated review target to escalate to blocked, got %s", got)
	}
	if task.Frontmatter.DispatchState != dispatchStateNeedsHuman {
		t.Fatalf("expected needs_human dispatch state, got %q", task.Frontmatter.DispatchState)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "review rounds 2 and 1") {
		t.Fatalf("expected loop reason to mention repeated rounds, got %q", task.Frontmatter.LastBlockedReason)
	}
	if !containsEventKind(result.Events, EventTaskNeedsHuman) {
		t.Fatalf("expected needs-human event, got %+v", result.Events)
	}
	foundTemplate := false
	for _, event := range result.Events {
		if event.Kind != EventTaskNeedsHuman {
			continue
		}
		if strings.Contains(event.Detail, "/alice guide-task T001 accept <接受说明>") &&
			strings.Contains(event.Detail, "/alice guide-task T001 resume <恢复说明>") {
			foundTemplate = true
			break
		}
	}
	if !foundTemplate {
		t.Fatalf("expected needs-human event to include guide-task templates, got %+v", result.Events)
	}
}

func TestBuildDispatchSpecs_PlannerReviewerPromptChecksConsistency(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600))

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Review the plan"
source_repos: [repo-a]
current_phase: P01
plan_round: 1
plan_status: plan_review_pending
---
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-001-plan.md"), `---
proposal_id: "plan-r1"
plan_round: 1
status: submitted
---

# Plan Proposal
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "merged", "master-plan.md"), `---
status: draft
human_approved: false
---

# Master Plan
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: planned
goal: "Phase goal"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "repos", "repo-a.md"), `---
repo_id: repo-a
local_path: "/tmp/repo-a"
default_branch: main
role: source
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Planned task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope: [src/**]
---
`)

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	specs, err := buildDispatchSpecs(repo, now)
	if err != nil {
		t.Fatalf("build dispatch specs failed: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 dispatch spec, got %d", len(specs))
	}
	spec := specs[0]
	if spec.Kind != DispatchKindPlannerReviewer {
		t.Fatalf("unexpected dispatch kind: %s", spec.Kind)
	}
	if spec.Title != "Demo Campaign · 规划评审 · 第 1 轮" {
		t.Fatalf("unexpected planner reviewer title: %q", spec.Title)
	}
	if !containsAll(
		spec.Prompt,
		"评审第 1 轮计划是否已经可以交给人类审批，campaign repo 为 `"+root+"`",
		"Campaign ID: camp_demo",
		"下面提到的所有路径都相对于该 repo。",
		"`plans/proposals/round-001-plan.md`",
		"`plans/merged/master-plan.md`",
		"`alice-code-army repo-lint camp_demo`",
		"$ALICE_RUNTIME_BIN runtime campaigns repo-lint camp_demo",
		"$ALICE_RUNTIME_BIN runtime campaigns plan-self-check camp_demo planner_reviewer 1",
		"`created_at` 使用 RFC3339",
		"proposal / master-plan / phases / tasks 在 phase 目标、task ID、depends_on、target_repos、write_scope、acceptance、parallelism 上必须一致",
		"不要要求 planner 去替换那里静态说明文字或占位提示",
		"本轮评审不要使用 `repo-lint --for-approval`",
		"任务粒度过粗、只能靠聊天补 context、验收不可观察，也通常至少是 `concern`",
		"只有在你确认**完全没有剩余问题、没有注意事项、没有待人类补判边界**时，才允许写 `approve`",
		"只要还有任何问题，无论大小，都不允许带着备注 `approve`",
		"必须运行一次收尾自检命令并确认退出码为 0",
		"如果 verdict 是 `approve`，review 正文里不应再保留非空的 `## Concerns`",
		"优先级最高的返工 task ID",
	) {
		t.Fatalf("unexpected planner reviewer prompt: %q", spec.Prompt)
	}
}

func TestReconcileAndPrepare_RequiresPlannerSelfCheckBeforePlanReviewPending(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	initGitRepo(t, root)

	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
objective: "Ship a concrete plan"
source_repos: [repo-a]
current_phase: P01
plan_round: 1
plan_status: planning
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: planned
goal: "Phase one"
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
title: "Planner task"
phase: P01
status: draft
target_repos: [repo-a]
write_scope: [campaign:phases/P01/tasks/T001/**]
executor:
  role: executor
  workflow: code_army
reviewer:
  role: reviewer
  workflow: code_army
---

# Task

## Goal
- write a concrete planner task

## Background
- enough background

## Acceptance
- acceptance is clear

## Deliverables
- deliver the package
`)
	mustWriteTestFile(t, filepath.Join(root, "plans", "proposals", "round-001-plan.md"), `---
proposal_id: "plan-r1"
plan_round: 1
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

	result, err := ReconcileAndPrepare(root, now, 2, defaultDispatchLease)
	if err != nil {
		t.Fatalf("reconcile before self-check failed: %v", err)
	}
	if got := normalizePlanStatus(result.Repository.Campaign.Frontmatter.PlanStatus); got != PlanStatusPlanning {
		t.Fatalf("expected plan_status to stay planning before self-check, got %s", got)
	}
	if len(result.DispatchTasks) != 0 {
		t.Fatalf("expected no dispatch tasks before planner self-check, got %+v", result.DispatchTasks)
	}
	if result.Summary.RepositoryIssueCount == 0 {
		t.Fatal("expected repository issues to mention missing planner self-check proof")
	}

	validation, err := RunPlanSelfCheck(root, DispatchKindPlanner, 1, now)
	if err != nil {
		t.Fatalf("run planner self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected planner self-check to pass, got %+v", validation.Issues)
	}

	result, err = ReconcileAndPrepare(root, now, 2, defaultDispatchLease)
	if err != nil {
		t.Fatalf("reconcile after self-check failed: %v", err)
	}
	if got := normalizePlanStatus(result.Repository.Campaign.Frontmatter.PlanStatus); got != PlanStatusPlanReviewPending {
		t.Fatalf("expected plan_status to advance to plan_review_pending, got %s", got)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindPlannerReviewer {
		t.Fatalf("expected planner reviewer dispatch after self-check, got %+v", result.DispatchTasks)
	}
}

func TestReconcileAndPrepare_PlanningPhaseStillAppliesCompletedReviewVerdicts(t *testing.T) {
	root := t.TempDir()
	mustWriteTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_round: 4
plan_status: planning
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
title: "Review completed before replanning finishes"
phase: P01
status: reviewing
dispatch_state: reviewer_dispatched
reviewer:
  role: reviewer.codex
write_scope: [campaign:reports/live-report.md]
execution_round: 2
review_round: 4
review_status: reviewing
owner_agent: reviewer.codex
lease_until: "2026-03-24T12:00:00+08:00"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R003.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R003.md"), `---
review_id: R003
target_task: T001
review_round: 3
reviewer:
  role: reviewer.codex
verdict: approve
blocking: false
created_at: "2026-03-24T10:00:00+08:00"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R004.md"), `---
review_id: R004
target_task: T001
review_round: 4
reviewer:
  role: reviewer.codex
verdict: concern
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	checkedAt := time.Date(2026, 3, 24, 10, 45, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunTaskSelfCheck(root, "T001", DispatchKindReviewer, checkedAt)
	if err != nil {
		t.Fatalf("reviewer self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected reviewer self-check to pass, got %+v", validation.Issues)
	}

	now := time.Date(2026, 3, 24, 11, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, pragmaticReviewerRoleDefaults())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.AppliedReviews != 1 {
		t.Fatalf("expected completed review verdict to be applied during planning, got %d", result.AppliedReviews)
	}
	if result.ClaimedExecutors != 0 || result.ClaimedReviewers != 0 {
		t.Fatalf("did not expect planning reconcile to claim new tasks, got executors=%d reviewers=%d", result.ClaimedExecutors, result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindPlanner {
		t.Fatalf("expected only planner dispatch to remain during planning, got %+v", result.DispatchTasks)
	}

	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusRework {
		t.Fatalf("expected task to move to rework after concern verdict, got %s", got)
	}
	if task.Frontmatter.ReviewStatus != "changes_requested" {
		t.Fatalf("expected review_status=changes_requested, got %q", task.Frontmatter.ReviewStatus)
	}
	if task.Frontmatter.LastReviewPath != "phases/P01/tasks/T001/reviews/R004.md" {
		t.Fatalf("expected last_review_path to advance to R004, got %q", task.Frontmatter.LastReviewPath)
	}
}

func TestReconcileAndPrepare_ClearsExecutorLeaseBeforeReviewerDispatch(t *testing.T) {
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
current_phase: P01
plan_status: human_approved
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
title: "Needs review dispatch"
phase: P01
status: review_pending
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
owner_agent: executor.kimi
lease_until: "2026-03-28T17:45:21+08:00"
execution_round: 1
review_round: 0
base_commit: "`+baseCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/summary.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	now := time.Date(2026, 3, 28, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, CampaignRoleDefaults{
		Reviewer: RoleConfig{
			Role:            "reviewer.codex",
			Provider:        "codex",
			Model:           "gpt-5.4",
			Profile:         "reviewer",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.ClaimedReviewers != 1 {
		t.Fatalf("expected 1 claimed reviewer, got %d", result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected one reviewer dispatch, got %+v", result.DispatchTasks)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected task to move to reviewing, got %s", got)
	}
	if task.Frontmatter.OwnerAgent != "reviewer.codex" {
		t.Fatalf("expected reviewer to own task, got %q", task.Frontmatter.OwnerAgent)
	}
	if task.LeaseUntil.IsZero() {
		t.Fatal("expected reviewer lease to be claimed")
	}
	if task.Frontmatter.ReviewRound != 1 {
		t.Fatalf("expected review round 1, got %d", task.Frontmatter.ReviewRound)
	}

	updated, err := Load(root)
	if err != nil {
		t.Fatalf("reload repo failed: %v", err)
	}
	task = updated.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected persisted task status reviewing, got %s", got)
	}
	if task.Frontmatter.OwnerAgent != "reviewer.codex" {
		t.Fatalf("expected persisted reviewer owner, got %q", task.Frontmatter.OwnerAgent)
	}
	if task.LeaseUntil.IsZero() {
		t.Fatal("expected persisted reviewer lease")
	}
}

func TestReconcileAndPrepare_RepairsDanglingExecutorHandOffBeforeReviewerDispatch(t *testing.T) {
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
current_phase: P01
plan_status: human_approved
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
title: "Executor finished but forgot status handoff"
phase: P01
status: executing
dispatch_state: executor_dispatched
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
owner_agent: ""
lease_until: ""
execution_round: 2
review_round: 2
review_status: pending
base_commit: "`+baseCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: concern
blocking: false
target_commit: "`+baseCommit+`"
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	now := time.Date(2026, 3, 28, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, CampaignRoleDefaults{
		Reviewer: RoleConfig{
			Role:            "reviewer.codex",
			Provider:        "codex",
			Model:           "gpt-5.4",
			Profile:         "reviewer",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.AppliedReviews != 0 {
		t.Fatalf("expected no old review verdict to be re-applied, got %d", result.AppliedReviews)
	}
	if result.ClaimedReviewers != 1 {
		t.Fatalf("expected 1 claimed reviewer after repair, got %d", result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected one reviewer dispatch, got %+v", result.DispatchTasks)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected task to move to reviewing, got %s", got)
	}
	if task.Frontmatter.OwnerAgent != "reviewer.codex" {
		t.Fatalf("expected reviewer to own repaired task, got %q", task.Frontmatter.OwnerAgent)
	}
	if task.Frontmatter.ReviewRound != 2 {
		t.Fatalf("expected repaired task to dispatch reviewer round 2, got %d", task.Frontmatter.ReviewRound)
	}
}

func TestReconcileAndPrepare_RepairsTerminalPostRunValidationExecutorBlock(t *testing.T) {
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
goal: "Ship the first phase"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Executor self-check proof was missing"
phase: P01
status: blocked
dispatch_state: signal_blocked_terminal
write_scope: [campaign:reports/live-report.md]
execution_round: 2
review_round: 1
review_status: blocked
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
last_blocked_reason: "post-run validation failed after executor round: task T001 finished an executor round but has no recorded self-check proof"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
reviewer:
  role: reviewer.codex
verdict: concern
blocking: false
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	checkedAt := time.Date(2026, 3, 28, 15, 30, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunTaskSelfCheck(root, "T001", DispatchKindExecutor, checkedAt)
	if err != nil {
		t.Fatalf("executor self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected executor self-check to pass, got %+v", validation.Issues)
	}

	now := time.Date(2026, 3, 28, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, pragmaticReviewerRoleDefaults())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.AppliedReviews != 0 {
		t.Fatalf("expected no review verdict to be applied, got %d", result.AppliedReviews)
	}
	if result.ClaimedReviewers != 1 {
		t.Fatalf("expected repaired task to be dispatched for review, got %d reviewer claims", result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected one reviewer dispatch, got %+v", result.DispatchTasks)
	}

	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected task to move to reviewing, got %s", got)
	}
	if task.Frontmatter.OwnerAgent != "reviewer.codex" {
		t.Fatalf("expected reviewer to own repaired task, got %q", task.Frontmatter.OwnerAgent)
	}
	if task.Frontmatter.ReviewRound != 2 {
		t.Fatalf("expected fresh reviewer round 2, got %d", task.Frontmatter.ReviewRound)
	}
	if task.Frontmatter.LastBlockedReason != "" {
		t.Fatalf("expected blocked reason to be cleared, got %q", task.Frontmatter.LastBlockedReason)
	}
}

func TestReconcileAndPrepare_RepairsTerminalPostRunValidationReviewerBlock(t *testing.T) {
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
goal: "Ship the first phase"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Reviewer proof was missing"
phase: P01
status: blocked
dispatch_state: signal_blocked_terminal
reviewer:
  role: reviewer.codex
write_scope: [campaign:reports/live-report.md]
execution_round: 2
review_round: 2
review_status: blocked
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R002.md"
last_blocked_reason: "post-run validation failed after reviewer round: task T001 finished a reviewer round but has no recorded self-check proof"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R002.md"), `---
review_id: R002
target_task: T001
review_round: 2
reviewer:
  role: reviewer.codex
verdict: approve
blocking: false
created_at: "2026-03-24T11:00:00+08:00"
---
`)

	checkedAt := time.Date(2026, 3, 28, 15, 45, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunTaskSelfCheck(root, "T001", DispatchKindReviewer, checkedAt)
	if err != nil {
		t.Fatalf("reviewer self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected reviewer self-check to pass, got %+v", validation.Issues)
	}

	now := time.Date(2026, 3, 28, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, pragmaticReviewerRoleDefaults())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.AppliedReviews != 1 {
		t.Fatalf("expected repaired review verdict to be applied, got %d", result.AppliedReviews)
	}
	if result.ClaimedReviewers != 0 {
		t.Fatalf("did not expect a fresh reviewer dispatch, got %d", result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 0 {
		t.Fatalf("expected no dispatch tasks after judge apply, got %+v", result.DispatchTasks)
	}

	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusDone {
		t.Fatalf("expected task to become done after judge apply/integration, got %s", got)
	}
	if task.Frontmatter.DispatchState != "integration_not_required" {
		t.Fatalf("expected dispatch_state=integration_not_required, got %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.LastBlockedReason != "" {
		t.Fatalf("expected blocked reason to be cleared, got %q", task.Frontmatter.LastBlockedReason)
	}
}

func TestReconcileAndPrepare_DoesNotRepairNonPostRunTerminalBlock(t *testing.T) {
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
goal: "Ship the first phase"
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Real external blocker"
phase: P01
status: blocked
dispatch_state: signal_blocked_terminal
write_scope: [campaign:reports/live-report.md]
execution_round: 2
review_round: 1
review_status: blocked
last_run_path: "results/summary.md"
last_blocked_reason: "missing IHEP cluster handoff"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")

	checkedAt := time.Date(2026, 3, 28, 15, 50, 0, 0, time.FixedZone("CST", 8*3600))
	validation, err := RunTaskSelfCheck(root, "T001", DispatchKindExecutor, checkedAt)
	if err != nil {
		t.Fatalf("executor self-check failed: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected executor self-check to pass, got %+v", validation.Issues)
	}

	now := time.Date(2026, 3, 28, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, pragmaticReviewerRoleDefaults())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.AppliedReviews != 0 {
		t.Fatalf("expected no applied reviews, got %d", result.AppliedReviews)
	}
	if result.ClaimedReviewers != 0 {
		t.Fatalf("did not expect blocked task to dispatch reviewer, got %d", result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 0 {
		t.Fatalf("expected no dispatch tasks, got %+v", result.DispatchTasks)
	}

	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusBlocked {
		t.Fatalf("expected task to remain blocked, got %s", got)
	}
	if task.Frontmatter.DispatchState != "signal_blocked_terminal" {
		t.Fatalf("unexpected dispatch state: %q", task.Frontmatter.DispatchState)
	}
	if task.Frontmatter.LastBlockedReason != "missing IHEP cluster handoff" {
		t.Fatalf("expected blocked reason to stay intact, got %q", task.Frontmatter.LastBlockedReason)
	}
}

func TestTaskLooksReadyForReviewHandOff_AcceptsPendingAliases(t *testing.T) {
	task := TaskDocument{
		Frontmatter: TaskFrontmatter{
			LastRunPath:  "results/summary.md",
			ReviewStatus: "pending",
		},
	}
	if !taskLooksReadyForReviewHandOff(task) {
		t.Fatal("expected pending review status to be repairable")
	}
	task.Frontmatter.ReviewStatus = "review_pending"
	if !taskLooksReadyForReviewHandOff(task) {
		t.Fatal("expected review_pending alias to be repairable")
	}
	task.Frontmatter.ReviewStatus = "changes_requested"
	if taskLooksReadyForReviewHandOff(task) {
		t.Fatal("did not expect changes_requested to be treated as ready for review handoff")
	}
}

func TestReconcileAndPrepare_DoesNotReapplySameReviewWhenTaskReturnsToReviewPending(t *testing.T) {
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
current_phase: P01
plan_status: human_approved
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
title: "Needs fresh review"
phase: P01
status: review_pending
dispatch_state: executor_completed
target_repos: [repo-a]
working_branches: [codearmy/t001]
write_scope: [src/lib.rs]
execution_round: 2
review_round: 1
review_status: pending
base_commit: "`+baseCommit+`"
head_commit: "`+headCommit+`"
last_run_path: "results/summary.md"
last_review_path: "phases/P01/tasks/T001/reviews/R001.md"
---
`)
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "results", "summary.md"), "# Summary\n")
	mustWriteTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "reviews", "R001.md"), `---
review_id: R001
target_task: T001
review_round: 1
verdict: concern
blocking: false
target_commit: "`+baseCommit+`"
created_at: "2026-03-24T10:30:00+08:00"
---
`)

	now := time.Date(2026, 3, 28, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := ReconcileAndPrepare(root, now, 2, time.Hour, CampaignRoleDefaults{
		Reviewer: RoleConfig{
			Role:            "reviewer.codex",
			Provider:        "codex",
			Model:           "gpt-5.4",
			Profile:         "reviewer",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
		},
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.AppliedReviews != 0 {
		t.Fatalf("expected old review not to be re-applied, got %d", result.AppliedReviews)
	}
	if result.ClaimedReviewers != 1 {
		t.Fatalf("expected 1 claimed reviewer, got %d", result.ClaimedReviewers)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected one reviewer dispatch, got %+v", result.DispatchTasks)
	}
	task := result.Repository.Tasks[0]
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewing {
		t.Fatalf("expected task to move to reviewing, got %s", got)
	}
	if task.Frontmatter.ReviewRound != 2 {
		t.Fatalf("expected fresh reviewer round 2, got %d", task.Frontmatter.ReviewRound)
	}
}

func TestLatestRelevantReview_RoundAndTimeOrdering(t *testing.T) {
	task := TaskDocument{
		Frontmatter: TaskFrontmatter{
			TaskID:      "T001",
			ReviewRound: 2,
		},
	}
	reviews := []ReviewDocument{
		{
			Path:      "phases/P01/tasks/T001/reviews/R001.md",
			Dir:       "phases/P01/tasks/T001/reviews",
			TaskDir:   "phases/P01/tasks/T001",
			CreatedAt: time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC),
			Frontmatter: ReviewFrontmatter{
				TargetTask:  "T001",
				ReviewRound: 1,
				Verdict:     "approve",
			},
		},
		{
			Path:      "phases/P01/tasks/T001/reviews/R002.md",
			Dir:       "phases/P01/tasks/T001/reviews",
			TaskDir:   "phases/P01/tasks/T001",
			CreatedAt: time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC),
			Frontmatter: ReviewFrontmatter{
				TargetTask:  "T001",
				ReviewRound: 2,
				Verdict:     "concern",
			},
		},
		{
			Path:      "phases/P01/tasks/T001/reviews/R003.md",
			Dir:       "phases/P01/tasks/T001/reviews",
			TaskDir:   "phases/P01/tasks/T001",
			CreatedAt: time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC),
			Frontmatter: ReviewFrontmatter{
				TargetTask:  "T001",
				ReviewRound: 2,
				Verdict:     "approve",
			},
		},
	}

	chosen, ok := latestRelevantReview(task, reviews)
	if !ok {
		t.Fatal("expected latest relevant review to be found")
	}
	if chosen.Path != "phases/P01/tasks/T001/reviews/R003.md" {
		t.Fatalf("unexpected chosen review: %q", chosen.Path)
	}
	if chosen.Frontmatter.ReviewRound != 2 {
		t.Fatalf("unexpected review round: %d", chosen.Frontmatter.ReviewRound)
	}
}

func boolYAML(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func pragmaticReviewerRoleDefaults() CampaignRoleDefaults {
	return CampaignRoleDefaults{
		Reviewer: RoleConfig{
			Role:            "reviewer.codex",
			Provider:        "codex",
			Model:           "gpt-5.4",
			Profile:         "reviewer",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
		},
	}
}

func containsAll(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if !strings.Contains(text, pattern) {
			return false
		}
	}
	return true
}

func containsEventKind(events []ReconcileEvent, want ReconcileEventKind) bool {
	for _, event := range events {
		if event.Kind == want {
			return true
		}
	}
	return false
}
