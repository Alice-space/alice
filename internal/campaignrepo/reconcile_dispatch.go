package campaignrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	campaignRepoPromptExecutorDispatch        = "campaignrepo/executor_dispatch.md.tmpl"
	campaignRepoPromptReviewerDispatch        = "campaignrepo/reviewer_dispatch.md.tmpl"
	campaignRepoPromptPlannerDispatch         = "campaignrepo/planner_dispatch.md.tmpl"
	campaignRepoPromptPlannerReviewerDispatch = "campaignrepo/planner_reviewer_dispatch.md.tmpl"
)

type DispatchKind string

const (
	DispatchKindExecutor        DispatchKind = "executor"
	DispatchKindReviewer        DispatchKind = "reviewer"
	DispatchKindPlanner         DispatchKind = "planner"
	DispatchKindPlannerReviewer DispatchKind = "planner_reviewer"
)

type DispatchTaskSpec struct {
	StateKey string       `json:"state_key"`
	Kind     DispatchKind `json:"kind"`
	TaskID   string       `json:"task_id"`
	Title    string       `json:"title"`
	TaskPath string       `json:"task_path"`
	RunAt    time.Time    `json:"run_at"`
	Prompt   string       `json:"prompt"`
	Role     RoleConfig   `json:"role"`
}

func buildDispatchSpecs(repo Repository, now time.Time) ([]DispatchTaskSpec, error) {
	if now.IsZero() {
		now = time.Now().Local()
	}
	var specs []DispatchTaskSpec

	campaignID := blankForKey(repo.Campaign.Frontmatter.CampaignID)
	planStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)
	planRound := repo.Campaign.Frontmatter.PlanRound

	if planStatus == PlanStatusPlanning && !hasSubmittedProposal(repo.PlanProposals, planRound) {
		role := resolvePlannerRole(repo)
		prompt, err := buildPlannerDispatchPrompt(repo, role)
		if err != nil {
			return nil, err
		}
		specs = append(specs, DispatchTaskSpec{
			StateKey: fmt.Sprintf("campaign_dispatch:%s:planner:r%d", campaignID, planRound),
			Kind:     DispatchKindPlanner,
			TaskID:   fmt.Sprintf("plan-r%d", planRound),
			Title:    fmt.Sprintf("campaign planner %s r%d", campaignID, planRound),
			RunAt:    now,
			Prompt:   prompt,
			Role:     role,
		})
	}

	if planStatus == PlanStatusPlanReviewPending {
		if _, ok := latestPlanReviewForRound(repo.PlanReviews, planRound); !ok {
			role := resolvePlannerReviewerRole(repo)
			prompt, err := buildPlannerReviewerDispatchPrompt(repo, role)
			if err != nil {
				return nil, err
			}
			specs = append(specs, DispatchTaskSpec{
				StateKey: fmt.Sprintf("campaign_dispatch:%s:planner_reviewer:r%d", campaignID, planRound),
				Kind:     DispatchKindPlannerReviewer,
				TaskID:   fmt.Sprintf("plan-review-r%d", planRound),
				Title:    fmt.Sprintf("campaign planner reviewer %s r%d", campaignID, planRound),
				RunAt:    now,
				Prompt:   prompt,
				Role:     role,
			})
		}
	}

	for _, task := range repo.Tasks {
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		if taskID == "" {
			continue
		}
		switch normalizeTaskStatus(task.Frontmatter.Status) {
		case TaskStatusExecuting:
			if task.Frontmatter.ExecutionRound <= 0 {
				continue
			}
			role := resolveExecutorRole(repo, task)
			prompt, err := buildExecutorDispatchPrompt(repo, task, role)
			if err != nil {
				return nil, err
			}
			specs = append(specs, DispatchTaskSpec{
				StateKey: executionDispatchStateKey(repo, task),
				Kind:     DispatchKindExecutor,
				TaskID:   taskID,
				Title:    fmt.Sprintf("campaign executor %s %s x%d", blankForKey(repo.Campaign.Frontmatter.CampaignID), blankForKey(taskID), task.Frontmatter.ExecutionRound),
				TaskPath: filepath.ToSlash(task.Dir),
				RunAt:    now,
				Prompt:   prompt,
				Role:     role,
			})
		case TaskStatusReviewing:
			if task.Frontmatter.ReviewRound <= 0 {
				continue
			}
			role := resolveReviewerRole(repo, task)
			prompt, err := buildReviewerDispatchPrompt(repo, task, role)
			if err != nil {
				return nil, err
			}
			specs = append(specs, DispatchTaskSpec{
				StateKey: reviewDispatchStateKey(repo, task),
				Kind:     DispatchKindReviewer,
				TaskID:   taskID,
				Title:    fmt.Sprintf("campaign reviewer %s %s r%d", blankForKey(repo.Campaign.Frontmatter.CampaignID), blankForKey(taskID), task.Frontmatter.ReviewRound),
				TaskPath: filepath.ToSlash(task.Dir),
				RunAt:    now,
				Prompt:   prompt,
				Role:     role,
			})
		}
	}
	return specs, nil
}

