package campaignrepo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type ValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues,omitempty"`
}

var taskFileRefPattern = regexp.MustCompile("(?m)(?:^|[^A-Za-z0-9_./-])((?:src|tests)/[A-Za-z0-9_./-]+\\.[A-Za-z0-9_.-]+|Cargo\\.toml|Cargo\\.lock)(?:$|[^A-Za-z0-9_./-])")

func (r ValidationResult) Error() error {
	if r.Valid {
		return nil
	}
	if len(r.Issues) == 0 {
		return fmt.Errorf("campaign repo validation failed")
	}
	first := r.Issues[0]
	return fmt.Errorf("%s: %s", first.Code, first.Message)
}

func Validate(root string) (Repository, ValidationResult, error) {
	repo, err := Load(root)
	if err != nil {
		return Repository{}, ValidationResult{}, err
	}
	return repo, ValidateRepository(repo), nil
}

func ValidateForApproval(root string) (Repository, ValidationResult, error) {
	repo, err := Load(root)
	if err != nil {
		return Repository{}, ValidationResult{}, err
	}
	return repo, ValidateRepositoryForApproval(repo), nil
}

func ValidateRepository(repo Repository) ValidationResult {
	return validateRepository(repo, false)
}

func ValidateRepositoryForApproval(repo Repository) ValidationResult {
	return validateRepository(repo, true)
}

func validateRepository(repo Repository, requireApprovalArtifacts bool) ValidationResult {
	var issues []ValidationIssue

	phaseByID := make(map[string]PhaseDocument, len(repo.Phases))
	for _, phase := range repo.Phases {
		phaseID := strings.TrimSpace(phase.Frontmatter.Phase)
		if phaseID == "" {
			issues = append(issues, ValidationIssue{
				Code:    "phase_missing_id",
				Path:    phase.Path,
				Message: "phase frontmatter.phase is empty",
			})
			continue
		}
		expected := filepath.ToSlash(filepath.Join("phases", phaseID, "phase.md"))
		if filepath.ToSlash(phase.Path) != expected {
			issues = append(issues, ValidationIssue{
				Code:    "phase_path_mismatch",
				Path:    phase.Path,
				Message: fmt.Sprintf("phase %s should live at %s", phaseID, expected),
			})
		}
		if _, exists := phaseByID[phaseID]; exists {
			issues = append(issues, ValidationIssue{
				Code:    "phase_duplicate",
				Path:    phase.Path,
				Message: fmt.Sprintf("duplicate phase id %s", phaseID),
			})
			continue
		}
		phaseByID[phaseID] = phase
		if isPlaceholderText(phase.Frontmatter.Goal) {
			issues = append(issues, ValidationIssue{
				Code:    "phase_goal_missing",
				Path:    phase.Path,
				Message: fmt.Sprintf("phase %s must define a concrete goal", phaseID),
			})
		}
	}

	currentPhase := strings.TrimSpace(repo.Campaign.Frontmatter.CurrentPhase)
	if currentPhase != "" {
		if _, ok := phaseByID[currentPhase]; !ok {
			issues = append(issues, ValidationIssue{
				Code:    "campaign_current_phase_missing",
				Path:    repo.Campaign.Path,
				Message: fmt.Sprintf("campaign current_phase %s has no matching phase.md", currentPhase),
			})
		}
	}

	taskByID := make(map[string]TaskDocument, len(repo.Tasks))
	sourceRepoByID := make(map[string]SourceRepoDocument, len(repo.SourceRepos))
	for _, repoDoc := range repo.SourceRepos {
		repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
		if repoID == "" {
			continue
		}
		if _, exists := sourceRepoByID[repoID]; exists {
			issues = append(issues, ValidationIssue{
				Code:    "source_repo_duplicate",
				Path:    repoDoc.Path,
				Message: fmt.Sprintf("duplicate source repo id %s", repoID),
			})
			continue
		}
		sourceRepoByID[repoID] = repoDoc
		validateSourceRepoDocument(repoDoc, &issues)
	}
	validateCampaignSourceRepoIndex(repo.Campaign, sourceRepoByID, &issues)

	for _, task := range repo.Tasks {
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		if taskID == "" {
			issues = append(issues, ValidationIssue{
				Code:    "task_missing_id",
				Path:    task.Path,
				Message: "task frontmatter.task_id is empty",
			})
			continue
		}
		if _, exists := taskByID[taskID]; exists {
			issues = append(issues, ValidationIssue{
				Code:    "task_duplicate",
				Path:    task.Path,
				Message: fmt.Sprintf("duplicate task id %s", taskID),
			})
			continue
		}
		taskByID[taskID] = task
		validateTaskDocument(repo.Root, task, phaseByID, sourceRepoByID, &issues)
	}

	for _, task := range repo.Tasks {
		validateTaskDependencies(task, taskByID, &issues)
	}

	reviewKeys := make(map[string]struct{}, len(repo.Reviews))
	for _, review := range repo.Reviews {
		key := strings.TrimSpace(review.Path)
		if key == "" {
			continue
		}
		if _, exists := reviewKeys[key]; exists {
			issues = append(issues, ValidationIssue{
				Code:    "review_duplicate_path",
				Path:    review.Path,
				Message: fmt.Sprintf("duplicate review path %s", review.Path),
			})
			continue
		}
		reviewKeys[key] = struct{}{}
		validateReviewDocument(review, taskByID, &issues)
	}

	if requireApprovalArtifacts {
		validateApprovalArtifacts(repo, &issues)
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Message < issues[j].Message
	})
	return ValidationResult{
		Valid:  len(issues) == 0,
		Issues: issues,
	}
}

