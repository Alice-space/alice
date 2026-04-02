package campaignrepo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	TaskHumanGuidanceActionAccept = "accept"
	TaskHumanGuidanceActionResume = "resume"
)

type taskHumanGuidanceFrontmatter struct {
	GuidanceID            string `yaml:"guidance_id"`
	TargetTask            string `yaml:"target_task"`
	Action                string `yaml:"action"`
	Status                string `yaml:"status"`
	CreatedAtRaw          string `yaml:"created_at,omitempty"`
	Summary               string `yaml:"summary,omitempty"`
	PreviousBlockedReason string `yaml:"previous_blocked_reason,omitempty"`
}

var humanGuidanceFilePattern = regexp.MustCompile(`^G(\d{3,})\.md$`)

func ApplyTaskHumanGuidance(root, taskID, action, guidance string, now time.Time) (TaskDocument, error) {
	root = strings.TrimSpace(root)
	taskID = strings.TrimSpace(taskID)
	action = normalizeTaskHumanGuidanceAction(action)
	guidance = strings.TrimSpace(guidance)
	if root == "" {
		return TaskDocument{}, errors.New("campaign repo root is required")
	}
	if taskID == "" {
		return TaskDocument{}, errors.New("task id is required")
	}
	if guidance == "" {
		return TaskDocument{}, errors.New("guidance text is required")
	}
	switch action {
	case TaskHumanGuidanceActionAccept, TaskHumanGuidanceActionResume:
	default:
		return TaskDocument{}, fmt.Errorf("unsupported task guidance action %q", action)
	}
	if now.IsZero() {
		now = time.Now().Local()
	}

	repo, err := Load(root)
	if err != nil {
		return TaskDocument{}, err
	}
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		if strings.TrimSpace(task.Frontmatter.TaskID) != taskID {
			continue
		}
		status := normalizeTaskStatus(task.Frontmatter.Status)
		switch status {
		case TaskStatusDone, TaskStatusRejected:
			return TaskDocument{}, fmt.Errorf("task %s is terminal with status %s", taskID, status)
		}
		blockedReason := strings.TrimSpace(task.Frontmatter.LastBlockedReason)
		guidancePath, round, err := writeTaskHumanGuidanceDocument(root, *task, action, guidance, blockedReason, now)
		if err != nil {
			return TaskDocument{}, err
		}
		clearTaskAssignment(task)
		task.Frontmatter.HumanGuidanceRound = round
		task.Frontmatter.HumanGuidanceStatus = action
		task.Frontmatter.LastHumanGuidancePath = guidancePath
		task.Frontmatter.LastHumanGuidanceSummary = guidance
		task.Frontmatter.LastBlockedReason = ""
		switch action {
		case TaskHumanGuidanceActionAccept:
			task.Frontmatter.Status = TaskStatusAccepted
			task.Frontmatter.ReviewStatus = "approved"
			task.Frontmatter.DispatchState = dispatchStateHumanGuidanceApplied
		case TaskHumanGuidanceActionResume:
			task.Frontmatter.Status = TaskStatusRework
			task.Frontmatter.ReviewStatus = "changes_requested"
			task.Frontmatter.DispatchState = dispatchStateHumanGuidanceRequested
		}
		if err := persistTaskDocument(&repo, idx); err != nil {
			return TaskDocument{}, err
		}
		return repo.Tasks[idx], nil
	}
	return TaskDocument{}, fmt.Errorf("task %s not found", taskID)
}

func normalizeTaskHumanGuidanceAction(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func writeTaskHumanGuidanceDocument(root string, task TaskDocument, action, guidance, blockedReason string, now time.Time) (string, int, error) {
	guidanceDir := filepath.ToSlash(filepath.Join(task.Dir, "guidance"))
	round, err := nextTaskHumanGuidanceRound(filepath.Join(root, filepath.FromSlash(guidanceDir)), task.Frontmatter.HumanGuidanceRound)
	if err != nil {
		return "", 0, err
	}
	guidanceID := fmt.Sprintf("G%03d", round)
	relPath := filepath.ToSlash(filepath.Join(guidanceDir, guidanceID+".md"))
	doc := taskHumanGuidanceFrontmatter{
		GuidanceID:            guidanceID,
		TargetTask:            strings.TrimSpace(task.Frontmatter.TaskID),
		Action:                action,
		Status:                "applied",
		CreatedAtRaw:          now.Format(time.RFC3339),
		Summary:               guidance,
		PreviousBlockedReason: blockedReason,
	}
	frontmatter, err := yaml.Marshal(doc)
	if err != nil {
		return "", 0, err
	}
	var body strings.Builder
	body.WriteString("# Human Guidance\n\n")
	body.WriteString("## Decision\n\n")
	body.WriteString(guidance)
	body.WriteString("\n")
	if blockedReason != "" {
		body.WriteString("\n## Previous Blocker\n\n")
		body.WriteString(blockedReason)
		body.WriteString("\n")
	}
	rendered := "---\n" + strings.TrimRight(string(frontmatter), "\n") + "\n---\n\n" + strings.TrimSpace(body.String()) + "\n"
	if err := writeFileIfChanged(filepath.Join(root, filepath.FromSlash(relPath)), []byte(rendered)); err != nil {
		return "", 0, err
	}
	return relPath, round, nil
}

func nextTaskHumanGuidanceRound(dir string, current int) (int, error) {
	maxRound := current
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return maxRound + 1, nil
		}
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := humanGuidanceFilePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}
		var round int
		if _, scanErr := fmt.Sscanf(matches[1], "%d", &round); scanErr == nil && round > maxRound {
			maxRound = round
		}
	}
	return maxRound + 1, nil
}
