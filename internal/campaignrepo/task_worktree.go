package campaignrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const taskWorktreeRootDir = ".worktrees"

func ensureTaskExecutionWorkspaces(repo *Repository, task *TaskDocument) error {
	if repo == nil || task == nil || !taskRequiresSourceRepoEvidence(*task) {
		return nil
	}

	sourceRepoByID := make(map[string]SourceRepoDocument, len(repo.SourceRepos))
	for _, repoDoc := range repo.SourceRepos {
		repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
		if repoID == "" {
			continue
		}
		sourceRepoByID[repoID] = repoDoc
	}

	targetRepos := resolveTaskSourceRepos(*task, sourceRepoByID)
	if len(targetRepos) == 0 {
		return nil
	}

	workingBranches := append([]string(nil), task.Frontmatter.WorkingBranches...)
	worktreePaths := append([]string(nil), task.Frontmatter.WorktreePaths...)
	singleTarget := len(targetRepos) == 1

	for _, repoDoc := range targetRepos {
		repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
		localPath := strings.TrimSpace(repoDoc.Frontmatter.LocalPath)
		if repoID == "" || localPath == "" {
			continue
		}
		if info, err := os.Stat(localPath); err != nil || !info.IsDir() || !gitWorktreeExists(localPath) {
			continue
		}

		branchName, ok := taskBranchForRepo(workingBranches, repoID)
		if !ok {
			branchName = defaultTaskBranchName(repo.Campaign.Frontmatter.CampaignID, task.Frontmatter.TaskID, repoID)
			workingBranches = append(workingBranches, repoScopedValue(repoID, branchName))
		}

		worktreePath, ok := taskWorktreePathForRepo(worktreePaths, repoID, singleTarget)
		if !ok {
			worktreePath = defaultTaskWorktreePath(repo.Root, repoID, task.Frontmatter.TaskID)
			worktreePaths = append(worktreePaths, repoScopedValue(repoID, worktreePath))
		}

		baseRevision, resolvedBaseCommit, err := resolveTaskWorkspaceBase(repoDoc, *task)
		if err != nil {
			return fmt.Errorf("resolve worktree base for task %s repo %s: %w", task.Frontmatter.TaskID, repoID, err)
		}
		if err := ensureGitTaskWorktree(localPath, worktreePath, branchName, baseRevision); err != nil {
			return fmt.Errorf("ensure task worktree for task %s repo %s: %w", task.Frontmatter.TaskID, repoID, err)
		}
		if singleTarget && strings.TrimSpace(task.Frontmatter.BaseCommit) == "" {
			task.Frontmatter.BaseCommit = resolvedBaseCommit
		}
	}

	task.Frontmatter.WorkingBranches = normalizeStringList(workingBranches)
	task.Frontmatter.WorktreePaths = normalizeStringList(worktreePaths)
	return nil
}

func taskBranchForRepo(branches []string, repoID string) (string, bool) {
	for _, branchSpec := range normalizeDeclaredBranches(branches) {
		branchName, ok := branchRefForRepo(branchSpec, repoID)
		if !ok || branchName == "" {
			continue
		}
		return strings.TrimSpace(branchName), true
	}
	return "", false
}

func taskWorktreePathForRepo(worktreePaths []string, repoID string, allowUnscoped bool) (string, bool) {
	for _, spec := range normalizeStringList(worktreePaths) {
		path, ok := worktreePathSpecForRepo(spec, repoID, allowUnscoped)
		if !ok || path == "" {
			continue
		}
		return path, true
	}
	return "", false
}

func worktreePathSpecForRepo(spec, repoID string, allowUnscoped bool) (string, bool) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return "", false
	}
	if filepath.IsAbs(trimmed) {
		if !allowUnscoped {
			return "", false
		}
		return filepath.Clean(trimmed), true
	}
	scopeRepoID, pathValue, ok := splitScopePrefix(trimmed)
	if !ok || pathValue == "" {
		return "", false
	}
	if repoID == "" || !strings.EqualFold(scopeRepoID, repoID) {
		return "", false
	}
	if !filepath.IsAbs(pathValue) {
		return "", false
	}
	return filepath.Clean(pathValue), true
}

func repoScopedValue(repoID, value string) string {
	repoID = strings.TrimSpace(repoID)
	value = strings.TrimSpace(value)
	if repoID == "" {
		return value
	}
	return repoID + ":" + value
}

func defaultTaskBranchName(campaignID, taskID, repoID string) string {
	segments := []string{
		"codearmy",
		sanitizeBranchSegment(campaignID, "campaign"),
		sanitizeBranchSegment(taskID, "task"),
	}
	if repo := sanitizeBranchSegment(repoID, "repo"); repo != "" {
		segments = append(segments, repo)
	}
	return strings.Join(segments, "/")
}