func validateTaskDocument(root string, task TaskDocument, phases map[string]PhaseDocument, repos map[string]SourceRepoDocument, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	phaseID := strings.TrimSpace(task.Frontmatter.Phase)
	expectedDir := canonicalTaskDir(phaseID, taskID, task.Dir)
	expectedPath := filepath.ToSlash(filepath.Join(expectedDir, "task.md"))

	if phaseID == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_phase_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s must declare frontmatter.phase", taskID),
		})
	} else if _, ok := phases[phaseID]; !ok {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_phase_missing_doc",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s references missing phase %s", taskID, phaseID),
		})
	}
	if filepath.ToSlash(task.Dir) != expectedDir {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_dir_mismatch",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s must live in %s", taskID, expectedDir),
		})
	}
	if filepath.ToSlash(task.Path) != expectedPath {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_path_mismatch",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s must use task.md at %s", taskID, expectedPath),
		})
	}
	validateExistingFile(root, task.Path, "task file", issues)
	validateExistingFile(root, task.ContextPath, "task context", issues)
	validateExistingFile(root, task.PlanPath, "task plan", issues)
	validateExistingFile(root, task.ProgressPath, "task progress", issues)
	validateExistingDir(root, task.ResultsDir, "task results dir", issues)
	validateExistingDir(root, task.ReviewsDir, "task reviews dir", issues)

	requireMarkdownSection(task.Path, task.Body, "Goal", issues, "task_goal_missing")
	requireMarkdownSection(task.Path, task.Body, "Background", issues, "task_background_missing")
	requireMarkdownSection(task.Path, task.Body, "Acceptance", issues, "task_acceptance_missing")
	requireMarkdownSection(task.Path, task.Body, "Deliverables", issues, "task_deliverables_missing")

	requireMarkdownSection(task.ContextPath, task.ContextBody, "Context", issues, "task_context_section_missing")
	requireMarkdownSection(task.ContextPath, task.ContextBody, "Relevant Repos", issues, "task_context_repos_missing")
	requireMarkdownSection(task.ContextPath, task.ContextBody, "Relevant Files", issues, "task_context_files_missing")
	requireMarkdownSection(task.ContextPath, task.ContextBody, "Dependencies", issues, "task_context_dependencies_missing")

	requireMarkdownSection(task.PlanPath, task.PlanBody, "Execution Steps", issues, "task_plan_steps_missing")
	requireMarkdownSection(task.PlanPath, task.PlanBody, "Validation", issues, "task_plan_validation_missing")
	requireMarkdownSection(task.PlanPath, task.PlanBody, "Handoff", issues, "task_plan_handoff_missing")

	if isPlaceholderText(task.Frontmatter.Title) {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_title_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s must define a concrete title", taskID),
		})
	}
	if len(task.Frontmatter.TargetRepos) == 0 {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_target_repo_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s must define target_repos", taskID),
		})
	}
	if len(task.Frontmatter.WriteScope) == 0 {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_write_scope_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s must define write_scope", taskID),
		})
	}
	for _, repoID := range task.Frontmatter.TargetRepos {
		if _, ok := repos[repoID]; !ok {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_target_repo_unknown",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s references unknown source repo %s", taskID, repoID),
			})
		}
	}
	validateTaskStatusValue(root, task, issues)
	validateTaskStateContract(root, task, repos, issues)
	validateTaskRoleWorkflows(task, issues)
	validateTaskWriteScopeCoverage(task, issues)
}

