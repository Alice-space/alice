package campaignrepo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	TaskStatusDraft           = "draft"
	TaskStatusReady           = "ready"
	TaskStatusExecuting       = "executing"
	TaskStatusInProgress      = TaskStatusExecuting
	TaskStatusReviewPending   = "review_pending"
	TaskStatusReviewing       = "reviewing"
	TaskStatusReview          = TaskStatusReviewing
	TaskStatusRework          = "rework"
	TaskStatusAccepted        = "accepted"
	TaskStatusBlocked         = "blocked"
	TaskStatusWaitingExternal = "waiting_external"
	TaskStatusDone            = "done"
	TaskStatusRejected        = "rejected"
)

type RoleConfig struct {
	Role            string `yaml:"role" json:"role,omitempty"`
	Provider        string `yaml:"provider" json:"provider,omitempty"`
	Model           string `yaml:"model" json:"model,omitempty"`
	Profile         string `yaml:"profile" json:"profile,omitempty"`
	Workflow        string `yaml:"workflow" json:"workflow,omitempty"`
	ReasoningEffort string `yaml:"reasoning_effort" json:"reasoning_effort,omitempty"`
	Personality     string `yaml:"personality" json:"personality,omitempty"`
}

type CampaignDocument struct {
	Path        string              `json:"path"`
	Body        string              `json:"body,omitempty"`
	Frontmatter CampaignFrontmatter `json:"frontmatter"`
}

type CampaignFrontmatter struct {
	CampaignID             string     `yaml:"campaign_id" json:"campaign_id,omitempty"`
	Title                  string     `yaml:"title" json:"title,omitempty"`
	Objective              string     `yaml:"objective" json:"objective,omitempty"`
	Status                 string     `yaml:"status" json:"status,omitempty"`
	CampaignRepoPath       string     `yaml:"campaign_repo_path" json:"campaign_repo_path,omitempty"`
	CurrentPhase           string     `yaml:"current_phase" json:"current_phase,omitempty"`
	CurrentDirection       string     `yaml:"current_direction" json:"current_direction,omitempty"`
	CurrentWinnerTask      string     `yaml:"current_winner_task" json:"current_winner_task,omitempty"`
	SourceRepos            []string   `yaml:"source_repos" json:"source_repos,omitempty"`
	ReviewMode             string     `yaml:"review_mode" json:"review_mode,omitempty"`
	ReportMode             string     `yaml:"report_mode" json:"report_mode,omitempty"`
	DefaultExecutor        RoleConfig `yaml:"default_executor" json:"default_executor,omitempty"`
	DefaultReviewer        RoleConfig `yaml:"default_reviewer" json:"default_reviewer,omitempty"`
	DefaultPlanner         RoleConfig `yaml:"default_planner" json:"default_planner,omitempty"`
	DefaultPlannerReviewer RoleConfig `yaml:"default_planner_reviewer" json:"default_planner_reviewer,omitempty"`
	PlanRound              int        `yaml:"plan_round" json:"plan_round,omitempty"`
	PlanStatus             string     `yaml:"plan_status" json:"plan_status,omitempty"`
}

type PhaseDocument struct {
	Path        string           `json:"path"`
	Body        string           `json:"body,omitempty"`
	Frontmatter PhaseFrontmatter `json:"frontmatter"`
}

type PhaseFrontmatter struct {
	Phase      string   `yaml:"phase" json:"phase,omitempty"`
	Title      string   `yaml:"title" json:"title,omitempty"`
	Status     string   `yaml:"status" json:"status,omitempty"`
	Goal       string   `yaml:"goal" json:"goal,omitempty"`
	EntryGates []string `yaml:"entry_gates" json:"entry_gates,omitempty"`
	ExitGates  []string `yaml:"exit_gates" json:"exit_gates,omitempty"`
}

type TaskDocument struct {
	Path        string          `json:"path"`
	Dir         string          `json:"dir"`
	Body        string          `json:"body,omitempty"`
	Frontmatter TaskFrontmatter `json:"frontmatter"`
	LeaseUntil  time.Time       `json:"lease_until,omitempty"`
	WakeAt      time.Time       `json:"wake_at,omitempty"`
}

