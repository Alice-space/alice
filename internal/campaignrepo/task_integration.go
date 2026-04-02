package campaignrepo

import (
	"fmt"
	"strings"
	"time"
)

const maxIntegrationConflictRetries = 3

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
		targetRepos := integrationTargetRepos(*task, resolveTaskSourceRepos(*task, sourceRepoByID))

		switch {
		case !taskRequiresSourceRepoEvidence(*task), len(targetRepos) == 0:
			task.Frontmatter.Status = TaskStatusDone
			task.Frontmatter.DispatchState = "integration_not_required"
			task.Frontmatter.IntegrationRetryCount = 0
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
			if integrationFailureLooksLikeMergeConflict(err.Error()) {
				task.Frontmatter.LastBlockedReason = err.Error()
			}
			if integrationFailureLooksLikeMergeConflict(err.Error()) && queueIntegrationConflictRecovery(task) {
				if err := persistTaskDocument(repo, idx); err != nil {
					return changed, events, err
				}
				events = append(events, ReconcileEvent{
					Kind:       EventTaskRetrying,
					CampaignID: campaignID,
					TaskID:     taskID,
					Title:      "任务集成冲突，回流执行修复",
					Detail:     fmt.Sprintf("任务 **%s** %s 在回主线集成时发生 merge conflict，已回流给 executor 做第 %d 次集成冲突修复。", taskID, taskTitle, task.Frontmatter.IntegrationRetryCount),
					Severity:   "warning",
				})
				changed++
				continue
			}
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
		task.Frontmatter.IntegrationRetryCount = 0
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

func queueIntegrationConflictRecovery(task *TaskDocument) bool {
	if task == nil {
		return false
	}
	if task.Frontmatter.IntegrationRetryCount >= maxIntegrationConflictRetries {
		return false
	}
	task.Frontmatter.IntegrationRetryCount++
	task.Frontmatter.Status = TaskStatusRework
	task.Frontmatter.DispatchState = "integration_conflict_requested"
	task.Frontmatter.ReviewStatus = "changes_requested"
	task.Frontmatter.OwnerAgent = ""
	task.LeaseUntil = time.Time{}
	task.WakeAt = time.Time{}
	task.Frontmatter.WakePrompt = ""
	return true
}

func integrationFailureLooksLikeMergeConflict(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	return strings.Contains(reason, "automatic merge failed") ||
		strings.Contains(reason, "conflict (content)") ||
		strings.Contains(reason, "merge conflict in")
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

func integrationTargetRepos(task TaskDocument, repos []SourceRepoDocument) []SourceRepoDocument {
	if len(repos) <= 1 {
		return repos
	}

	// Multi-repo tasks may list read-context repos in target_repos. Only repos
	// with an explicit non-campaign write_scope should participate in merge-back.
	writableRepoIDs := make(map[string]struct{}, len(repos))
	for _, rawScope := range task.Frontmatter.WriteScope {
		repoID, scope, ok := splitScopePrefix(rawScope)
		if !ok || strings.EqualFold(repoID, "campaign") || strings.TrimSpace(scope) == "" {
			continue
		}
		writableRepoIDs[strings.ToLower(strings.TrimSpace(repoID))] = struct{}{}
	}
	if len(writableRepoIDs) == 0 {
		return repos
	}

	filtered := make([]SourceRepoDocument, 0, len(repos))
	for _, repoDoc := range repos {
		repoID := strings.ToLower(strings.TrimSpace(repoDoc.Frontmatter.RepoID))
		if _, ok := writableRepoIDs[repoID]; ok {
			filtered = append(filtered, repoDoc)
		}
	}
	if len(filtered) == 0 {
		return repos
	}
	return filtered
}

func integrateTaskIntoTargetRepo(task TaskDocument, repoDoc SourceRepoDocument) (string, error) {
	branchName, err := validateTaskIntegrationTarget(task, repoDoc)
	if err != nil {
		return "", err
	}
	repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
	defaultBranch := strings.TrimSpace(repoDoc.Frontmatter.DefaultBranch)
	localPath := strings.TrimSpace(repoDoc.Frontmatter.LocalPath)

	if err := gitMergeBranchIntoCurrent(localPath, branchName); err != nil {
		return "", fmt.Errorf("task %s repo %s merge %s -> %s failed: %w", strings.TrimSpace(task.Frontmatter.TaskID), repoID, branchName, defaultBranch, err)
	}
	return gitResolveCommit(localPath, "HEAD")
}

func validateTaskIntegrationTarget(task TaskDocument, repoDoc SourceRepoDocument) (string, error) {
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
	return branchName, nil
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