func validateTaskDependencies(task TaskDocument, taskByID map[string]TaskDocument, issues *[]ValidationIssue) {
	for _, depID := range task.Frontmatter.DependsOn {
		if _, ok := taskByID[depID]; ok {
			continue
		}
		*issues = append(*issues, ValidationIssue{
			Code:    "task_dependency_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s depends on missing task %s", task.Frontmatter.TaskID, depID),
		})
	}
}

func validateTaskStateContract(root string, task TaskDocument, repos map[string]SourceRepoDocument, issues *[]ValidationIssue) {
	status := normalizeTaskStatus(task.Frontmatter.Status)
	switch status {
	case TaskStatusReviewPending, TaskStatusReviewing:
		if taskRequiresSourceRepoEvidence(task) && strings.TrimSpace(task.Frontmatter.HeadCommit) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_review_pending_head_commit_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is %s but head_commit is empty", task.Frontmatter.TaskID, status),
			})
		}
		if strings.TrimSpace(task.Frontmatter.LastRunPath) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_review_pending_last_run_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is %s but last_run_path is empty", task.Frontmatter.TaskID, status),
			})
		}
		if status == TaskStatusReviewPending && (strings.TrimSpace(task.Frontmatter.OwnerAgent) != "" || !task.LeaseUntil.IsZero()) {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_review_pending_lease_not_cleared",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is review_pending but owner_agent/lease_until is still set", task.Frontmatter.TaskID),
			})
		}
		*issues = append(*issues, taskExecutionArtifactIssues(root, task, repos)...)
	case TaskStatusWaitingExternal:
		if task.WakeAt.IsZero() {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_waiting_external_wake_at_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is waiting_external but wake_at is empty", task.Frontmatter.TaskID),
			})
		}
		if strings.TrimSpace(task.Frontmatter.WakePrompt) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_waiting_external_wake_prompt_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is waiting_external but wake_prompt is empty", task.Frontmatter.TaskID),
			})
		}
	case TaskStatusExecuting:
		if strings.TrimSpace(task.Frontmatter.OwnerAgent) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_active_owner_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is %s but owner_agent is empty", task.Frontmatter.TaskID, status),
			})
		}
		if task.LeaseUntil.IsZero() {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_active_lease_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is %s but lease_until is empty", task.Frontmatter.TaskID, status),
			})
		}
	case TaskStatusAccepted, TaskStatusDone, TaskStatusRejected:
		if strings.TrimSpace(task.Frontmatter.OwnerAgent) != "" || !task.LeaseUntil.IsZero() {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_terminal_lease_not_cleared",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is terminal but owner_agent/lease_until is still set", task.Frontmatter.TaskID),
			})
		}
	}
	if status == TaskStatusReviewing {
		if strings.TrimSpace(task.Frontmatter.OwnerAgent) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_active_owner_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is %s but owner_agent is empty", task.Frontmatter.TaskID, status),
			})
		}
		if task.LeaseUntil.IsZero() {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_active_lease_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is %s but lease_until is empty", task.Frontmatter.TaskID, status),
			})
		}
	}
}

func validateTaskStatusValue(root string, task TaskDocument, issues *[]ValidationIssue) {
	raw := rawTaskStatusValue(root, task)
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return
	}
	if raw == "planned" {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_status_planned_deprecated",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s uses deprecated status %q; planner-generated task packages must use status %q", task.Frontmatter.TaskID, raw, TaskStatusDraft),
		})
		return
	}

	switch normalizeTaskStatus(task.Frontmatter.Status) {
	case TaskStatusDraft, TaskStatusReady, TaskStatusExecuting, TaskStatusReviewPending, TaskStatusReviewing,
		TaskStatusRework, TaskStatusAccepted, TaskStatusBlocked, TaskStatusWaitingExternal, TaskStatusDone, TaskStatusRejected:
		return
	default:
		*issues = append(*issues, ValidationIssue{
			Code:    "task_status_unknown",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s uses unsupported status %q", task.Frontmatter.TaskID, task.Frontmatter.Status),
		})
	}
}

func rawTaskStatusValue(root string, task TaskDocument) string {
	path := filepath.Join(root, filepath.FromSlash(task.Path))
	raw, err := os.ReadFile(path)
	if err != nil {
		return task.Frontmatter.Status
	}
	parsed := parseMarkdownFrontmatter(string(raw))
	if !parsed.Found {
		return task.Frontmatter.Status
	}
	for _, line := range strings.Split(parsed.Frontmatter, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "status:") {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "status:")), `"'`)
	}
	return task.Frontmatter.Status
}

