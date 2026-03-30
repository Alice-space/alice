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
owner_agent: reviewer.claude
lease_until: "2026-03-24T12:00:00+08:00"
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
	if len(result.Events) == 0 || result.Events[0].Kind != EventTaskBlocked {
		t.Fatalf("expected blocked notification event, got %+v", result.Events)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindReviewer {
		t.Fatalf("expected reviewer dispatch after blocked handoff, got %+v", result.DispatchTasks)
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
	if !containsAll(executorSpec.Prompt, "Task id: T001", "Executor role: executor.gemini", "Reviewer role: reviewer.kimi", "Write scope: src/core", "Review status: changes_requested", "Last review path: phases/P01/tasks/T001/reviews/R001.md", "read that review before touching the source repo", "Source repos:", "repo-a: local_path="+root) {
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
	if !containsAll(reviewerSpec.Prompt, "Task id: T002", "Target commit: abc123", "Last run path: results/summary.md", "Source repo changes required: yes", "Write scope: -", "Suggested review file: "+expectedReviewFile, "Source repos:", "repo-a: local_path="+root, "Verify that `last_run_path` resolves", "also verify that `target_commit` and `working_branches` resolve", "diff stays inside `write_scope`", "Use RFC3339 for `created_at`") {
		t.Fatalf("unexpected reviewer prompt: %q", reviewerSpec.Prompt)
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
	if !containsAll(specs[0].Prompt, "Task id: T001", "Target commit: -", "Source repo changes required: no (campaign-only/archive-only task)", "do not require a source-repo `head_commit`", "task-local artifacts and campaign-repo diff instead") {
		t.Fatalf("unexpected campaign-only reviewer prompt: %q", specs[0].Prompt)
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
		"Plan round 1 for campaign repo `"+root+"`",
		"Campaign ID: camp_demo",
		"All paths below are relative to that repo.",
		"`plans/proposals/round-001-plan.md`",
		"`plans/merged/master-plan.md`",
		"`phases/Pxx/tasks/Txxx/{task.md,context.md,plan.md,progress.md,results/README.md,reviews/README.md}`",
		"`context.md` Context/Relevant Repos/Relevant Files/Dependencies",
		"`alice-code-army repo-lint camp_demo`",
		"$ALICE_RUNTIME_BIN runtime campaigns repo-lint camp_demo",
		"Keep proposal/master-plan/phases/tasks consistent",
		"must keep `status: draft`",
		"must not emit `planned`, `ready`, `executing`, or any `review_*` status during planning",
		"Keep validation inside each task's `write_scope`",
		"Verify claims from files or command output",
		"Never add `default_*` to `campaign.md`",
		"give a short public summary",
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
	if task.Frontmatter.ExecutionRound != 2 {
		t.Fatalf("expected execution round 2 after auto retry, got %d", task.Frontmatter.ExecutionRound)
	}
	if task.Frontmatter.AutoRetryCount != 1 {
		t.Fatalf("expected auto retry count 1, got %d", task.Frontmatter.AutoRetryCount)
	}
	if !strings.Contains(task.Frontmatter.LastBlockedReason, "working_branches") {
		t.Fatalf("expected blocked reason to mention working_branches, got %q", task.Frontmatter.LastBlockedReason)
	}
	if task.Frontmatter.DispatchState != "executor_dispatched" {
		t.Fatalf("expected executor_dispatched after auto retry, got %q", task.Frontmatter.DispatchState)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].Kind != DispatchKindExecutor {
		t.Fatalf("expected one executor dispatch task, got %+v", result.DispatchTasks)
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
	if got := normalizeTaskStatus(task.Frontmatter.Status); got != TaskStatusReviewPending {
		t.Fatalf("expected task to stay review_pending after retry budget is exhausted, got %s", got)
	}
	if task.Frontmatter.AutoRetryCount != 3 {
		t.Fatalf("expected auto retry count to stay at 3, got %d", task.Frontmatter.AutoRetryCount)
	}
	if len(result.DispatchTasks) != 0 {
		t.Fatalf("expected no dispatch tasks once retry budget is exhausted, got %+v", result.DispatchTasks)
	}
	if len(result.Summary.BlockedTasks) != 1 || result.Summary.BlockedTasks[0].TaskID != "T001" {
		t.Fatalf("expected exhausted task to stay blocked in summary, got %+v", result.Summary.BlockedTasks)
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
		"Review whether round 1 is ready for human approval for campaign repo `"+root+"`",
		"Campaign ID: camp_demo",
		"All paths below are relative to that repo.",
		"`plans/proposals/round-001-plan.md`",
		"`plans/merged/master-plan.md`",
		"`alice-code-army repo-lint camp_demo`",
		"$ALICE_RUNTIME_BIN runtime campaigns repo-lint camp_demo",
		"Use RFC3339 for `created_at`",
		"proposal/master-plan/phases/tasks agree on phase goals, task IDs, depends_on, target_repos, write_scope, acceptance, and parallelism",
		"do not require planner to replace static guidance text or placeholders there unless frontmatter/objective/source-repo facts are actually inconsistent",
		"do not use `repo-lint --for-approval` in this review",
		"placeholder task packages, or inconsistent artifacts are usually `concern`, not `blocking`",
		"give a short public summary with the review path, verdict, and repo-lint result",
	) {
		t.Fatalf("unexpected planner reviewer prompt: %q", spec.Prompt)
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

func containsAll(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if !strings.Contains(text, pattern) {
			return false
		}
	}
	return true
}
