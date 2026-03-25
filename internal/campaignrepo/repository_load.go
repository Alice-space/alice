package campaignrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type markdownFrontmatterResult struct {
	Frontmatter string
	Body        string
	Found       bool
}

func loadCampaignDocument(path, root string) (CampaignDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return CampaignDocument{}, err
	}
	parsed := parseMarkdownFrontmatter(string(raw))
	var frontmatter CampaignFrontmatter
	if parsed.Found {
		if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
			return CampaignDocument{}, fmt.Errorf("parse campaign frontmatter %s: %w", path, err)
		}
	}
	frontmatter.CampaignID = strings.TrimSpace(frontmatter.CampaignID)
	frontmatter.Title = strings.TrimSpace(frontmatter.Title)
	frontmatter.Objective = strings.TrimSpace(frontmatter.Objective)
	frontmatter.Status = strings.TrimSpace(frontmatter.Status)
	frontmatter.CampaignRepoPath = strings.TrimSpace(frontmatter.CampaignRepoPath)
	if frontmatter.CampaignRepoPath == "" {
		frontmatter.CampaignRepoPath = root
	}
	frontmatter.CurrentPhase = strings.TrimSpace(frontmatter.CurrentPhase)
	frontmatter.CurrentDirection = strings.TrimSpace(frontmatter.CurrentDirection)
	frontmatter.CurrentWinnerTask = strings.TrimSpace(frontmatter.CurrentWinnerTask)
	frontmatter.SourceRepos = normalizeStringList(frontmatter.SourceRepos)
	frontmatter.ReviewMode = strings.TrimSpace(frontmatter.ReviewMode)
	frontmatter.ReportMode = strings.TrimSpace(frontmatter.ReportMode)
	frontmatter.DefaultExecutor = normalizeRoleConfig(frontmatter.DefaultExecutor)
	frontmatter.DefaultReviewer = normalizeRoleConfig(frontmatter.DefaultReviewer)
	return CampaignDocument{
		Path:        relativePath(root, path),
		Body:        parsed.Body,
		Frontmatter: frontmatter,
	}, nil
}

func loadPhaseDocuments(root string) ([]PhaseDocument, error) {
	matches, err := filepath.Glob(filepath.Join(root, "phases", "*", "phase.md"))
	if err != nil {
		return nil, err
	}
	phases := make([]PhaseDocument, 0, len(matches))
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		parsed := parseMarkdownFrontmatter(string(raw))
		var frontmatter PhaseFrontmatter
		if parsed.Found {
			if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
				return nil, fmt.Errorf("parse phase frontmatter %s: %w", path, err)
			}
		}
		frontmatter.Phase = strings.TrimSpace(frontmatter.Phase)
		frontmatter.Title = strings.TrimSpace(frontmatter.Title)
		frontmatter.Status = strings.TrimSpace(frontmatter.Status)
		frontmatter.Goal = strings.TrimSpace(frontmatter.Goal)
		frontmatter.EntryGates = normalizeStringList(frontmatter.EntryGates)
		frontmatter.ExitGates = normalizeStringList(frontmatter.ExitGates)
		phases = append(phases, PhaseDocument{
			Path:        relativePath(root, path),
			Body:        parsed.Body,
			Frontmatter: frontmatter,
		})
	}
	sort.Slice(phases, func(i, j int) bool { return phases[i].Path < phases[j].Path })
	return phases, nil
}

