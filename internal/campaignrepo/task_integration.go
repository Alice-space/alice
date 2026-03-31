package campaignrepo

import (
	"fmt"
	"strings"
	"time"
)

func integrateAcceptedTasks(repo *Repository, campaignID string) (int, []ReconcileEvent, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil, nil
	}

	sourceRepoByID := make(map[string]SourceRepoDocument, len(repo.SourceRepos))
	for _, repoDoc := range repo.SourceRepos {
		repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
		if repoID == "" {
			continue
		}
		sourceRepoByID[repoID] = repoDoc
	}

	changed := 0
	var events []ReconcileEvent
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusAccepted {
			continue
		}

		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		taskTitle := strings.TrimSpace(task.Frontmatter.Title)
		targetRepos := resolveTaskSourceRepos(*task, sourceRepoByID)

		switch {
		case !taskRequiresSourceRepoEvidence(*task), len(targetRepos) == 0:
			task.Frontmatter.Status = TaskStatusDone
			task.Frontmatter.DispatchState = "integration_not_required"
			task.Frontmatter.LastBlockedReason = ""
			task.Frontmatter.OwnerAgent = ""
			task.LeaseUntil = time.Time{}
			if err := persistTaskDocument(repo, idx); err != nil {
				return changed, events, err
			}
			events = append(events, ReconcileEvent{
				Kind:       EventTaskIntegrated,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "任务已完成",
				Detail:     fmt.Sprintf("任务 **%s** %s 已通过评审，无需 source-repo 集成，已标记为完成", taskID, taskTitle),
				Severity:   "success",
			})
			changed++
			continue
		}

		mergeCommit, err := integrateTaskIntoTargetRepos(*task, targetRepos)
		if err != nil {
			task.Frontmatter.Status = TaskStatusBlocked
			task.Frontmatter.DispatchState = "integration_blocked"
			task.Frontmatter.LastBlockedReason = err.Error()
			task.Frontmatter.OwnerAgent = ""
			task.LeaseUntil = time.Time{}
			task.WakeAt = time.Time{}
			task.Frontmatter.WakePrompt = ""
			if err := persistTaskDocument(repo, idx); err != nil {
				return changed, events, err
			}
			events = append(events, ReconcileEvent{
				Kind:       EventTaskBlocked,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "任务集成受阻",
				Detail:     fmt.Sprintf("任务 **%s** %s 已通过评审，但回主线集成失败。\n\n**原因**: %s", taskID, taskTitle, blankForSummary(task.Frontmatter.LastBlockedReason)),
				Severity:   "warning",
			})
			changed++
			continue
		}

		if mergeCommit != "" {
			task.Frontmatter.HeadCommit = mergeCommit
		}
		task.Frontmatter.Status = TaskStatusDone
		task.Frontmatter.DispatchState = "integrated"
		task.Frontmatter.LastBlockedReason = ""
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if err := persistTaskDocument(repo, idx); err != nil {
			return changed, events, err
		}
		events = append(events, ReconcileEvent{
			Kind:       EventTaskIntegrated,
			CampaignID: campaignID,
			TaskID:     taskID,
			Title:      "任务已集成完成",
			Detail:     fmt.Sprintf("任务 **%s** %s 已通过评审并合并回目标主线，状态更新为 done", taskID, taskTitle),
			Severity:   "success",
		})
		changed++
	}
	return changed, events, nil
}

func integrateTaskIntoTargetRepos(task TaskDocument, repos []SourceRepoDocument) (string, error) {
	if len(repos) == 0 {
		return "", nil
	}
	if issues := taskExecutionWorkspaceIssues(task, repos); len(issues) > 0 {
		return "", fmt.Errorf("%s", issues[0].Message)
	}

	singleTarget := len(repos) == 1
	mergedHead := ""
	for _, repoDoc := range repos {
		commit, err := integrateTaskIntoTargetRepo(task, repoDoc)
		if err != nil {
			return "", err
		}
		if singleTarget {
			mergedHead = commit
		}
	}
	return mergedHead, nil
}

func integrateTaskIntoTargetRepo(task TaskDocument, repoDoc SourceRepoDocument) (string, error) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
	localPath := strings.TrimSpace(repoDoc.Frontmatter.LocalPath)
	defaultBranch := strings.TrimSpace(repoDoc.Frontmatter.DefaultBranch)
	if repoID == "" || localPath == "" {
		return "", fmt.Errorf("task %s integration missing repo_id/local_path", taskID)
	}
	if defaultBranch == "" {
		return "", fmt.Errorf("task %s repo %s is missing default_branch for integration", taskID, repoID)
	}
	if !gitWorktreeExists(localPath) {
		return "", fmt.Errorf("task %s repo %s local_path is not a git worktree: %s", taskID, repoID, localPath)
	}

	currentBranch, err := gitCurrentBranch(localPath)
	if err != nil {
		return "", err
	}
	if currentBranch != defaultBranch {
		return "", fmt.Errorf("task %s repo %s local_path must stay on default branch %s for integration, got %s", taskID, repoID, defaultBranch, blankForSummary(currentBranch))
	}
	clean, err := gitWorktreeIsClean(localPath)
	if err != nil {
		return "", err
	}
	if !clean {
		return "", fmt.Errorf("task %s repo %s local_path has uncommitted changes; cannot merge task branch safely", taskID, repoID)
	}

	branchName, ok := taskBranchForRepo(task.Frontmatter.WorkingBranches, repoID)
	if !ok || branchName == "" {
		return "", fmt.Errorf("task %s repo %s is missing a working_branch for integration", taskID, repoID)
	}
	if branchName == defaultBranch {
		return "", fmt.Errorf("task %s repo %s still points at default branch %s; isolated task branch is required before integration", taskID, repoID, defaultBranch)
	}
	if !gitLocalBranchExists(localPath, branchName) {
		return "", fmt.Errorf("task %s repo %s working_branch %s does not exist locally for integration", taskID, repoID, branchName)
	}
	if headCommit := strings.TrimSpace(task.Frontmatter.HeadCommit); headCommit != "" && !gitBranchContainsCommit(localPath, branchName, headCommit) {
		return "", fmt.Errorf("task %s repo %s working_branch %s does not contain reviewed head_commit %s", taskID, repoID, branchName, headCommit)
	}

	if err := gitMergeBranchIntoCurrent(localPath, branchName); err != nil {
		return "", fmt.Errorf("task %s repo %s merge %s -> %s failed: %w", taskID, repoID, branchName, defaultBranch, err)
	}
	return gitResolveCommit(localPath, "HEAD")
}

func gitWorktreeIsClean(path string) (bool, error) {
	output, err := runGit(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "", nil
}

func gitMergeBranchIntoCurrent(path, branch string) error {
	_, err := runGit(
		path,
		"-c", "user.name=Alice CodeArmy",
		"-c", "user.email=alice-codearmy@local",
		"merge",
		"--no-ff",
		"--no-edit",
		branch,
	)
	if err == nil {
		return nil
	}
	if _, abortErr := runGit(path, "merge", "--abort"); abortErr != nil && !strings.Contains(strings.ToLower(abortErr.Error()), "merge_head") {
		return fmt.Errorf("%w; additionally failed to abort merge: %v", err, abortErr)
	}
	return err
}
