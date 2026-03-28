package campaignrepo

import (
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
	mustWriteTestTaskPackage(t, root, "P01", "T001", `---
task_id: T001
title: "Execute me"
phase: P01
status: executing
execution_round: 2
target_repos: [repo-a]
working_branches: [feat/t001]
write_scope: [src/core]
---
`)
	mustWriteTestTaskPackage(t, root, "P01", "T002", `---
task_id: T002
title: "Review me"
phase: P01
status: reviewing
review_round: 1
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
	if !containsAll(executorSpec.Prompt, "Task id: T001", "Executor role: executor.gemini", "Reviewer role: reviewer.kimi", "Write scope: src/core") {
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
	expectedReviewFile := filepath.Join(root, "phases", "P01", "tasks", "T002", "reviews", "R001.md")
	if !containsAll(reviewerSpec.Prompt, "Task id: T002", "Target commit: abc123", "Last run path: results/summary.md", "Suggested review file: "+expectedReviewFile) {
		t.Fatalf("unexpected reviewer prompt: %q", reviewerSpec.Prompt)
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
	masterPlanPath := filepath.Join(root, "plans", "merged", "master-plan.md")
	if !containsAll(
		spec.Prompt,
		"Master plan output: "+masterPlanPath,
		"Keep repo-first truth consistent across proposal, master plan, phase docs, and task packages",
		"if you say you changed a dependency, acceptance criterion, or output contract in the proposal, you must update the relevant `phase.md`, `task.md`, `context.md`, `plan.md`, and `master-plan.md` too",
	) {
		t.Fatalf("unexpected planner prompt: %q", spec.Prompt)
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
	masterPlanPath := filepath.Join(root, "plans", "merged", "master-plan.md")
	if !containsAll(
		spec.Prompt,
		"Master plan: "+masterPlanPath,
		"the proposal, master plan, phase docs, and task packages agree on phase goals, task IDs, depends_on, target_repos, write_scope, acceptance criteria, and parallelism notes",
		"Missing merged-plan content or inconsistent plan artifacts should usually be `concern`, not `blocking`",
	) {
		t.Fatalf("unexpected planner reviewer prompt: %q", spec.Prompt)
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
