package campaignrepo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/prompting"
	"go.yaml.in/yaml/v3"
)

func taskIndexesByPath(tasks []TaskDocument) map[string]int {
	index := make(map[string]int, len(tasks))
	for i, task := range tasks {
		index[filepath.ToSlash(task.Path)] = i
	}
	return index
}

func reviewsByTask(reviews []ReviewDocument) map[string][]ReviewDocument {
	index := make(map[string][]ReviewDocument)
	for _, review := range reviews {
		taskID := strings.TrimSpace(review.Frontmatter.TargetTask)
		if taskID == "" {
			continue
		}
		index[taskID] = append(index[taskID], review)
	}
	return index
}

func latestRelevantReview(task TaskDocument, reviews []ReviewDocument) (ReviewDocument, bool) {
	if len(reviews) == 0 {
		return ReviewDocument{}, false
	}
	targetRound := task.Frontmatter.ReviewRound
	var chosen ReviewDocument
	found := false
	for _, review := range reviews {
		if !reviewMatchesTaskReviewer(task, review) {
			continue
		}
		if targetRound > 0 && review.Frontmatter.ReviewRound > 0 && review.Frontmatter.ReviewRound != targetRound {
			continue
		}
		if !found || compareReviewDocs(chosen, review) < 0 {
			chosen = review
			found = true
		}
	}
	return chosen, found
}

func latestTaskReview(reviews []ReviewDocument) (ReviewDocument, bool) {
	if len(reviews) == 0 {
		return ReviewDocument{}, false
	}
	chosen := reviews[0]
	for _, review := range reviews[1:] {
		if compareReviewDocs(chosen, review) < 0 {
			chosen = review
		}
	}
	return chosen, true
}

func reviewMatchesTaskReviewer(task TaskDocument, review ReviewDocument) bool {
	expected := normalizeReviewActorRole(task.Frontmatter.Reviewer.Role)
	actual := normalizeReviewActorRole(review.Frontmatter.Reviewer.Role)
	switch {
	case actual == "":
		return true
	case expected == "":
		return roleLooksLikeReviewer(actual)
	case expected == actual:
		return true
	default:
		return roleLooksLikeReviewer(expected) && roleLooksLikeReviewer(actual)
	}
}

