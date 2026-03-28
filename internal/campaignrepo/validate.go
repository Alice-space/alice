package campaignrepo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	validateTaskStateContract(task, issues)
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

func validateTaskStateContract(task TaskDocument, issues *[]ValidationIssue) {
	status := normalizeTaskStatus(task.Frontmatter.Status)
	switch status {
	case TaskStatusReviewPending:
		if strings.TrimSpace(task.Frontmatter.HeadCommit) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_review_pending_head_commit_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is review_pending but head_commit is empty", task.Frontmatter.TaskID),
			})
		}
		if strings.TrimSpace(task.Frontmatter.LastRunPath) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_review_pending_last_run_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is review_pending but last_run_path is empty", task.Frontmatter.TaskID),
			})
		}
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
	case TaskStatusExecuting, TaskStatusReviewing:
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
	if _, err := runGit(path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return true
	}
	_, err := runGit(path, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

func gitCommitExists(path, commit string) bool {
	_, err := runGit(path, "rev-parse", "--verify", commit+"^{commit}")
	return err == nil
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