func taskExecutionArtifactIssues(root string, task TaskDocument, repos map[string]SourceRepoDocument) []ValidationIssue {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	if taskID == "" {
		return nil
	}

	var issues []ValidationIssue
	lastRunPath := strings.TrimSpace(task.Frontmatter.LastRunPath)
	if lastRunPath != "" {
		if _, ok := resolveTaskArtifactPath(root, task, lastRunPath); !ok {
			issues = append(issues, ValidationIssue{
				Code:    "task_last_run_path_missing_file",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s last_run_path %s does not resolve to a file inside the campaign repo", taskID, lastRunPath),
			})
		}
	}
	if !taskRequiresSourceRepoEvidence(task) {
		return issues
	}

	targetRepos := resolveTaskSourceRepos(task, repos)
	if len(targetRepos) == 0 {
		return issues
	}

	headCommit := strings.TrimSpace(task.Frontmatter.HeadCommit)
	if headCommit == "" {
		return issues
	}
	if !gitCommitExistsInRepos(targetRepos, headCommit) {
		issues = append(issues, ValidationIssue{
			Code:    "task_head_commit_unknown",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s head_commit %s is not reachable in any target_repos local_path", taskID, headCommit),
		})
		return issues
	}
	if baseCommit := strings.TrimSpace(task.Frontmatter.BaseCommit); baseCommit != "" {
		diffIssues := taskExecutionDiffIssues(task, targetRepos, baseCommit, headCommit)
		issues = append(issues, diffIssues...)
	}

	workingBranches := normalizeDeclaredBranches(task.Frontmatter.WorkingBranches)
	if len(workingBranches) == 0 {
		return issues
	}
	if !gitAnyBranchExistsInRepos(targetRepos, workingBranches) {
		issues = append(issues, ValidationIssue{
			Code:    "task_working_branch_unknown",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s declared working_branches %s but none exist in target_repos local_path", taskID, strings.Join(workingBranches, ", ")),
		})
		return issues
	}
	if !gitAnyBranchContainsCommit(targetRepos, workingBranches, headCommit) {
		issues = append(issues, ValidationIssue{
			Code:    "task_head_commit_not_on_working_branch",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s head_commit %s is not reachable from declared working_branches %s", taskID, headCommit, strings.Join(workingBranches, ", ")),
		})
	}
	return issues
}

func taskExecutionDiffIssues(task TaskDocument, repos []SourceRepoDocument, baseCommit string, headCommit string) []ValidationIssue {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	if taskID == "" {
		return nil
	}
	baseCommit = strings.TrimSpace(baseCommit)
	headCommit = strings.TrimSpace(headCommit)
	if baseCommit == "" || headCommit == "" {
		return nil
	}
	var issues []ValidationIssue
	diffChecked := false
	for _, repo := range repos {
		localPath := strings.TrimSpace(repo.Frontmatter.LocalPath)
		if localPath == "" {
			continue
		}
		if !gitCommitExists(localPath, baseCommit) || !gitCommitExists(localPath, headCommit) {
			continue
		}
		diffChecked = true
		files, err := gitChangedFiles(localPath, baseCommit, headCommit)
		if err != nil {
			issues = append(issues, ValidationIssue{
				Code:    "task_execution_diff_unreadable",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s could not read changed files for %s..%s in %s: %v", taskID, baseCommit, headCommit, localPath, err),
			})
			continue
		}
		for _, file := range files {
			if writeScopeCoversRepoRef(task.Frontmatter.WriteScope, repo.Frontmatter.RepoID, file) {
				continue
			}
			issues = append(issues, ValidationIssue{
				Code:    "task_head_diff_outside_write_scope",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s changed %s between %s..%s but write_scope does not cover it", taskID, file, baseCommit, headCommit),
			})
		}
	}
	if diffChecked {
		return issues
	}
	return []ValidationIssue{{
		Code:    "task_base_commit_unknown",
		Path:    task.Path,
		Message: fmt.Sprintf("task %s base_commit %s is not reachable with head_commit %s in any target_repos local_path", taskID, baseCommit, headCommit),
	}}
}

func taskExecutionArtifactBlockReason(root string, task TaskDocument, repos map[string]SourceRepoDocument) string {
	issues := taskExecutionArtifactIssues(root, task, repos)
	if len(issues) == 0 {
		return ""
	}
	return issues[0].Message
}

func resolveTaskSourceRepos(task TaskDocument, repos map[string]SourceRepoDocument) []SourceRepoDocument {
	if len(task.Frontmatter.TargetRepos) == 0 || len(repos) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(task.Frontmatter.TargetRepos))
	out := make([]SourceRepoDocument, 0, len(task.Frontmatter.TargetRepos))
	for _, repoID := range task.Frontmatter.TargetRepos {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		doc, ok := repos[repoID]
		if !ok {
			continue
		}
		out = append(out, doc)
	}
	return out
}