type TaskFrontmatter struct {
	TaskID            string     `yaml:"task_id" json:"task_id,omitempty"`
	Title             string     `yaml:"title" json:"title,omitempty"`
	Phase             string     `yaml:"phase" json:"phase,omitempty"`
	Status            string     `yaml:"status" json:"status,omitempty"`
	DependsOn         []string   `yaml:"depends_on" json:"depends_on,omitempty"`
	TargetRepos       []string   `yaml:"target_repos" json:"target_repos,omitempty"`
	WorkingBranches   []string   `yaml:"working_branches" json:"working_branches,omitempty"`
	WriteScope        []string   `yaml:"write_scope" json:"write_scope,omitempty"`
	OwnerAgent        string     `yaml:"owner_agent" json:"owner_agent,omitempty"`
	LeaseUntilRaw     string     `yaml:"lease_until" json:"lease_until,omitempty"`
	Executor          RoleConfig `yaml:"executor" json:"executor,omitempty"`
	Reviewer          RoleConfig `yaml:"reviewer" json:"reviewer,omitempty"`
	DispatchState     string     `yaml:"dispatch_state" json:"dispatch_state,omitempty"`
	ReviewStatus      string     `yaml:"review_status" json:"review_status,omitempty"`
	ExecutionRound    int        `yaml:"execution_round" json:"execution_round,omitempty"`
	ReviewRound       int        `yaml:"review_round" json:"review_round,omitempty"`
	BaseCommit        string     `yaml:"base_commit" json:"base_commit,omitempty"`
	HeadCommit        string     `yaml:"head_commit" json:"head_commit,omitempty"`
	LastRunPath       string     `yaml:"last_run_path" json:"last_run_path,omitempty"`
	LastReviewPath    string     `yaml:"last_review_path" json:"last_review_path,omitempty"`
	WakeAtRaw         string     `yaml:"wake_at" json:"wake_at,omitempty"`
	WakePrompt        string     `yaml:"wake_prompt" json:"wake_prompt,omitempty"`
	ReportSnippetPath string     `yaml:"report_snippet_path" json:"report_snippet_path,omitempty"`
	Artifacts         []string   `yaml:"artifacts" json:"artifacts,omitempty"`
	ResultPaths       []string   `yaml:"result_paths" json:"result_paths,omitempty"`
}

