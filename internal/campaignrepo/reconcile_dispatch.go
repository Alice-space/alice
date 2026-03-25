package campaignrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	campaignRepoPromptExecutorDispatch = "campaignrepo/executor_dispatch.md.tmpl"
	campaignRepoPromptReviewerDispatch = "campaignrepo/reviewer_dispatch.md.tmpl"
)

type DispatchKind string

const (
	DispatchKindExecutor DispatchKind = "executor"
	DispatchKindReviewer DispatchKind = "reviewer"
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
				TaskPath: filepath.ToSlash(task.Path),
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
				TaskPath: filepath.ToSlash(task.Path),
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
	return filepath.ToSlash(filepath.Join("reviews", task.Frontmatter.TaskID, fmt.Sprintf("R%03d.md", maxInt(task.Frontmatter.ReviewRound, 1))))
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