func taskRequiresSourceRepoEvidence(task TaskDocument) bool {
	sawScope := false
	for _, raw := range task.Frontmatter.WriteScope {
		scope := strings.ToLower(strings.TrimSpace(filepath.ToSlash(raw)))
		if scope == "" {
			continue
		}
		sawScope = true
		if !strings.HasPrefix(scope, "campaign:") {
			return true
		}
	}
	if sawScope {
		return false
	}
	return len(task.Frontmatter.TargetRepos) > 0 ||
		len(task.Frontmatter.WorkingBranches) > 0 ||
		strings.TrimSpace(task.Frontmatter.BaseCommit) != "" ||
		strings.TrimSpace(task.Frontmatter.HeadCommit) != ""
}

func resolveTaskArtifactPath(root string, task TaskDocument, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	candidates := []string{}
	if filepath.IsAbs(raw) {
		candidates = append(candidates, raw)
	} else {
		rel := filepath.FromSlash(raw)
		candidates = append(candidates, filepath.Join(root, rel))
		candidates = append(candidates, filepath.Join(root, filepath.FromSlash(task.Dir), rel))
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func normalizeDeclaredBranches(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func splitScopePrefix(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", false
	}
	left, right, ok := strings.Cut(trimmed, ":")
	if !ok || strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return "", trimmed, false
	}
	if strings.ContainsAny(left, `/\`) {
		return "", trimmed, false
	}
	return strings.TrimSpace(left), strings.TrimSpace(right), true
}

func branchRefForRepo(branchSpec, repoID string) (string, bool) {
	repoID = strings.TrimSpace(repoID)
	prefix, branch, ok := splitScopePrefix(branchSpec)
	if !ok {
		return strings.TrimSpace(branchSpec), true
	}
	if repoID == "" || !strings.EqualFold(prefix, repoID) {
		return "", false
	}
	return strings.TrimSpace(branch), true
}

func validateReviewDocument(review ReviewDocument, taskByID map[string]TaskDocument, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(review.Frontmatter.TargetTask)
	if taskID == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_target_missing",
			Path:    review.Path,
			Message: "review frontmatter.target_task is empty",
		})
		return
	}
	task, ok := taskByID[taskID]
	if !ok {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_target_unknown",
			Path:    review.Path,
			Message: fmt.Sprintf("review targets missing task %s", taskID),
		})
		return
	}
	expectedDir := filepath.ToSlash(filepath.Join(task.Dir, "reviews"))
	expectedPath := filepath.ToSlash(filepath.Join(expectedDir, filepath.Base(review.Path)))
	if filepath.ToSlash(review.Dir) != expectedDir {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_dir_mismatch",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s must live under %s", taskID, expectedDir),
		})
	}
	if filepath.ToSlash(review.Path) != expectedPath {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_path_mismatch",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s must stay inside task-local reviews dir", taskID),
		})
	}
	if review.Frontmatter.ReviewRound <= 0 {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_round_missing",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s must set review_round > 0", taskID),
		})
	}
	if strings.TrimSpace(review.Frontmatter.Reviewer.Role) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_role_missing",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s must set reviewer.role", taskID),
		})
	}
	validateRoleWorkflow(review.Path, "review", taskID, "reviewer", review.Frontmatter.Reviewer.Workflow, issues)
}

func validateTaskRoleWorkflows(task TaskDocument, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	validateRoleWorkflow(task.Path, "task", taskID, "executor", task.Frontmatter.Executor.Workflow, issues)
	validateRoleWorkflow(task.Path, "task", taskID, "reviewer", task.Frontmatter.Reviewer.Workflow, issues)
}

func validateTaskWriteScopeCoverage(task TaskDocument, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	if taskID == "" || len(task.Frontmatter.WriteScope) == 0 {
		return
	}
	checks := []struct {
		path    string
		label   string
		content string
	}{
		{
			path:    task.Path,
			label:   "Acceptance",
			content: markdownSectionContent(task.Body, "Acceptance"),
		},
		{
			path:    task.Path,
			label:   "Deliverables",
			content: markdownSectionContent(task.Body, "Deliverables"),
		},
		{
			path:    task.PlanPath,
			label:   "Execution Steps",
			content: markdownSectionContent(task.PlanBody, "Execution Steps"),
		},
	}
	for _, check := range checks {
		for _, ref := range referencedTaskFiles(check.content) {
			if writeScopeCoversRef(task.Frontmatter.WriteScope, ref) {
				continue
			}
			*issues = append(*issues, ValidationIssue{
				Code:    "task_write_scope_incomplete",
				Path:    check.path,
				Message: fmt.Sprintf("task %s %s references %s but write_scope does not cover it", taskID, check.label, ref),
			})
		}
	}
}

func validateRoleWorkflow(path, artifactKind, artifactID, roleName, rawWorkflow string, issues *[]ValidationIssue) {
	workflow := strings.ToLower(strings.TrimSpace(rawWorkflow))
	if workflow == "" {
		return
	}
	if workflow == "code_army" {
		return
	}
	*issues = append(*issues, ValidationIssue{
		Code:    fmt.Sprintf("%s_role_workflow_invalid", artifactKind),
		Path:    path,
		Message: fmt.Sprintf("%s %s must use %s.workflow=code_army, got %q", artifactKind, artifactID, roleName, rawWorkflow),
	})
}

func referencedTaskFiles(content string) []string {
	matches := taskFileRefPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		ref := filepath.ToSlash(strings.TrimSpace(match[1]))
		if !isTaskContractFileRef(ref) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func isTaskContractFileRef(ref string) bool {
	switch ref {
	case "Cargo.toml", "Cargo.lock":
		return true
	}
	return strings.HasPrefix(ref, "src/") || strings.HasPrefix(ref, "tests/")
}

func writeScopeCoversRef(writeScope []string, ref string) bool {
	_, ref, _ = splitScopePrefix(ref)
	ref = filepath.ToSlash(strings.TrimSpace(ref))
	if ref == "" {
		return false
	}
	for _, rawScope := range writeScope {
		_, scope, _ := splitScopePrefix(rawScope)
		scope = filepath.ToSlash(strings.TrimSpace(scope))
		if scope == "" {
			continue
		}
		if scope == ref {
			return true
		}
		if strings.ContainsAny(scope, "*?[") {
			if ok, _ := filepath.Match(scope, ref); ok {
				return true
			}
			if strings.HasSuffix(scope, "/**") {
				prefix := strings.TrimSuffix(scope, "/**")
				if ref == prefix || strings.HasPrefix(ref, prefix+"/") {
					return true
				}
			}
			continue
		}
		if !strings.Contains(filepath.Base(scope), ".") {
			if ref == scope || strings.HasPrefix(ref, scope+"/") {
				return true
			}
		}
	}
	return false
}

func writeScopeCoversRepoRef(writeScope []string, repoID, ref string) bool {
	ref = filepath.ToSlash(strings.TrimSpace(ref))
	repoID = strings.TrimSpace(repoID)
	if ref == "" {
		return false
	}
	for _, rawScope := range writeScope {
		scopeRepoID, scope, ok := splitScopePrefix(rawScope)
		if ok {
			if repoID == "" || !strings.EqualFold(scopeRepoID, repoID) {
				continue
			}
		} else {
			scope = rawScope
		}
		scope = filepath.ToSlash(strings.TrimSpace(scope))
		if scope == "" {
			continue
		}
		if scope == ref {
			return true
		}
		if strings.ContainsAny(scope, "*?[") {
			if ok, _ := filepath.Match(scope, ref); ok {
				return true
			}
			if strings.HasSuffix(scope, "/**") {
				prefix := strings.TrimSuffix(scope, "/**")
				if ref == prefix || strings.HasPrefix(ref, prefix+"/") {
					return true
				}
			}
			continue
		}
		if !strings.Contains(filepath.Base(scope), ".") {
			if ref == scope || strings.HasPrefix(ref, scope+"/") {
				return true
			}
		}
	}
	return false
}

func validateCampaignSourceRepoIndex(campaignDoc CampaignDocument, sourceRepoByID map[string]SourceRepoDocument, issues *[]ValidationIssue) {
	expected := make(map[string]struct{}, len(campaignDoc.Frontmatter.SourceRepos))
	for _, repoID := range campaignDoc.Frontmatter.SourceRepos {
		expected[repoID] = struct{}{}
		if _, ok := sourceRepoByID[repoID]; ok {
			continue
		}
		*issues = append(*issues, ValidationIssue{
			Code:    "campaign_source_repo_missing_doc",
			Path:    campaignDoc.Path,
			Message: fmt.Sprintf("campaign source_repos references missing repos/%s.md", repoID),
		})
	}
	for repoID, doc := range sourceRepoByID {
		if _, ok := expected[repoID]; ok {
			continue
		}
		*issues = append(*issues, ValidationIssue{
			Code:    "campaign_source_repo_unindexed",
			Path:    doc.Path,
			Message: fmt.Sprintf("source repo %s exists in repos/ but is absent from campaign.md source_repos", repoID),
		})
	}
}

func validateSourceRepoDocument(repoDoc SourceRepoDocument, issues *[]ValidationIssue) {
	repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
	if strings.TrimSpace(repoDoc.Frontmatter.LocalPath) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_local_path_missing",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s must set local_path", repoID),
		})
		return
	}
	localPath := repoDoc.Frontmatter.LocalPath
	info, err := os.Stat(localPath)
	if err != nil {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_local_path_missing",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s local_path does not exist: %s", repoID, localPath),
		})
		return
	}
	if !info.IsDir() {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_local_path_not_dir",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s local_path is not a directory: %s", repoID, localPath),
		})
		return
	}
	if strings.TrimSpace(repoDoc.Frontmatter.DefaultBranch) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_default_branch_missing",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s must set default_branch", repoID),
		})
	}
	if !gitWorktreeExists(localPath) {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_not_git",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s local_path is not a git worktree: %s", repoID, localPath),
		})
		return
	}
	if branch := strings.TrimSpace(repoDoc.Frontmatter.DefaultBranch); branch != "" && !gitBranchExists(localPath, branch) {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_default_branch_unknown",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s default_branch %s does not exist in %s", repoID, branch, localPath),
		})
	}
	if commit := strings.TrimSpace(repoDoc.Frontmatter.BaseCommit); commit != "" && !gitCommitExists(localPath, commit) {
		*issues = append(*issues, ValidationIssue{
			Code:    "source_repo_base_commit_unknown",
			Path:    repoDoc.Path,
			Message: fmt.Sprintf("source repo %s base_commit %s is not reachable in %s", repoID, commit, localPath),
		})
	}
}

func validateApprovalArtifacts(repo Repository, issues *[]ValidationIssue) {
	round := repo.Campaign.Frontmatter.PlanRound
	planStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)
	if planStatus != PlanStatusPlanApproved {
		*issues = append(*issues, ValidationIssue{
			Code:    "approve_plan_status_invalid",
			Path:    repo.Campaign.Path,
			Message: fmt.Sprintf("plan_status must be %s before human approval, got %s", PlanStatusPlanApproved, planStatus),
		})
	}
	proposal, ok := latestProposalForRound(repo.PlanProposals, round)
	if !ok || proposal.Frontmatter.Status != "submitted" {
		*issues = append(*issues, ValidationIssue{
			Code:    "approve_proposal_missing",
			Path:    filepath.ToSlash(filepath.Join("plans", "proposals")),
			Message: fmt.Sprintf("plan round %d requires a submitted proposal", round),
		})
	}
	review, ok := latestPlanReviewForRound(repo.PlanReviews, round)
	if !ok || normalizeReviewVerdict(review.Frontmatter.Verdict, review.Frontmatter.Blocking) != "approve" {
		*issues = append(*issues, ValidationIssue{
			Code:    "approve_review_missing",
			Path:    filepath.ToSlash(filepath.Join("plans", "reviews")),
			Message: fmt.Sprintf("plan round %d requires an approving plan review", round),
		})
	}
	masterPlanPath := filepath.Join(repo.Root, "plans", "merged", "master-plan.md")
	masterPlanBody, err := loadMarkdownBodyIfExists(masterPlanPath)
	if err != nil || isPlaceholderText(masterPlanBody) {
		*issues = append(*issues, ValidationIssue{
			Code:    "approve_master_plan_missing",
			Path:    filepath.ToSlash(relativePath(repo.Root, masterPlanPath)),
			Message: "merged master-plan.md must exist and contain a concrete refined plan",
		})
	}
	if len(repo.Tasks) == 0 {
		*issues = append(*issues, ValidationIssue{
			Code:    "approve_task_tree_empty",
			Path:    filepath.ToSlash(filepath.Join("phases")),
			Message: "approve-plan requires planner to fully expand at least one phase and one task package",
		})
	}
}

func validateExistingFile(root, relPath, label string, issues *[]ValidationIssue) {
	if strings.TrimSpace(relPath) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_required_file_missing",
			Path:    relPath,
			Message: fmt.Sprintf("%s path is empty", label),
		})
		return
	}
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if info, err := os.Stat(absPath); err != nil || info.IsDir() {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_required_file_missing",
			Path:    relPath,
			Message: fmt.Sprintf("%s is missing at %s", label, relPath),
		})
	}
}

func validateExistingDir(root, relPath, label string, issues *[]ValidationIssue) {
	if strings.TrimSpace(relPath) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_required_dir_missing",
			Path:    relPath,
			Message: fmt.Sprintf("%s path is empty", label),
		})
		return
	}
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_required_dir_missing",
			Path:    relPath,
			Message: fmt.Sprintf("%s is missing at %s", label, relPath),
		})
	}
}

func requireMarkdownSection(path, body, heading string, issues *[]ValidationIssue, code string) {
	content := markdownSectionContent(body, heading)
	if !isPlaceholderText(content) {
		return
	}
	*issues = append(*issues, ValidationIssue{
		Code:    code,
		Path:    path,
		Message: fmt.Sprintf("%s must include a non-placeholder \"## %s\" section", blankForSummary(path), heading),
	})
}

func markdownSectionContent(body, heading string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	var buf []string
	inSection := false
	target := "## " + heading
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if trimmed == target {
				inSection = true
				buf = buf[:0]
				continue
			}
			if inSection {
				break
			}
		}
		if inSection {
			buf = append(buf, line)
		}
	}
	return strings.TrimSpace(strings.Join(buf, "\n"))
}

func isPlaceholderText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	for _, line := range strings.Split(value, "\n") {
		candidate := normalizePlaceholderCandidate(line)
		if candidate == "" {
			continue
		}
		if !isPlaceholderCandidate(candidate) {
			return false
		}
	}
	return true
}

func normalizePlaceholderCandidate(line string) string {
	candidate := strings.TrimSpace(line)
	for {
		trimmed := strings.TrimLeft(candidate, "#>*- \t")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == candidate {
			break
		}
		candidate = trimmed
	}
	candidate = strings.Trim(candidate, "`*_\"'[]()")
	return strings.TrimSpace(candidate)
}

func isPlaceholderCandidate(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "-", "tbd", "todo", "待补充", "待完善", "待填写", "pending":
		return true
	}
	for _, prefix := range []string{"tbd", "todo", "pending", "待补充", "待完善", "待填写"} {
		if strings.HasPrefix(normalized, prefix+":") || strings.HasPrefix(normalized, prefix+"：") || strings.HasPrefix(normalized, prefix+" ") {
			return true
		}
	}
	return false
}