func normalizeReviewActorRole(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func roleLooksLikeReviewer(role string) bool {
	role = normalizeReviewActorRole(role)
	return strings.Contains(role, "reviewer")
}

func compareReviewDocs(left, right ReviewDocument) int {
	if left.Frontmatter.ReviewRound != right.Frontmatter.ReviewRound {
		if left.Frontmatter.ReviewRound < right.Frontmatter.ReviewRound {
			return -1
		}
		return 1
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		if left.CreatedAt.Before(right.CreatedAt) {
			return -1
		}
		return 1
	}
	return strings.Compare(left.Path, right.Path)
}

func persistTaskDocument(repo *Repository, index int) error {
	if repo == nil || index < 0 || index >= len(repo.Tasks) {
		return errors.New("task index out of range")
	}
	repo.Tasks[index] = normalizeTaskDocument(repo.Tasks[index])
	return writeTaskDocument(repo.Root, repo.Tasks[index])
}

func normalizeTaskDocument(task TaskDocument) TaskDocument {
	task.Frontmatter.Status = normalizeTaskStatus(task.Frontmatter.Status)
	task.Frontmatter.Executor = normalizeRoleConfig(task.Frontmatter.Executor)
	task.Frontmatter.Reviewer = normalizeRoleConfig(task.Frontmatter.Reviewer)
	task.Frontmatter.DispatchState = strings.ToLower(strings.TrimSpace(task.Frontmatter.DispatchState))
	task.Frontmatter.ReviewStatus = normalizeReviewStatus(task.Frontmatter.ReviewStatus)
	task.Frontmatter.OwnerAgent = strings.TrimSpace(task.Frontmatter.OwnerAgent)
	task.Frontmatter.BaseCommit = strings.TrimSpace(task.Frontmatter.BaseCommit)
	task.Frontmatter.HeadCommit = strings.TrimSpace(task.Frontmatter.HeadCommit)
	task.Frontmatter.LastRunPath = filepath.ToSlash(strings.TrimSpace(task.Frontmatter.LastRunPath))
	task.Frontmatter.LastReviewPath = filepath.ToSlash(strings.TrimSpace(task.Frontmatter.LastReviewPath))
	task.Frontmatter.WakePrompt = strings.TrimSpace(task.Frontmatter.WakePrompt)
	task.Frontmatter.ReportSnippetPath = strings.TrimSpace(task.Frontmatter.ReportSnippetPath)
	task.Path = filepath.ToSlash(strings.TrimSpace(task.Path))
	task.Dir = filepath.ToSlash(strings.TrimSpace(task.Dir))
	if task.Dir == "" {
		task.Dir = canonicalTaskDir(task.Frontmatter.Phase, task.Frontmatter.TaskID, filepath.Dir(task.Path))
	}
	if task.Path == "" || filepath.Base(task.Path) != "task.md" {
		if task.Path != "" && strings.Contains(task.Path, "/tasks/") && strings.EqualFold(filepath.Ext(task.Path), ".md") && !strings.EqualFold(filepath.Base(task.Path), "task.md") {
			task.LegacyPath = filepath.ToSlash(task.Path)
		}
		task.Path = filepath.ToSlash(filepath.Join(task.Dir, "task.md"))
	}
	task.ContextPath = filepath.ToSlash(strings.TrimSpace(task.ContextPath))
	task.PlanPath = filepath.ToSlash(strings.TrimSpace(task.PlanPath))
	task.ProgressPath = filepath.ToSlash(strings.TrimSpace(task.ProgressPath))
	task.ResultsDir = filepath.ToSlash(strings.TrimSpace(task.ResultsDir))
	task.ReviewsDir = filepath.ToSlash(strings.TrimSpace(task.ReviewsDir))
	if task.ContextPath == "" {
		task.ContextPath = filepath.ToSlash(filepath.Join(task.Dir, "context.md"))
	}
	if task.PlanPath == "" {
		task.PlanPath = filepath.ToSlash(filepath.Join(task.Dir, "plan.md"))
	}
	if task.ProgressPath == "" {
		task.ProgressPath = filepath.ToSlash(filepath.Join(task.Dir, "progress.md"))
	}
	if task.ResultsDir == "" {
		task.ResultsDir = filepath.ToSlash(filepath.Join(task.Dir, "results"))
	}
	if task.ReviewsDir == "" {
		task.ReviewsDir = filepath.ToSlash(filepath.Join(task.Dir, "reviews"))
	}
	task.Frontmatter.LeaseUntilRaw = formatOptionalTime(task.LeaseUntil)
	task.Frontmatter.WakeAtRaw = formatOptionalTime(task.WakeAt)
	return task
}

func writeTaskDocument(root string, task TaskDocument) error {
	task = normalizeTaskDocument(task)
	frontmatter, err := yaml.Marshal(task.Frontmatter)
	if err != nil {
		return err
	}
	content := strings.TrimRight(string(frontmatter), "\n")
	body := strings.TrimSpace(task.Body)
	rendered := "---\n" + content + "\n---\n"
	if body != "" {
		rendered += "\n" + body + "\n"
	}
	path := filepath.Join(root, filepath.FromSlash(task.Path))
	if err := writeFileIfChanged(path, []byte(rendered)); err != nil {
		return err
	}
	for _, dir := range []string{task.Dir, task.ResultsDir, task.ReviewsDir} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			return err
		}
	}
	if legacy := filepath.ToSlash(strings.TrimSpace(task.LegacyPath)); legacy != "" && legacy != filepath.ToSlash(task.Path) {
		legacyPath := filepath.Join(root, filepath.FromSlash(legacy))
		if _, err := os.Stat(legacyPath); err == nil {
			if removeErr := os.Remove(legacyPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return removeErr
			}
		}
	}
	return nil
}

func renderCampaignPrompt(name string, data any) (string, error) {
	loader := prompting.DefaultLoader()
	if loader == nil {
		return "", errors.New("prompt loader is nil")
	}
	return loader.RenderFile(name, data)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