type ReviewDocument struct {
	Path        string            `json:"path"`
	Body        string            `json:"body,omitempty"`
	Frontmatter ReviewFrontmatter `json:"frontmatter"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
}

type ReviewFrontmatter struct {
	ReviewID     string     `yaml:"review_id" json:"review_id,omitempty"`
	TargetTask   string     `yaml:"target_task" json:"target_task,omitempty"`
	ReviewRound  int        `yaml:"review_round" json:"review_round,omitempty"`
	Reviewer     RoleConfig `yaml:"reviewer" json:"reviewer,omitempty"`
	Verdict      string     `yaml:"verdict" json:"verdict,omitempty"`
	Blocking     bool       `yaml:"blocking" json:"blocking,omitempty"`
	TargetCommit string     `yaml:"target_commit" json:"target_commit,omitempty"`
	CreatedAtRaw string     `yaml:"created_at" json:"created_at,omitempty"`
}

type PlanProposalDocument struct {
	Path        string                  `json:"path"`
	Body        string                  `json:"body,omitempty"`
	Frontmatter PlanProposalFrontmatter `json:"frontmatter"`
}

type PlanProposalFrontmatter struct {
	ProposalID string `yaml:"proposal_id" json:"proposal_id,omitempty"`
	PlanRound  int    `yaml:"plan_round" json:"plan_round,omitempty"`
	Status     string `yaml:"status" json:"status,omitempty"`
}

type PlanReviewDocument struct {
	Path        string                `json:"path"`
	Body        string                `json:"body,omitempty"`
	Frontmatter PlanReviewFrontmatter `json:"frontmatter"`
	CreatedAt   time.Time             `json:"created_at,omitempty"`
}

type PlanReviewFrontmatter struct {
	ReviewID     string     `yaml:"review_id" json:"review_id,omitempty"`
	PlanRound    int        `yaml:"plan_round" json:"plan_round,omitempty"`
	Reviewer     RoleConfig `yaml:"reviewer" json:"reviewer,omitempty"`
	Verdict      string     `yaml:"verdict" json:"verdict,omitempty"`
	Blocking     bool       `yaml:"blocking" json:"blocking,omitempty"`
	CreatedAtRaw string     `yaml:"created_at" json:"created_at,omitempty"`
}

type Repository struct {
	Root          string                 `json:"root"`
	Campaign      CampaignDocument       `json:"campaign"`
	Phases        []PhaseDocument        `json:"phases,omitempty"`
	Tasks         []TaskDocument         `json:"tasks,omitempty"`
	Reviews       []ReviewDocument       `json:"reviews,omitempty"`
	PlanProposals []PlanProposalDocument `json:"plan_proposals,omitempty"`
	PlanReviews   []PlanReviewDocument   `json:"plan_reviews,omitempty"`
}

type TaskSummary struct {
	TaskID         string    `json:"task_id"`
	Title          string    `json:"title,omitempty"`
	Phase          string    `json:"phase,omitempty"`
	Status         string    `json:"status"`
	Path           string    `json:"path"`
	OwnerAgent     string    `json:"owner_agent,omitempty"`
	LeaseUntil     time.Time `json:"lease_until,omitempty"`
	WakeAt         time.Time `json:"wake_at,omitempty"`
	WakePrompt     string    `json:"wake_prompt,omitempty"`
	BlockedReason  string    `json:"blocked_reason,omitempty"`
	DependsOn      []string  `json:"depends_on,omitempty"`
	TargetRepos    []string  `json:"target_repos,omitempty"`
	WriteScope     []string  `json:"write_scope,omitempty"`
	DispatchState  string    `json:"dispatch_state,omitempty"`
	ReviewStatus   string    `json:"review_status,omitempty"`
	ExecutionRound int       `json:"execution_round,omitempty"`
	ReviewRound    int       `json:"review_round,omitempty"`
	HeadCommit     string    `json:"head_commit,omitempty"`
	LastReviewPath string    `json:"last_review_path,omitempty"`
}

type WakeTaskSpec struct {
	StateKey string    `json:"state_key"`
	TaskID   string    `json:"task_id"`
	Title    string    `json:"title"`
	TaskPath string    `json:"task_path"`
	RunAt    time.Time `json:"run_at"`
	Prompt   string    `json:"prompt"`
}

type Summary struct {
	Root                string         `json:"root"`
	CampaignID          string         `json:"campaign_id,omitempty"`
	CampaignTitle       string         `json:"campaign_title,omitempty"`
	CurrentPhase        string         `json:"current_phase,omitempty"`
	PlanRound           int            `json:"plan_round,omitempty"`
	PlanStatus          string         `json:"plan_status,omitempty"`
	MaxParallel         int            `json:"max_parallel"`
	TaskCount           int            `json:"task_count"`
	DraftCount          int            `json:"draft_count"`
	ReadyCount          int            `json:"ready_count"`
	ReworkCount         int            `json:"rework_count"`
	SelectedReadyCount  int            `json:"selected_ready_count"`
	ActiveCount         int            `json:"active_count"`
	ExecutingCount      int            `json:"executing_count"`
	ReviewCount         int            `json:"review_count"`
	ReviewPendingCount  int            `json:"review_pending_count"`
	ReviewingCount      int            `json:"reviewing_count"`
	SelectedReviewCount int            `json:"selected_review_count"`
	AcceptedCount       int            `json:"accepted_count"`
	BlockedCount        int            `json:"blocked_count"`
	WaitingCount        int            `json:"waiting_count"`
	DoneCount           int            `json:"done_count"`
	RejectedCount       int            `json:"rejected_count"`
	GeneratedAt         time.Time      `json:"generated_at"`
	ActiveTasks         []TaskSummary  `json:"active_tasks,omitempty"`
	ReadyTasks          []TaskSummary  `json:"ready_tasks,omitempty"`
	SelectedReady       []TaskSummary  `json:"selected_ready,omitempty"`
	ReviewPendingTasks  []TaskSummary  `json:"review_pending_tasks,omitempty"`
	SelectedReview      []TaskSummary  `json:"selected_review,omitempty"`
	AcceptedTasks       []TaskSummary  `json:"accepted_tasks,omitempty"`
	BlockedTasks        []TaskSummary  `json:"blocked_tasks,omitempty"`
	WakePending         []TaskSummary  `json:"wake_pending,omitempty"`
	WakeDue             []TaskSummary  `json:"wake_due,omitempty"`
	WakeTasks           []WakeTaskSpec `json:"wake_tasks,omitempty"`
	PhaseCounts         map[string]int `json:"phase_counts,omitempty"`
}

func Load(root string) (Repository, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Repository{}, errors.New("campaign repo path is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Repository{}, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return Repository{}, err
	}
	if !info.IsDir() {
		return Repository{}, fmt.Errorf("campaign repo path is not a directory: %s", absRoot)
	}

	repo := Repository{Root: absRoot}
	repo.Campaign, err = loadCampaignDocument(filepath.Join(absRoot, "campaign.md"), absRoot)
	if err != nil {
		return Repository{}, err
	}
	if repo.Campaign.Frontmatter.CampaignRepoPath == "" {
		repo.Campaign.Frontmatter.CampaignRepoPath = absRoot
	}
	repo.Phases, err = loadPhaseDocuments(absRoot)
	if err != nil {
		return Repository{}, err
	}
	repo.Tasks, err = loadTaskDocuments(absRoot)
	if err != nil {
		return Repository{}, err
	}
	repo.Reviews, err = loadReviewDocuments(absRoot)
	if err != nil {
		return Repository{}, err
	}
	repo.PlanProposals, err = loadPlanProposalDocuments(absRoot)
	if err != nil {
		return Repository{}, err
	}
	repo.PlanReviews, err = loadPlanReviewDocuments(absRoot)
	if err != nil {
		return Repository{}, err
	}
	return repo, nil
}

func ScanFromPath(root string, now time.Time, maxParallel int) (Repository, Summary, error) {
	repo, err := Load(root)
	if err != nil {
		return Repository{}, Summary{}, err
	}
	return repo, Summarize(repo, now, maxParallel), nil
}
