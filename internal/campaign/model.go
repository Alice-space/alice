package campaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/sessionkey"
	"github.com/Alice-space/alice/internal/storeutil"
)

type ManageMode string

const (
	ManageModeCreatorOnly ManageMode = "creator_only"
	ManageModeScopeAll    ManageMode = "scope_all"
)

type CampaignStatus string

const (
	StatusPlanned           CampaignStatus = "planned"
	StatusPlanning          CampaignStatus = "planning"
	StatusPlanReviewPending CampaignStatus = "plan_review_pending"
	StatusPlanApproved      CampaignStatus = "plan_approved"
	StatusRunning           CampaignStatus = "running"
	StatusHold              CampaignStatus = "hold"
	StatusMerged            CampaignStatus = "merged"
	StatusRejected          CampaignStatus = "rejected"
	StatusCompleted         CampaignStatus = "completed"
	StatusCanceled          CampaignStatus = "canceled"
)

type TrialStatus string

const (
	TrialStatusPlanned   TrialStatus = "planned"
	TrialStatusRunning   TrialStatus = "running"
	TrialStatusCandidate TrialStatus = "candidate"
	TrialStatusHold      TrialStatus = "hold"
	TrialStatusCompleted TrialStatus = "completed"
	TrialStatusMerged    TrialStatus = "merged"
	TrialStatusRejected  TrialStatus = "rejected"
	TrialStatusAborted   TrialStatus = "aborted"
)

type Verdict string

const (
	VerdictMerge             Verdict = "merge"
	VerdictReject            Verdict = "reject"
	VerdictHold              Verdict = "hold"
	VerdictNeedsMoreEvidence Verdict = "needs-more-evidence"
	VerdictAborted           Verdict = "aborted"
	VerdictApprove           Verdict = "approve"
	VerdictConcern           Verdict = "concern"
	VerdictBlocking          Verdict = "blocking"
)

type SessionRoute struct {
	ScopeKey      string `json:"scope_key"`
	ReceiveIDType string `json:"receive_id_type,omitempty"`
	ReceiveID     string `json:"receive_id,omitempty"`
	ChatType      string `json:"chat_type,omitempty"`
}

func (r SessionRoute) VisibilityKey() string {
	if key := sessionkey.Build(strings.ToLower(strings.TrimSpace(r.ReceiveIDType)), r.ReceiveID); key != "" {
		return key
	}
	return sessionkey.VisibilityKey(r.ScopeKey)
}

type Actor struct {
	UserID string `json:"user_id,omitempty"`
	OpenID string `json:"open_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

func (a Actor) PreferredID() string {
	if id := strings.TrimSpace(a.UserID); id != "" {
		return id
	}
	return strings.TrimSpace(a.OpenID)
}

type Metric struct {
	Name    string  `json:"name"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit,omitempty"`
	Context string  `json:"context,omitempty"`
}

type Gate struct {
	Metric   string  `json:"metric"`
	Operator string  `json:"operator"`
	Target   float64 `json:"target"`
	Unit     string  `json:"unit,omitempty"`
	Context  string  `json:"context,omitempty"`
}