func gitWorktreeExists(path string) bool {
	_, err := runGit(path, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func gitBranchExists(path, branch string) bool {
	return gitBranchRef(path, branch) != ""
}

func gitCommitExists(path, commit string) bool {
	_, err := runGit(path, "rev-parse", "--verify", commit+"^{commit}")
	return err == nil
}

func gitChangedFiles(path, baseCommit, headCommit string) ([]string, error) {
	output, err := runGit(path, "diff", "--name-only", "--no-renames", baseCommit+".."+headCommit)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	lines := strings.Split(output, "\n")
	files := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		file := filepath.ToSlash(strings.TrimSpace(line))
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		files = append(files, file)
	}
	sort.Strings(files)
	return files, nil
}

func gitBranchRef(path, branch string) string {
	if _, err := runGit(path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return "refs/heads/" + branch
	}
	if _, err := runGit(path, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
		return "refs/remotes/origin/" + branch
	}
	return ""
}

func gitBranchContainsCommit(path, branch, commit string) bool {
	ref := gitBranchRef(path, branch)
	if ref == "" {
		return false
	}
	_, err := runGit(path, "merge-base", "--is-ancestor", commit, ref)
	return err == nil
}

func gitCommitExistsInRepos(repos []SourceRepoDocument, commit string) bool {
	for _, repo := range repos {
		if gitCommitExists(strings.TrimSpace(repo.Frontmatter.LocalPath), commit) {
			return true
		}
	}
	return false
}

func gitAnyBranchExistsInRepos(repos []SourceRepoDocument, branches []string) bool {
	for _, repo := range repos {
		localPath := strings.TrimSpace(repo.Frontmatter.LocalPath)
		repoID := strings.TrimSpace(repo.Frontmatter.RepoID)
		for _, branch := range branches {
			branchName, ok := branchRefForRepo(branch, repoID)
			if !ok || branchName == "" {
				continue
			}
			if gitBranchExists(localPath, branchName) {
				return true
			}
		}
	}
	return false
}

func gitAnyBranchContainsCommit(repos []SourceRepoDocument, branches []string, commit string) bool {
	for _, repo := range repos {
		localPath := strings.TrimSpace(repo.Frontmatter.LocalPath)
		repoID := strings.TrimSpace(repo.Frontmatter.RepoID)
		for _, branch := range branches {
			branchName, ok := branchRefForRepo(branch, repoID)
			if !ok || branchName == "" {
				continue
			}
			if gitBranchContainsCommit(localPath, branchName, commit) {
				return true
			}
		}
	}
	return false
}

func runGit(path string, args ...string) (string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, append([]string{"-C", path}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
