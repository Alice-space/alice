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
		base := filepath.Base(path)
		if strings.EqualFold(base, "README.md") {
			return nil
		}
		rel := filepath.ToSlash(relativePath(root, path))
		if !strings.Contains(rel, "/tasks/") {
			return nil
		}
		if base != "task.md" && !isLegacyTaskFilePath(rel) {
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
		taskDirRel := relativeTaskDir(root, path, frontmatter)
		contextPath := filepath.ToSlash(filepath.Join(taskDirRel, "context.md"))
		contextBody, err := loadMarkdownBodyIfExists(filepath.Join(root, filepath.FromSlash(contextPath)))
		if err != nil {
			return err
		}
		planPath := filepath.ToSlash(filepath.Join(taskDirRel, "plan.md"))
		planBody, err := loadMarkdownBodyIfExists(filepath.Join(root, filepath.FromSlash(planPath)))
		if err != nil {
			return err
		}
		progressPath := filepath.ToSlash(filepath.Join(taskDirRel, "progress.md"))
		progressBody, err := loadMarkdownBodyIfExists(filepath.Join(root, filepath.FromSlash(progressPath)))
		if err != nil {
			return err
		}
		taskPath := filepath.ToSlash(relativePath(root, path))
		legacyPath := ""
		if isLegacyTaskFilePath(taskPath) {
			legacyPath = taskPath
			taskPath = filepath.ToSlash(filepath.Join(taskDirRel, "task.md"))
		}
		tasks = append(tasks, TaskDocument{
			Path:         taskPath,
			Dir:          taskDirRel,
			Body:         parsed.Body,
			ContextPath:  contextPath,
			ContextBody:  contextBody,
			PlanPath:     planPath,
			PlanBody:     planBody,
			ProgressPath: progressPath,
			ProgressBody: progressBody,
			ResultsDir:   filepath.ToSlash(filepath.Join(taskDirRel, "results")),
			ReviewsDir:   filepath.ToSlash(filepath.Join(taskDirRel, "reviews")),
			LegacyPath:   legacyPath,
			Frontmatter:  frontmatter,
			LeaseUntil:   leaseUntil,
			WakeAt:       wakeAt,
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
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d == nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		rel := filepath.ToSlash(relativePath(root, path))
		if strings.EqualFold(filepath.Base(path), "README.md") {
			return nil
		}
		if !isReviewFilePath(rel) {
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
			Dir:         relativePath(root, filepath.Dir(path)),
			TaskDir:     taskDirForReviewPath(rel),
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

func loadSourceRepoDocuments(root string) ([]SourceRepoDocument, error) {
	var docs []SourceRepoDocument
	err := filepath.WalkDir(filepath.Join(root, "repos"), func(path string, d os.DirEntry, walkErr error) error {
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
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed := parseMarkdownFrontmatter(string(raw))
		if !parsed.Found {
			return nil
		}
		var frontmatter SourceRepoFrontmatter
		if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
			return fmt.Errorf("parse source repo frontmatter %s: %w", path, err)
		}
		frontmatter.RepoID = strings.TrimSpace(frontmatter.RepoID)
		frontmatter.RemoteURL = strings.TrimSpace(frontmatter.RemoteURL)
		frontmatter.LocalPath = strings.TrimSpace(frontmatter.LocalPath)
		frontmatter.DefaultBranch = strings.TrimSpace(frontmatter.DefaultBranch)
		frontmatter.ActiveBranches = normalizeStringList(frontmatter.ActiveBranches)
		frontmatter.BaseCommit = strings.TrimSpace(frontmatter.BaseCommit)
		frontmatter.Role = strings.TrimSpace(frontmatter.Role)
		if frontmatter.RepoID == "" {
			return nil
		}
		docs = append(docs, SourceRepoDocument{
			Path:        relativePath(root, path),
			Body:        parsed.Body,
			Frontmatter: frontmatter,
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Frontmatter.RepoID != docs[j].Frontmatter.RepoID {
			return docs[i].Frontmatter.RepoID < docs[j].Frontmatter.RepoID
		}
		return docs[i].Path < docs[j].Path
	})
	return docs, nil
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

func loadMarkdownBodyIfExists(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
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
			return parsed, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func loadPlanProposalDocuments(root string) ([]PlanProposalDocument, error) {
	var proposals []PlanProposalDocument
	err := filepath.WalkDir(filepath.Join(root, "plans", "proposals"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d == nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		if strings.ToLower(filepath.Base(path)) == "readme.md" {
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
		var frontmatter PlanProposalFrontmatter
		if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
			return fmt.Errorf("parse plan proposal frontmatter %s: %w", path, err)
		}
		frontmatter.ProposalID = strings.TrimSpace(frontmatter.ProposalID)
		frontmatter.Status = strings.ToLower(strings.TrimSpace(frontmatter.Status))
		if frontmatter.PlanRound < 0 {
			frontmatter.PlanRound = 0
		}
		proposals = append(proposals, PlanProposalDocument{
			Path:        relativePath(root, path),
			Body:        parsed.Body,
			Frontmatter: frontmatter,
		})
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(proposals, func(i, j int) bool {
		if proposals[i].Frontmatter.PlanRound != proposals[j].Frontmatter.PlanRound {
			return proposals[i].Frontmatter.PlanRound < proposals[j].Frontmatter.PlanRound
		}
		return proposals[i].Path < proposals[j].Path
	})
	return proposals, nil
}

func loadPlanReviewDocuments(root string) ([]PlanReviewDocument, error) {
	var reviews []PlanReviewDocument
	err := filepath.WalkDir(filepath.Join(root, "plans", "reviews"), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d == nil || d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		if strings.ToLower(filepath.Base(path)) == "readme.md" {
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
		var frontmatter PlanReviewFrontmatter
		if err := yaml.Unmarshal([]byte(parsed.Frontmatter), &frontmatter); err != nil {
			return fmt.Errorf("parse plan review frontmatter %s: %w", path, err)
		}
		frontmatter.ReviewID = strings.TrimSpace(frontmatter.ReviewID)
		frontmatter.Reviewer = normalizeRoleConfig(frontmatter.Reviewer)
		frontmatter.Verdict = normalizeReviewVerdict(frontmatter.Verdict, frontmatter.Blocking)
		frontmatter.CreatedAtRaw = strings.TrimSpace(frontmatter.CreatedAtRaw)
		if frontmatter.PlanRound < 0 {
			frontmatter.PlanRound = 0
		}
		createdAt, err := parseFlexibleTime(frontmatter.CreatedAtRaw)
		if err != nil {
			return fmt.Errorf("parse plan review created_at %s: %w", path, err)
		}
		if createdAt.IsZero() {
			if info, statErr := os.Stat(path); statErr == nil {
				createdAt = info.ModTime().Local()
			}
		}
		reviews = append(reviews, PlanReviewDocument{
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
		if reviews[i].Frontmatter.PlanRound != reviews[j].Frontmatter.PlanRound {
			return reviews[i].Frontmatter.PlanRound < reviews[j].Frontmatter.PlanRound
		}
		if !reviews[i].CreatedAt.Equal(reviews[j].CreatedAt) {
			return reviews[i].CreatedAt.Before(reviews[j].CreatedAt)
		}
		return reviews[i].Path < reviews[j].Path
	})
	return reviews, nil
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func relativeTaskDir(root, taskPath string, frontmatter TaskFrontmatter) string {
	rel := filepath.ToSlash(relativePath(root, filepath.Dir(taskPath)))
	if filepath.Base(taskPath) == "task.md" {
		return rel
	}
	return canonicalTaskDir(frontmatter.Phase, frontmatter.TaskID, rel)
}

func canonicalTaskDir(phase, taskID, fallback string) string {
	phase = strings.TrimSpace(phase)
	taskID = strings.TrimSpace(taskID)
	if phase != "" && taskID != "" {
		return filepath.ToSlash(filepath.Join("phases", phase, "tasks", taskID))
	}
	return filepath.ToSlash(fallback)
}

func isLegacyTaskFilePath(path string) bool {
	if !strings.Contains(path, "/tasks/") {
		return false
	}
	if !strings.HasSuffix(filepath.ToSlash(filepath.Dir(path)), "/tasks") {
		return false
	}
	if strings.EqualFold(filepath.Base(path), "task.md") {
		return false
	}
	if strings.EqualFold(filepath.Base(path), "README.md") {
		return false
	}
	return strings.EqualFold(filepath.Ext(path), ".md")
}

func isReviewFilePath(path string) bool {
	switch {
	case strings.HasPrefix(path, "reviews/"):
		return true
	case strings.Contains(path, "/tasks/") && strings.Contains(path, "/reviews/"):
		return true
	default:
		return false
	}
}

func taskDirForReviewPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	if strings.Contains(path, "/tasks/") {
		dir := filepath.ToSlash(filepath.Dir(path))
		return filepath.ToSlash(filepath.Dir(dir))
	}
	return ""
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