type Trial struct {
	ID          string      `json:"id"`
	Title       string      `json:"title,omitempty"`
	Hypothesis  string      `json:"hypothesis,omitempty"`
	Branch      string      `json:"branch,omitempty"`
	MergeReq    string      `json:"merge_request,omitempty"`
	Executor    string      `json:"executor,omitempty"`
	Resource    string      `json:"resource,omitempty"`
	JobID       string      `json:"job_id,omitempty"`
	Status      TrialStatus `json:"status"`
	Verdict     Verdict     `json:"verdict,omitempty"`
	Summary     string      `json:"summary,omitempty"`
	Metrics     []Metric    `json:"metrics,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	CreatedAt   time.Time   `json:"created_at,omitempty"`
	UpdatedAt   time.Time   `json:"updated_at,omitempty"`
	CompletedAt time.Time   `json:"completed_at,omitempty"`
}

type Guidance struct {
	ID        string    `json:"id"`
	Source    string    `json:"source,omitempty"`
	Command   string    `json:"command,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Applied   bool      `json:"applied,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type ReviewFinding struct {
	Category   string `json:"category,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

type Review struct {
	ID         string          `json:"id"`
	ReviewerID string          `json:"reviewer_id,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	Model      string          `json:"model,omitempty"`
	Verdict    Verdict         `json:"verdict,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	Blocking   bool            `json:"blocking,omitempty"`
	Confidence string          `json:"confidence,omitempty"`
	Findings   []ReviewFinding `json:"findings,omitempty"`
	CreatedAt  time.Time       `json:"created_at,omitempty"`
}

type Pitfall struct {
	ID             string    `json:"id"`
	Summary        string    `json:"summary,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	RelatedTrialID string    `json:"related_trial_id,omitempty"`
	RetryIf        string    `json:"retry_if,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
}

type Campaign struct {
	ID                   string         `json:"id"`
	Title                string         `json:"title,omitempty"`
	Objective            string         `json:"objective"`
	Repo                 string         `json:"repo,omitempty"`
	CampaignRepoPath     string         `json:"campaign_repo_path,omitempty"`
	IssueIID             string         `json:"issue_iid,omitempty"`
	IssueURL             string         `json:"issue_url,omitempty"`
	Session              SessionRoute   `json:"session"`
	Creator              Actor          `json:"creator"`
	ManageMode           ManageMode     `json:"manage_mode"`
	Status               CampaignStatus `json:"status"`
	MaxParallelTrials    int            `json:"max_parallel_trials,omitempty"`
	Summary              string         `json:"summary,omitempty"`
	CurrentWinnerTrialID string         `json:"current_winner_trial_id,omitempty"`
	Baseline             []Metric       `json:"baseline,omitempty"`
	Gates                []Gate         `json:"gates,omitempty"`
	Trials               []Trial        `json:"trials,omitempty"`
	Guidance             []Guidance     `json:"guidance,omitempty"`
	Reviews              []Review       `json:"reviews,omitempty"`
	Pitfalls             []Pitfall      `json:"pitfalls,omitempty"`
	Tags                 []string       `json:"tags,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	Revision             int64          `json:"revision"`
}

type Snapshot struct {
	Version   int        `json:"version"`
	Campaigns []Campaign `json:"campaigns"`
}

func NormalizeCampaign(c Campaign) Campaign {
	c.ID = strings.TrimSpace(c.ID)
	c.Title = strings.TrimSpace(c.Title)
	c.Objective = strings.TrimSpace(c.Objective)
	c.Repo = strings.TrimSpace(c.Repo)
	c.CampaignRepoPath = strings.TrimSpace(c.CampaignRepoPath)
	c.IssueIID = strings.TrimSpace(c.IssueIID)
	c.IssueURL = strings.TrimSpace(c.IssueURL)
	c.Session.ScopeKey = strings.TrimSpace(c.Session.ScopeKey)
	c.Session.ReceiveIDType = strings.TrimSpace(c.Session.ReceiveIDType)
	c.Session.ReceiveID = strings.TrimSpace(c.Session.ReceiveID)
	c.Session.ChatType = strings.ToLower(strings.TrimSpace(c.Session.ChatType))
	c.Creator.UserID = strings.TrimSpace(c.Creator.UserID)
	c.Creator.OpenID = strings.TrimSpace(c.Creator.OpenID)
	c.Creator.Name = strings.TrimSpace(c.Creator.Name)
	c.ManageMode = ManageMode(strings.ToLower(strings.TrimSpace(string(c.ManageMode))))
	c.Status = CampaignStatus(strings.ToLower(strings.TrimSpace(string(c.Status))))
	c.Summary = strings.TrimSpace(c.Summary)
	c.CurrentWinnerTrialID = strings.TrimSpace(c.CurrentWinnerTrialID)
	c.Tags = storeutil.UniqueNonEmptyStrings(c.Tags)
	if c.ManageMode == "" {
		c.ManageMode = ManageModeCreatorOnly
	}
	if c.Status == "" {
		c.Status = StatusPlanned
	}
	if c.MaxParallelTrials <= 0 {
		c.MaxParallelTrials = 1
	}
	c.Baseline = normalizeMetrics(c.Baseline)
	c.Gates = normalizeGates(c.Gates)
	c.Trials = normalizeTrials(c.Trials)
	c.Guidance = normalizeGuidanceEntries(c.Guidance)
	c.Reviews = normalizeReviews(c.Reviews)
	c.Pitfalls = normalizePitfalls(c.Pitfalls)
	return c
}

func ValidateCampaign(c Campaign) error {
	c = NormalizeCampaign(c)
	if c.ID == "" {
		return errors.New("campaign id is empty")
	}
	if c.Session.ScopeKey == "" {
		return errors.New("campaign session scope key is empty")
	}
	if c.Creator.PreferredID() == "" {
		return errors.New("campaign creator is empty")
	}
	if c.Objective == "" {
		return errors.New("campaign objective is empty")
	}
	if c.ManageMode != ManageModeCreatorOnly && c.ManageMode != ManageModeScopeAll {
		return fmt.Errorf("invalid manage mode %q", c.ManageMode)
	}
	switch c.Status {
	case StatusPlanned, StatusRunning, StatusHold, StatusMerged, StatusRejected, StatusCompleted, StatusCanceled:
	default:
		return fmt.Errorf("invalid campaign status %q", c.Status)
	}
	if c.MaxParallelTrials <= 0 {
		return errors.New("max_parallel_trials must be > 0")
	}
	if c.MaxParallelTrials > 32 {
		return errors.New("max_parallel_trials must be <= 32")
	}
	seenTrials := make(map[string]struct{}, len(c.Trials))
	for _, trial := range c.Trials {
		if err := validateTrial(trial); err != nil {
			return err
		}
		if _, exists := seenTrials[trial.ID]; exists {
			return fmt.Errorf("duplicate trial id %q", trial.ID)
		}
		seenTrials[trial.ID] = struct{}{}
	}
	if c.CurrentWinnerTrialID != "" {
		if _, exists := seenTrials[c.CurrentWinnerTrialID]; !exists {
			return fmt.Errorf("current winner trial %q not found", c.CurrentWinnerTrialID)
		}
	}
	return nil
}

func normalizeMetrics(values []Metric) []Metric {
	if len(values) == 0 {
		return nil
	}
	out := make([]Metric, 0, len(values))
	for _, value := range values {
		value.Name = strings.TrimSpace(value.Name)
		value.Unit = strings.TrimSpace(value.Unit)
		value.Context = strings.TrimSpace(value.Context)
		if value.Name == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func normalizeGates(values []Gate) []Gate {
	if len(values) == 0 {
		return nil
	}
	out := make([]Gate, 0, len(values))
	for _, value := range values {
		value.Metric = strings.TrimSpace(value.Metric)
		value.Operator = strings.TrimSpace(value.Operator)
		value.Unit = strings.TrimSpace(value.Unit)
		value.Context = strings.TrimSpace(value.Context)
		if value.Metric == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func normalizeTrials(values []Trial) []Trial {
	if len(values) == 0 {
		return nil
	}
	out := make([]Trial, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Title = strings.TrimSpace(value.Title)
		value.Hypothesis = strings.TrimSpace(value.Hypothesis)
		value.Branch = strings.TrimSpace(value.Branch)
		value.MergeReq = strings.TrimSpace(value.MergeReq)
		value.Executor = strings.TrimSpace(value.Executor)
		value.Resource = strings.TrimSpace(value.Resource)
		value.JobID = strings.TrimSpace(value.JobID)
		value.Status = TrialStatus(strings.ToLower(strings.TrimSpace(string(value.Status))))
		value.Verdict = Verdict(strings.ToLower(strings.TrimSpace(string(value.Verdict))))
		value.Summary = strings.TrimSpace(value.Summary)
		value.Tags = storeutil.UniqueNonEmptyStrings(value.Tags)
		value.Metrics = normalizeMetrics(value.Metrics)
		if value.Status == "" {
			value.Status = TrialStatusPlanned
		}
		if value.ID == "" {
			continue
		}
		if _, exists := seen[value.ID]; exists {
			continue
		}
		seen[value.ID] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeGuidanceEntries(values []Guidance) []Guidance {
	if len(values) == 0 {
		return nil
	}
	out := make([]Guidance, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Source = strings.TrimSpace(value.Source)
		value.Command = strings.TrimSpace(value.Command)
		value.Summary = strings.TrimSpace(value.Summary)
		if value.ID == "" {
			continue
		}
		if _, exists := seen[value.ID]; exists {
			continue
		}
		seen[value.ID] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeReviews(values []Review) []Review {
	if len(values) == 0 {
		return nil
	}
	out := make([]Review, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.ReviewerID = strings.TrimSpace(value.ReviewerID)
		value.Provider = strings.TrimSpace(value.Provider)
		value.Model = strings.TrimSpace(value.Model)
		value.Verdict = Verdict(strings.ToLower(strings.TrimSpace(string(value.Verdict))))
		value.Summary = strings.TrimSpace(value.Summary)
		value.Confidence = strings.TrimSpace(value.Confidence)
		value.Findings = normalizeFindings(value.Findings)
		if value.ID == "" {
			continue
		}
		if _, exists := seen[value.ID]; exists {
			continue
		}
		seen[value.ID] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeFindings(values []ReviewFinding) []ReviewFinding {
	if len(values) == 0 {
		return nil
	}
	out := make([]ReviewFinding, 0, len(values))
	for _, value := range values {
		value.Category = strings.TrimSpace(value.Category)
		value.Severity = strings.TrimSpace(value.Severity)
		value.Summary = strings.TrimSpace(value.Summary)
		value.Evidence = strings.TrimSpace(value.Evidence)
		value.Suggestion = strings.TrimSpace(value.Suggestion)
		if value.Category == "" && value.Summary == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func normalizePitfalls(values []Pitfall) []Pitfall {
	if len(values) == 0 {
		return nil
	}
	out := make([]Pitfall, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Summary = strings.TrimSpace(value.Summary)
		value.Reason = strings.TrimSpace(value.Reason)
		value.RelatedTrialID = strings.TrimSpace(value.RelatedTrialID)
		value.RetryIf = strings.TrimSpace(value.RetryIf)
		value.Tags = storeutil.UniqueNonEmptyStrings(value.Tags)
		if value.ID == "" {
			continue
		}
		if _, exists := seen[value.ID]; exists {
			continue
		}
		seen[value.ID] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validateTrial(t Trial) error {
	t = normalizeTrials([]Trial{t})[0]
	if t.ID == "" {
		return errors.New("trial id is empty")
	}
	switch t.Status {
	case TrialStatusPlanned, TrialStatusRunning, TrialStatusCandidate, TrialStatusHold, TrialStatusCompleted, TrialStatusMerged, TrialStatusRejected, TrialStatusAborted:
	default:
		return fmt.Errorf("invalid trial status %q", t.Status)
	}
	return nil
}