func loadTaskDocuments(root string) ([]TaskDocument, error) {
	var tasks []TaskDocument
	err := filepath.WalkDir(filepath.Join(root, "phases"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d == nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "README.md") {
			return nil
		}
		rel := filepath.ToSlash(relativePath(root, path))
		if !strings.Contains(rel, "/tasks/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed := parseMarkdownFrontmatter(string(raw))
		if !parsed.Found {
			return nil
		}
		var frontmatter TaskFrontmatter
		if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
			return fmt.Errorf("parse task frontmatter %s: %w", path, err)
		}
		frontmatter.TaskID = strings.TrimSpace(frontmatter.TaskID)
		if frontmatter.TaskID == "" {
			return nil
		}
		frontmatter.Title = strings.TrimSpace(frontmatter.Title)
		frontmatter.Phase = strings.TrimSpace(frontmatter.Phase)
		frontmatter.Status = normalizeTaskStatus(frontmatter.Status)
		frontmatter.DependsOn = normalizeStringList(frontmatter.DependsOn)
		frontmatter.TargetRepos = normalizeStringList(frontmatter.TargetRepos)
		frontmatter.WorkingBranches = normalizeStringList(frontmatter.WorkingBranches)
		frontmatter.WriteScope = normalizeStringList(frontmatter.WriteScope)
		frontmatter.OwnerAgent = strings.TrimSpace(frontmatter.OwnerAgent)
		frontmatter.LeaseUntilRaw = strings.TrimSpace(frontmatter.LeaseUntilRaw)
		frontmatter.Executor = normalizeRoleConfig(frontmatter.Executor)
		frontmatter.Reviewer = normalizeRoleConfig(frontmatter.Reviewer)
		frontmatter.DispatchState = strings.ToLower(strings.TrimSpace(frontmatter.DispatchState))
		frontmatter.ReviewStatus = normalizeReviewStatus(frontmatter.ReviewStatus)
		if frontmatter.ExecutionRound < 0 {
			frontmatter.ExecutionRound = 0
		}
		if frontmatter.ReviewRound < 0 {
			frontmatter.ReviewRound = 0
		}
		frontmatter.BaseCommit = strings.TrimSpace(frontmatter.BaseCommit)
		frontmatter.HeadCommit = strings.TrimSpace(frontmatter.HeadCommit)
		frontmatter.LastRunPath = filepath.ToSlash(strings.TrimSpace(frontmatter.LastRunPath))
		frontmatter.LastReviewPath = filepath.ToSlash(strings.TrimSpace(frontmatter.LastReviewPath))
		frontmatter.WakeAtRaw = strings.TrimSpace(frontmatter.WakeAtRaw)
		frontmatter.WakePrompt = strings.TrimSpace(frontmatter.WakePrompt)
		frontmatter.ReportSnippetPath = strings.TrimSpace(frontmatter.ReportSnippetPath)
		frontmatter.Artifacts = normalizeStringList(frontmatter.Artifacts)
		frontmatter.ResultPaths = normalizeStringList(frontmatter.ResultPaths)
		leaseUntil, err := parseFlexibleTime(frontmatter.LeaseUntilRaw)
		if err != nil {
			return fmt.Errorf("parse lease_until %s: %w", path, err)
		}
		wakeAt, err := parseFlexibleTime(frontmatter.WakeAtRaw)
		if err != nil {
			return fmt.Errorf("parse wake_at %s: %w", path, err)
		}
		tasks = append(tasks, TaskDocument{
			Path:        relativePath(root, path),
			Dir:         relativePath(root, filepath.Dir(path)),
			Body:        parsed.Body,
			Frontmatter: frontmatter,
			LeaseUntil:  leaseUntil,
			WakeAt:      wakeAt,
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Frontmatter.Phase != tasks[j].Frontmatter.Phase {
			return tasks[i].Frontmatter.Phase < tasks[j].Frontmatter.Phase
		}
		if tasks[i].Frontmatter.TaskID != tasks[j].Frontmatter.TaskID {
			return tasks[i].Frontmatter.TaskID < tasks[j].Frontmatter.TaskID
		}
		return tasks[i].Path < tasks[j].Path
	})
	return tasks, nil
}

func loadReviewDocuments(root string) ([]ReviewDocument, error) {
	var reviews []ReviewDocument
	err := filepath.WalkDir(filepath.Join(root, "reviews"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d == nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed := parseMarkdownFrontmatter(string(raw))
		if !parsed.Found {
			return nil
		}
		var frontmatter ReviewFrontmatter
		if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
			return fmt.Errorf("parse review frontmatter %s: %w", path, err)
		}
		frontmatter.ReviewID = strings.TrimSpace(frontmatter.ReviewID)
		frontmatter.TargetTask = strings.TrimSpace(frontmatter.TargetTask)
		if frontmatter.TargetTask == "" {
			return nil
		}
		if frontmatter.ReviewRound < 0 {
			frontmatter.ReviewRound = 0
		}
		frontmatter.Reviewer = normalizeRoleConfig(frontmatter.Reviewer)
		frontmatter.Verdict = normalizeReviewVerdict(frontmatter.Verdict, frontmatter.Blocking)
		frontmatter.TargetCommit = strings.TrimSpace(frontmatter.TargetCommit)
		frontmatter.CreatedAtRaw = strings.TrimSpace(frontmatter.CreatedAtRaw)
		createdAt, err := parseFlexibleTime(frontmatter.CreatedAtRaw)
		if err != nil {
			return fmt.Errorf("parse review created_at %s: %w", path, err)
		}
		if createdAt.IsZero() {
			if info, statErr := os.Stat(path); statErr == nil {
				createdAt = info.ModTime().Local()
			}
		}
		reviews = append(reviews, ReviewDocument{
			Path:        relativePath(root, path),
			Body:        parsed.Body,
			Frontmatter: frontmatter,
			CreatedAt:   createdAt,
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(reviews, func(i, j int) bool {
		left := reviews[i]
		right := reviews[j]
		if left.Frontmatter.TargetTask != right.Frontmatter.TargetTask {
			return left.Frontmatter.TargetTask < right.Frontmatter.TargetTask
		}
		if left.Frontmatter.ReviewRound != right.Frontmatter.ReviewRound {
			return left.Frontmatter.ReviewRound < right.Frontmatter.ReviewRound
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.Path < right.Path
	})
	return reviews, nil
}

func parseMarkdownFrontmatter(raw string) markdownFrontmatterResult {
	text := strings.TrimSpace(raw)
	if text == "" {
		return markdownFrontmatterResult{}
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return markdownFrontmatterResult{Body: text}
	}
	end := -1
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			end = idx
			break
		}
	}
	if end <= 0 {
		return markdownFrontmatterResult{Body: text}
	}
	return markdownFrontmatterResult{
		Frontmatter: strings.Join(lines[1:end], "\n"),
		Body:        strings.TrimSpace(strings.Join(lines[end+1:], "\n")),
		Found:       true,
	}
}

func parseFlexibleTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	var lastErr error
	for _, layout := range layouts {
		var (
			parsed time.Time
			err    error
		)
		if strings.Contains(layout, "Z07") {
			parsed, err = time.Parse(layout, raw)
		} else {
			parsed, err = time.ParseInLocation(layout, raw, time.Local)
		}
		if err == nil {
			return parsed.Local(), nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func writeFileIfChanged(path string, content []byte) error {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