func buildExecutorDispatchPrompt(repo Repository, task TaskDocument, role RoleConfig) (string, error) {
	return renderCampaignPrompt(campaignRepoPromptExecutorDispatch, map[string]any{
		"CampaignRepo":    repo.Root,
		"CampaignFile":    repo.Campaign.Path,
		"TaskFile":        filepath.ToSlash(task.Path),
		"TaskDir":         filepath.ToSlash(task.Dir),
		"TaskID":          task.Frontmatter.TaskID,
		"TaskTitle":       task.Frontmatter.Title,
		"ExecutorRole":    roleLabel(role),
		"ExecutionRound":  task.Frontmatter.ExecutionRound,
		"TargetRepos":     task.Frontmatter.TargetRepos,
		"WorkingBranches": task.Frontmatter.WorkingBranches,
		"WriteScope":      task.Frontmatter.WriteScope,
		"ReviewerRole":    roleLabel(resolveReviewerRole(repo, task)),
		"ReportSnippet":   blankForSummary(task.Frontmatter.ReportSnippetPath),
	})
}

func buildReviewerDispatchPrompt(repo Repository, task TaskDocument, role RoleConfig) (string, error) {
	reviewPath := reviewDocumentPath(task)
	return renderCampaignPrompt(campaignRepoPromptReviewerDispatch, map[string]any{
		"CampaignRepo":        repo.Root,
		"CampaignFile":        repo.Campaign.Path,
		"TaskFile":            filepath.ToSlash(task.Path),
		"TaskDir":             filepath.ToSlash(task.Dir),
		"TaskID":              task.Frontmatter.TaskID,
		"TaskTitle":           task.Frontmatter.Title,
		"ReviewerRole":        roleLabel(role),
		"ReviewRound":         task.Frontmatter.ReviewRound,
		"TargetCommit":        blankForSummary(task.Frontmatter.HeadCommit),
		"LastRunPath":         blankForSummary(task.Frontmatter.LastRunPath),
		"SuggestedReviewFile": filepath.Join(repo.Root, filepath.FromSlash(reviewPath)),
	})
}

func executionDispatchStateKey(repo Repository, task TaskDocument) string {
	return fmt.Sprintf(
		"campaign_dispatch:%s:executor:%s:x%d",
		blankForKey(repo.Campaign.Frontmatter.CampaignID),
		blankForKey(task.Frontmatter.TaskID),
		task.Frontmatter.ExecutionRound,
	)
}

func reviewDispatchStateKey(repo Repository, task TaskDocument) string {
	return fmt.Sprintf(
		"campaign_dispatch:%s:reviewer:%s:r%d",
		blankForKey(repo.Campaign.Frontmatter.CampaignID),
		blankForKey(task.Frontmatter.TaskID),
		task.Frontmatter.ReviewRound,
	)
}

func reviewDocumentPath(task TaskDocument) string {
	reviewsDir := strings.TrimSpace(task.ReviewsDir)
	if reviewsDir == "" {
		reviewsDir = filepath.ToSlash(filepath.Join(task.Dir, "reviews"))
	}
	return filepath.ToSlash(filepath.Join(reviewsDir, fmt.Sprintf("R%03d.md", maxInt(task.Frontmatter.ReviewRound, 1))))
}

func buildPlannerDispatchPrompt(repo Repository, role RoleConfig) (string, error) {
	prevProposal, prevReview := previousProposalAndReview(repo)
	proposalOutputPath := filepath.Join(repo.Root, "plans", "proposals", fmt.Sprintf("round-%03d-plan.md", maxInt(repo.Campaign.Frontmatter.PlanRound, 1)))
	masterPlanPath := filepath.Join(repo.Root, "plans", "merged", "master-plan.md")
	findingsPath := filepath.Join(repo.Root, "findings.md")
	return renderCampaignPrompt(campaignRepoPromptPlannerDispatch, map[string]any{
		"CampaignRepo":         repo.Root,
		"CampaignFile":         filepath.Join(repo.Root, filepath.FromSlash(repo.Campaign.Path)),
		"Objective":            repo.Campaign.Frontmatter.Objective,
		"SourceRepos":          repo.Campaign.Frontmatter.SourceRepos,
		"PlanRound":            repo.Campaign.Frontmatter.PlanRound,
		"PlannerRole":          roleLabel(role),
		"PlannerReviewerRole":  roleLabel(resolvePlannerReviewerRole(repo)),
		"PreviousProposalPath": prevProposal,
		"PreviousReviewPath":   prevReview,
		"ProposalOutputPath":   proposalOutputPath,
		"MasterPlanPath":       masterPlanPath,
		"FindingsPath":         findingsPath,
	})
}

func buildPlannerReviewerDispatchPrompt(repo Repository, role RoleConfig) (string, error) {
	reviewOutputPath := filepath.Join(repo.Root, "plans", "reviews", fmt.Sprintf("round-%03d-review.md", maxInt(repo.Campaign.Frontmatter.PlanRound, 1)))
	masterPlanPath := filepath.Join(repo.Root, "plans", "merged", "master-plan.md")
	return renderCampaignPrompt(campaignRepoPromptPlannerReviewerDispatch, map[string]any{
		"CampaignRepo":     repo.Root,
		"CampaignFile":     filepath.Join(repo.Root, filepath.FromSlash(repo.Campaign.Path)),
		"Objective":        repo.Campaign.Frontmatter.Objective,
		"SourceRepos":      repo.Campaign.Frontmatter.SourceRepos,
		"PlanRound":        repo.Campaign.Frontmatter.PlanRound,
		"ProposalPath":     currentProposalPath(repo),
		"MasterPlanPath":   masterPlanPath,
		"ReviewerRole":     roleLabel(role),
		"ReviewOutputPath": reviewOutputPath,
	})
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