func defaultTaskWorktreePath(root, repoID, taskID string) string {
	segments := []string{root, taskWorktreeRootDir}
	if repo := sanitizePathSegment(repoID, "repo"); repo != "" {
		segments = append(segments, repo)
	}
	segments = append(segments, sanitizePathSegment(taskID, "task"))
	return filepath.Clean(filepath.Join(segments...))
}

func sanitizeBranchSegment(raw, fallback string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(strings.ToLower(raw)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	value := strings.Trim(b.String(), "-./_")
	if value == "" {
		return fallback
	}
	return value
}

func sanitizePathSegment(raw, fallback string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(strings.ToLower(raw)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	value := strings.Trim(b.String(), "-._")
	if value == "" {
		return fallback
	}
	return value
}

func resolveTaskWorkspaceBase(repoDoc SourceRepoDocument, task TaskDocument) (string, string, error) {
	localPath := strings.TrimSpace(repoDoc.Frontmatter.LocalPath)
	if localPath == "" {
		return "", "", fmt.Errorf("source repo %s local_path is empty", repoDoc.Frontmatter.RepoID)
	}

	if taskBase := strings.TrimSpace(task.Frontmatter.BaseCommit); taskBase != "" {
		commit, err := gitResolveCommit(localPath, taskBase)
		if err != nil {
			return "", "", fmt.Errorf("task base_commit %s is not reachable: %w", taskBase, err)
		}
		return taskBase, commit, nil
	}
	if repoBase := strings.TrimSpace(repoDoc.Frontmatter.BaseCommit); repoBase != "" {
		commit, err := gitResolveCommit(localPath, repoBase)
		if err != nil {
			return "", "", fmt.Errorf("source repo base_commit %s is not reachable: %w", repoBase, err)
		}
		return repoBase, commit, nil
	}
	if defaultBranch := strings.TrimSpace(repoDoc.Frontmatter.DefaultBranch); defaultBranch != "" {
		if ref := gitBranchRef(localPath, defaultBranch); ref != "" {
			commit, err := gitResolveCommit(localPath, ref)
			if err != nil {
				return "", "", fmt.Errorf("default branch %s is not reachable: %w", defaultBranch, err)
			}
			return ref, commit, nil
		}
	}
	commit, err := gitResolveCommit(localPath, "HEAD")
	if err != nil {
		return "", "", err
	}
	return commit, commit, nil
}

func ensureGitTaskWorktree(repoPath, worktreePath, branchName, baseRevision string) error {
	repoPath = strings.TrimSpace(repoPath)
	worktreePath = filepath.Clean(strings.TrimSpace(worktreePath))
	branchName = strings.TrimSpace(branchName)
	baseRevision = strings.TrimSpace(baseRevision)
	if repoPath == "" || worktreePath == "" || branchName == "" {
		return fmt.Errorf("repo_path/worktree_path/branch_name must be non-empty")
	}

	if gitWorktreeExists(worktreePath) {
		if same, err := gitWorktreesShareCommonDir(repoPath, worktreePath); err != nil {
			return err
		} else if !same {
			return fmt.Errorf("existing worktree %s does not belong to source repo %s", worktreePath, repoPath)
		}
		currentBranch, err := gitCurrentBranch(worktreePath)
		if err != nil {
			return err
		}
		if currentBranch != branchName {
			return fmt.Errorf("existing worktree %s is on branch %s, expected %s", worktreePath, blankForSummary(currentBranch), branchName)
		}
		return nil
	}

	if info, err := os.Stat(worktreePath); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("worktree path %s exists and is not a directory", worktreePath)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return err
	}

	if gitLocalBranchExists(repoPath, branchName) {
		occupants, err := gitWorktreeBranchOccupants(repoPath, branchName)
		if err != nil {
			return fmt.Errorf("inspect existing worktrees for branch %s: %w", branchName, err)
		}
		for _, occupant := range occupants {
			occupant = filepath.Clean(strings.TrimSpace(occupant))
			if occupant == "" || occupant == worktreePath {
				continue
			}
			return fmt.Errorf("branch %s is already checked out at %s; working_branches must use a task-private branch or stay empty so Alice can generate one", branchName, occupant)
		}
		_, err = runGit(repoPath, "worktree", "add", worktreePath, branchName)
		return err
	}
	if baseRevision == "" {
		baseRevision = "HEAD"
	}
	_, err := runGit(repoPath, "worktree", "add", "-b", branchName, worktreePath, baseRevision)
	return err
}

func gitLocalBranchExists(path, branch string) bool {
	_, err := runGit(path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func gitResolveCommit(path, rev string) (string, error) {
	return runGit(path, "rev-parse", "--verify", rev+"^{commit}")
}

func gitCurrentBranch(path string) (string, error) {
	branch, err := runGit(path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read current branch for %s: %w", path, err)
	}
	return strings.TrimSpace(branch), nil
}

func gitCommonDir(path string) (string, error) {
	out, err := runGit(path, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	commonDir := strings.TrimSpace(out)
	if commonDir == "" {
		return "", fmt.Errorf("git common dir is empty for %s", path)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(path, commonDir)
	}
	commonDir = filepath.Clean(commonDir)
	if resolved, err := filepath.EvalSymlinks(commonDir); err == nil {
		commonDir = resolved
	}
	return commonDir, nil
}

func gitWorktreesShareCommonDir(repoPath, worktreePath string) (bool, error) {
	left, err := gitCommonDir(repoPath)
	if err != nil {
		return false, fmt.Errorf("resolve common dir for %s: %w", repoPath, err)
	}
	right, err := gitCommonDir(worktreePath)
	if err != nil {
		return false, fmt.Errorf("resolve common dir for %s: %w", worktreePath, err)
	}
	return filepath.Clean(left) == filepath.Clean(right), nil
}

func validateTaskWorktreePaths(task TaskDocument, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	if taskID == "" || len(task.Frontmatter.WorktreePaths) == 0 {
		return
	}

	targetRepos := make(map[string]struct{}, len(task.Frontmatter.TargetRepos))
	for _, repoID := range task.Frontmatter.TargetRepos {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		targetRepos[repoID] = struct{}{}
	}
	multiRepo := len(targetRepos) > 1

	for _, rawSpec := range normalizeStringList(task.Frontmatter.WorktreePaths) {
		spec := strings.TrimSpace(rawSpec)
		if spec == "" {
			continue
		}
		if filepath.IsAbs(spec) {
			if multiRepo {
				*issues = append(*issues, ValidationIssue{
					Code:    "task_worktree_path_scope_missing",
					Path:    task.Path,
					Message: fmt.Sprintf("task %s declares unscoped worktree_path %s but targets multiple repos", taskID, spec),
				})
			}
			continue
		}
		repoID, pathValue, ok := splitScopePrefix(spec)
		if !ok || pathValue == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_worktree_path_invalid",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s worktree_path %s must be an absolute path or repo-scoped absolute path", taskID, spec),
			})
			continue
		}
		if _, exists := targetRepos[repoID]; !exists {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_worktree_path_repo_unknown",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s worktree_path %s references repo %s outside target_repos", taskID, spec, repoID),
			})
		}
		if !filepath.IsAbs(pathValue) {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_worktree_path_not_absolute",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s worktree_path %s must use an absolute filesystem path", taskID, spec),
			})
		}
	}
}

func taskExecutionWorkspaceIssues(task TaskDocument, repos []SourceRepoDocument) []ValidationIssue {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	if taskID == "" || len(task.Frontmatter.WorktreePaths) == 0 || len(repos) == 0 {
		return nil
	}

	singleTarget := len(repos) == 1
	branchSpecs := normalizeDeclaredBranches(task.Frontmatter.WorkingBranches)
	var issues []ValidationIssue
	for _, repoDoc := range repos {
		repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
		localPath := strings.TrimSpace(repoDoc.Frontmatter.LocalPath)
		if repoID == "" || localPath == "" {
			continue
		}
		worktreePath, ok := taskWorktreePathForRepo(task.Frontmatter.WorktreePaths, repoID, singleTarget)
		if !ok {
			issues = append(issues, ValidationIssue{
				Code:    "task_worktree_path_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s does not declare a worktree_path for repo %s", taskID, repoID),
			})
			continue
		}
		if !gitWorktreeExists(worktreePath) {
			issues = append(issues, ValidationIssue{
				Code:    "task_worktree_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s worktree_path %s does not exist as a git worktree", taskID, worktreePath),
			})
			continue
		}
		sameRepo, err := gitWorktreesShareCommonDir(localPath, worktreePath)
		if err != nil {
			issues = append(issues, ValidationIssue{
				Code:    "task_worktree_unreadable",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s could not inspect worktree_path %s: %v", taskID, worktreePath, err),
			})
			continue
		}
		if !sameRepo {
			issues = append(issues, ValidationIssue{
				Code:    "task_worktree_repo_mismatch",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s worktree_path %s does not belong to repo %s", taskID, worktreePath, repoID),
			})
			continue
		}
		branchName, hasBranch := taskBranchForRepo(branchSpecs, repoID)
		if !hasBranch {
			continue
		}
		currentBranch, err := gitCurrentBranch(worktreePath)
		if err != nil {
			issues = append(issues, ValidationIssue{
				Code:    "task_worktree_branch_unreadable",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s could not read current branch for worktree_path %s: %v", taskID, worktreePath, err),
			})
			continue
		}
		if currentBranch != branchName {
			issues = append(issues, ValidationIssue{
				Code:    "task_worktree_branch_mismatch",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s worktree_path %s is on branch %s, expected %s", taskID, worktreePath, currentBranch, branchName),
			})
		}
	}
	return issues
}
