package domain

import "time"

// RunStatus describes liveness of a run.
type RunStatus string

const (
	RunStatusQueued       RunStatus = "Queued"
	RunStatusRunning      RunStatus = "Running"
	RunStatusWaitingHuman RunStatus = "WaitingHuman"
	RunStatusBlocked      RunStatus = "Blocked"
	RunStatusSucceeded    RunStatus = "Succeeded"
	RunStatusFailed       RunStatus = "Failed"
	RunStatusAborted      RunStatus = "Aborted"
	RunStatusSuperseded   RunStatus = "Superseded"
)

func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusSucceeded, RunStatusFailed, RunStatusAborted, RunStatusSuperseded:
		return true
	default:
		return false
	}
}

// WorkflowPhase tracks workflow progression.
type WorkflowPhase string

const (
	WorkflowPhasePlanning      WorkflowPhase = "Planning"
	WorkflowPhaseImplementing  WorkflowPhase = "Implementing"
	WorkflowPhaseExperimenting WorkflowPhase = "Experimenting"
	WorkflowPhaseEvaluating    WorkflowPhase = "Evaluating"
	WorkflowPhaseReporting     WorkflowPhase = "Reporting"
	WorkflowPhaseWaitingHuman  WorkflowPhase = "WaitingHuman"
	WorkflowPhaseBlocked       WorkflowPhase = "Blocked"
	WorkflowPhaseDone          WorkflowPhase = "Done"
	WorkflowPhaseAborted       WorkflowPhase = "Aborted"
)

func (p WorkflowPhase) IsTerminal() bool {
	return p == WorkflowPhaseDone || p == WorkflowPhaseAborted
}

type TaskType string

const (
	TaskTypeQuery          TaskType = "query"
	TaskTypeCodeChange     TaskType = "code_change"
	TaskTypeExperiment     TaskType = "experiment"
	TaskTypeEvaluation     TaskType = "evaluation"
	TaskTypeScheduledWatch TaskType = "scheduled_watch"
	TaskTypeReview         TaskType = "review"
	TaskTypeReport         TaskType = "report"
	TaskTypeMaintenance    TaskType = "maintenance"
)

type TriggerType string

const (
	TriggerUser         TriggerType = "user"
	TriggerScheduler    TriggerType = "scheduler"
	TriggerWebhook      TriggerType = "webhook"
	TriggerEvalRetry    TriggerType = "eval_retry"
	TriggerManualSignal TriggerType = "manual_signal"
	TriggerRecovery     TriggerType = "recovery"
)

type WriteScope string

const (
	WriteScopeNone       WriteScope = "none"
	WriteScopeRepoBranch WriteScope = "repo_branch"
	WriteScopeProject    WriteScope = "project"
	WriteScopeSettings   WriteScope = "settings"
)

type RunMode string

const (
	RunModeFast       RunMode = "fast"
	RunModeWorkflow   RunMode = "workflow"
	RunModeEvaluation RunMode = "evaluation"
	RunModeMaintenance RunMode = "maintenance"
)

type IntentKind string

const (
	IntentKindTaskRequest      IntentKind = "task_request"
	IntentKindSettingChange    IntentKind = "setting_change"
	IntentKindStateQuery       IntentKind = "state_query"
	IntentKindApprovalResponse IntentKind = "approval_response"
	IntentKindManualSignal     IntentKind = "manual_signal"
)

type IntentScope string

const (
	IntentScopeGlobal  IntentScope = "global"
	IntentScopeProject IntentScope = "project"
	IntentScopeRun     IntentScope = "run"
)

type RuntimeSettingClass string

const (
	RuntimeSettingSafe      RuntimeSettingClass = "safe"
	RuntimeSettingGuarded   RuntimeSettingClass = "guarded"
	RuntimeSettingImmutable RuntimeSettingClass = "immutable"
)

type RuntimeSettingStatus string

const (
	RuntimeSettingDraft      RuntimeSettingStatus = "draft"
	RuntimeSettingActive     RuntimeSettingStatus = "active"
	RuntimeSettingRejected   RuntimeSettingStatus = "rejected"
	RuntimeSettingSuperseded RuntimeSettingStatus = "superseded"
)

type ConfigProposalKind string

const (
	ConfigProposalStaticConfigPatch   ConfigProposalKind = "static_config_patch"
	ConfigProposalSecretRotation      ConfigProposalKind = "secret_rotation_request"
	ConfigProposalIntegrationRebind   ConfigProposalKind = "integration_rebind"
)

type ConfigProposalStatus string

const (
	ConfigProposalDraft       ConfigProposalStatus = "draft"
	ConfigProposalWaitingHuman ConfigProposalStatus = "waiting_human"
	ConfigProposalApproved    ConfigProposalStatus = "approved"
	ConfigProposalRejected    ConfigProposalStatus = "rejected"
	ConfigProposalApplied     ConfigProposalStatus = "applied"
)

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

type ScheduleStatus string

const (
	ScheduleActive   ScheduleStatus = "active"
	SchedulePaused   ScheduleStatus = "paused"
	ScheduleArchived ScheduleStatus = "archived"
)

type MemoryScope string

const (
	MemoryScopeGlobal  MemoryScope = "global"
	MemoryScopeProject MemoryScope = "project"
	MemoryScopeTask    MemoryScope = "task"
	MemoryScopeRun     MemoryScope = "run"
)

type MemoryType string

const (
	MemoryTypePreference         MemoryType = "preference"
	MemoryTypeFact               MemoryType = "fact"
	MemoryTypeDecision           MemoryType = "decision"
	MemoryTypeFailurePattern     MemoryType = "failure_pattern"
	MemoryTypePlaybook           MemoryType = "playbook"
	MemoryTypeEvaluationBaseline MemoryType = "evaluation_baseline"
)

type Freshness string

const (
	FreshnessHot  Freshness = "hot"
	FreshnessWarm Freshness = "warm"
	FreshnessCold Freshness = "cold"
	FreshnessStale Freshness = "stale"
)

type EventSeverity string

const (
	SeverityDebug EventSeverity = "DEBUG"
	SeverityInfo  EventSeverity = "INFO"
	SeverityWarn  EventSeverity = "WARN"
	SeverityError EventSeverity = "ERROR"
	SeverityAlert EventSeverity = "ALERT"
)

type EventType string

const (
	EventTaskReceived          EventType = "TaskReceived"
	EventTaskClassified        EventType = "TaskClassified"
	EventTaskEscalated         EventType = "TaskEscalated"
	EventRunStarted            EventType = "RunStarted"
	EventRunStatusChanged      EventType = "RunStatusChanged"
	EventWorkflowPhaseChanged  EventType = "WorkflowPhaseChanged"
	EventRunSuperseded         EventType = "RunSuperseded"
	EventEvalRequested         EventType = "EvalRequested"
	EventEvalCompleted         EventType = "EvalCompleted"
	EventEvalFailed            EventType = "EvalFailed"
	EventApprovalRequested     EventType = "ApprovalRequested"
	EventApprovalResolved      EventType = "ApprovalResolved"
	EventSettingChangeRequested EventType = "SettingChangeRequested"
	EventSettingApplied        EventType = "SettingApplied"
	EventConfigChangeProposed  EventType = "ConfigChangeProposed"
	EventScheduleTriggered     EventType = "ScheduleTriggered"
	EventMemoryPromoted        EventType = "MemoryPromoted"
	EventHumanSignalReceived   EventType = "HumanSignalReceived"
	EventBudgetExceeded        EventType = "BudgetExceeded"
	EventIntegrationFailed     EventType = "IntegrationFailed"
	EventRunAborted            EventType = "RunAborted"
)

type FailureSource string

const (
	FailureSourceModelProvider       FailureSource = "model_provider_failure"
	FailureSourceExecutor            FailureSource = "executor_failure"
	FailureSourceIntegration         FailureSource = "integration_failure"
	FailureSourcePolicyBlocked       FailureSource = "policy_blocked"
	FailureSourceResourceExhausted   FailureSource = "resource_exhausted"
	FailureSourceDataInconsistent    FailureSource = "data_or_artifact_inconsistent"
	FailureSourceTerminalLogic       FailureSource = "terminal_logic_failure"
)

type FailureSemantic string

const (
	FailureSemanticRetryable    FailureSemantic = "retryable"
	FailureSemanticDegradable   FailureSemantic = "degradable"
	FailureSemanticHumanRequired FailureSemantic = "human_required"
	FailureSemanticTerminal     FailureSemantic = "terminal"
)

type LockType string

const (
	LockTypeProject         LockType = "project_lock"
	LockTypeRepoBranchWrite LockType = "repo_branch_write_lock"
	LockTypeWorkspace       LockType = "workspace_lock"
	LockTypeExecutorSlot    LockType = "executor_slot_lock"
	LockTypeMemoryPromotion LockType = "memory_promotion_lock"
	LockTypeScheduleInstance LockType = "schedule_instance_lock"
)

type LockStatus string

const (
	LockStatusHeld     LockStatus = "held"
	LockStatusReleased LockStatus = "released"
	LockStatusExpired  LockStatus = "expired"
)

type RoutingPath string

const (
	RoutingPathFast   RoutingPath = "fast path"
	RoutingPathTask   RoutingPath = "task path"
	RoutingPathReject RoutingPath = "reject"
)

type WorkspaceStrategy string

const (
	WorkspaceStrategyShared        WorkspaceStrategy = "shared"
	WorkspaceStrategyWorktree      WorkspaceStrategy = "worktree"
	WorkspaceStrategyEphemeralCopy WorkspaceStrategy = "ephemeral_copy"
)

type ProjectSpec struct {
	ProjectID              string                `json:"project_id"`
	Name                   string                `json:"name"`
	Goal                   string                `json:"goal"`
	Repositories           []RepositorySpec      `json:"repositories"`
	DefaultBranch          string                `json:"default_branch"`
	ExecutionTargets       []string              `json:"execution_targets"`
	BudgetPolicy           BudgetPolicy          `json:"budget_policy"`
	NotificationPolicy     NotificationPolicy    `json:"notification_policy"`
	ApprovalPolicy         ApprovalPolicy        `json:"approval_policy"`
	ArtifactPolicy         ArtifactPolicy        `json:"artifact_policy"`
	WorkspacePolicy        WorkspacePolicy       `json:"workspace_policy"`
	MemoryPolicy           MemoryPolicy          `json:"memory_policy"`
	EvaluationPolicy       EvaluationPolicy      `json:"evaluation_policy"`
	MutableSettingsPolicy  MutableSettingsPolicy `json:"mutable_settings_policy"`
	CreatedAt              time.Time             `json:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
}

type BudgetPolicy struct {
	TokenSoftLimit    int64         `json:"token_soft_limit"`
	TokenHardLimit    int64         `json:"token_hard_limit"`
	GPUMinuteLimit    int64         `json:"gpu_minute_limit"`
	MaxRuntime        time.Duration `json:"max_runtime"`
	MaxRetries        int           `json:"max_retries"`
	AllowManualBypass bool          `json:"allow_manual_bypass"`
}

type NotificationPolicy struct {
	ProgressChannels []string      `json:"progress_channels"`
	AlertChannels    []string      `json:"alert_channels"`
	ProgressInterval time.Duration `json:"progress_interval"`
	UseMentions      bool          `json:"use_mentions"`
}

type ApprovalPolicy struct {
	RequiredActions []string      `json:"required_actions"`
	TicketTTL       time.Duration `json:"ticket_ttl"`
}

type ArtifactPolicy struct {
	RootDir          string `json:"root_dir"`
	RetentionDays    int    `json:"retention_days"`
	KeepFailureState bool   `json:"keep_failure_state"`
}

type WorkspacePolicy struct {
	AllowedRoots         []string `json:"allowed_roots"`
	ForceIsolation       bool     `json:"force_isolation"`
	CleanupOnSuccess     bool     `json:"cleanup_on_success"`
	RequireWriteWorktree bool     `json:"require_write_worktree"`
}

type MemoryPolicy struct {
	ProjectCapacity     int           `json:"project_capacity"`
	GlobalCapacity      int           `json:"global_capacity"`
	TTL                 time.Duration `json:"ttl"`
	AllowCrossProject   bool          `json:"allow_cross_project"`
	RunMemoryAutoDecay  bool          `json:"run_memory_auto_decay"`
}

type EvaluationPolicy struct {
	RequirePRGate       bool     `json:"require_pr_gate"`
	GoldenBaselineID    string   `json:"golden_baseline_id"`
	DefaultDatasetRef   string   `json:"default_dataset_ref"`
	DefaultBenchRef     string   `json:"default_benchmark_suite_ref"`
	DefaultEnvRef       string   `json:"default_environment_ref"`
	RequiredShards      []string `json:"required_shards"`
	AllowNeutralOnInfra bool     `json:"allow_neutral_on_infra"`
}

type MutableSettingsPolicy struct {
	SafeKeys    []string `json:"safe_keys"`
	GuardedKeys []string `json:"guarded_keys"`
	ImmutableKeys []string `json:"immutable_keys"`
}

type RepositorySpec struct {
	RepositoryID      string            `json:"repository_id"`
	Name              string            `json:"name"`
	Root              string            `json:"root"`
	RemoteURL         string            `json:"remote_url"`
	DefaultBranch     string            `json:"default_branch"`
	WorkspaceStrategy WorkspaceStrategy `json:"workspace_strategy"`
	ProtectedBranches []string          `json:"protected_branches"`
}

type TaskTemplate struct {
	TaskTemplateID          string          `json:"task_template_id"`
	ProjectID               string          `json:"project_id"`
	TaskType                TaskType        `json:"task_type"`
	GoalTemplate            string          `json:"goal_template"`
	DefaultWriteScope       WriteScope      `json:"default_write_scope"`
	DefaultAcceptancePolicy AcceptancePolicy `json:"default_acceptance_policy"`
	DefaultTargetExecutor   string          `json:"default_target_executor"`
}

type TaskSpec struct {
	TaskID            string           `json:"task_id"`
	ProjectID         string           `json:"project_id"`
	TaskType          TaskType         `json:"task_type"`
	Priority          int              `json:"priority"`
	Goal              string           `json:"goal"`
	Dependencies      []string         `json:"dependencies"`
	ExpectedOutputs   []string         `json:"expected_outputs"`
	AllowedModels     []string         `json:"allowed_models"`
	ResourceLimits    ResourceLimits   `json:"resource_limits"`
	TargetExecutor    string           `json:"target_executor"`
	WriteScope        WriteScope       `json:"write_scope"`
	RetryPolicy       RetryPolicy      `json:"retry_policy"`
	AcceptancePolicy  AcceptancePolicy `json:"acceptance_policy"`
	MemoryHints       MemoryHints      `json:"memory_hints"`
	Trigger           TriggerType      `json:"trigger"`
	CreatedAt         time.Time        `json:"created_at"`
}

type MemoryHints struct {
	Scopes   []MemoryScope `json:"scopes"`
	Keywords []string      `json:"keywords"`
	MaxItems int           `json:"max_items"`
}

type ResourceLimits struct {
	TokenLimit      int64         `json:"token_limit"`
	WallClockLimit  time.Duration `json:"wall_clock_limit"`
	GPUMinuteLimit  int64         `json:"gpu_minute_limit"`
	MaxToolCalls    int           `json:"max_tool_calls"`
	NeedsNetwork    bool          `json:"needs_network"`
}

type RetryPolicy struct {
	MaxRetries      int           `json:"max_retries"`
	BackoffInitial  time.Duration `json:"backoff_initial"`
	BackoffMax      time.Duration `json:"backoff_max"`
	RetryOnSemantic []FailureSemantic `json:"retry_on_semantic"`
}

type RunRecord struct {
	RunID                string               `json:"run_id"`
	TaskID               string               `json:"task_id"`
	WorkflowID           string               `json:"workflow_id,omitempty"`
	ParentRunID          string               `json:"parent_run_id,omitempty"`
	RunMode              RunMode              `json:"run_mode"`
	RunStatus            RunStatus            `json:"run_status"`
	WorkflowPhase        WorkflowPhase        `json:"workflow_phase,omitempty"`
	StateVersion         int64                `json:"state_version"`
	Timeline             Timeline             `json:"timeline"`
	CostSummary          CostSummary          `json:"cost_summary"`
	LogIndex             []string             `json:"log_index"`
	ArtifactRefs         []string             `json:"artifact_refs"`
	FailureInfo          FailureInfo          `json:"failure_info"`
	InterventionHistory  []InterventionRecord `json:"intervention_history"`
	ResultSummary        string               `json:"result_summary"`
	RoutingDecision      RoutingDecision      `json:"routing_decision"`
	ClassifierSummary    ClassifierSummary    `json:"classifier_summary"`
	SupersededBy         string               `json:"superseded_by,omitempty"`
}

type Timeline struct {
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

type CostSummary struct {
	InputTokens     int64         `json:"input_tokens"`
	OutputTokens    int64         `json:"output_tokens"`
	Duration        time.Duration `json:"duration"`
	GPUMinutes      int64         `json:"gpu_minutes"`
	ModelCostMicros int64         `json:"model_cost_micros"`
}

type FailureInfo struct {
	Source         FailureSource   `json:"source"`
	Semantic       FailureSemantic `json:"semantic"`
	Summary        string          `json:"summary"`
	Recoverable    bool            `json:"recoverable"`
	LastError      string          `json:"last_error"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

type InterventionRecord struct {
	ActorID    string    `json:"actor_id"`
	SignalType string    `json:"signal_type"`
	Comment    string    `json:"comment"`
	At         time.Time `json:"at"`
}

type RoutingDecision struct {
	Path          RoutingPath `json:"path"`
	EscalatedFrom string      `json:"escalated_from,omitempty"`
	Reason        string      `json:"reason"`
}

type ClassifierSummary struct {
	RequiredCapabilities []string `json:"required_capabilities"`
	NeedsNetwork         bool     `json:"needs_network"`
	NeedsSimpleExecution bool     `json:"needs_simple_execution"`
	NeedsRepoContext     bool     `json:"needs_repo_context"`
	NeedsMemoryRecall    bool     `json:"needs_memory_recall"`
	RiskLevel            string   `json:"risk_level"`
	SuggestedBudgetTier  string   `json:"suggested_budget_tier"`
	SuggestedRuntime     string   `json:"suggested_runtime"`
	SuggestedRoles       []string `json:"suggested_roles"`
}

type Event struct {
	EventID         string        `json:"event_id"`
	EventType       EventType     `json:"event_type"`
	Source          string        `json:"source"`
	Target          string        `json:"target"`
	ProjectID       string        `json:"project_id"`
	TaskID          string        `json:"task_id"`
	RunID           string        `json:"run_id,omitempty"`
	Severity        EventSeverity `json:"severity"`
	Payload         map[string]any `json:"payload"`
	Attempt         int           `json:"attempt"`
	CreatedAt       time.Time     `json:"created_at"`
	IdempotencyKey  string        `json:"idempotency_key"`
}

type ScheduleEntry struct {
	ScheduleID      string         `json:"schedule_id"`
	ProjectID       string         `json:"project_id,omitempty"`
	TaskTemplateID  string         `json:"task_template_id"`
	TriggerSpec     TriggerSpec    `json:"trigger_spec"`
	DedupeCursor    string         `json:"dedupe_cursor"`
	Timezone        string         `json:"timezone"`
	Status          ScheduleStatus `json:"status"`
	LastSuccessAt   time.Time      `json:"last_success_at"`
	LastFailureAt   time.Time      `json:"last_failure_at"`
}

type TriggerSpec struct {
	CronExpr    string        `json:"cron_expr,omitempty"`
	Interval    time.Duration `json:"interval,omitempty"`
	WindowStart string        `json:"window_start,omitempty"`
	WindowEnd   string        `json:"window_end,omitempty"`
}

type IntentSpec struct {
	IntentID              string      `json:"intent_id"`
	RawText               string      `json:"raw_text"`
	ActorID               string      `json:"actor_id"`
	Channel               string      `json:"channel"`
	IntentKind            IntentKind  `json:"intent_kind"`
	Scope                 IntentScope `json:"scope"`
	ParsedPayload         map[string]any `json:"parsed_payload"`
	CompilerProfile       string      `json:"compiler_profile"`
	Confidence            float64     `json:"confidence"`
	RiskLevel             string      `json:"risk_level"`
	RequiresConfirmation  bool        `json:"requires_confirmation"`
}

type RuntimeSetting struct {
	SettingID       string               `json:"setting_id"`
	ProjectID       string               `json:"project_id,omitempty"`
	SettingKey      string               `json:"setting_key"`
	DesiredValue    any                  `json:"desired_value"`
	SourceIntentID  string               `json:"source_intent_id"`
	MutableClass    RuntimeSettingClass  `json:"mutable_class"`
	Version         int64                `json:"version"`
	Status          RuntimeSettingStatus `json:"status"`
}

type ConfigChangeProposal struct {
	ProposalID      string               `json:"proposal_id"`
	ProjectID       string               `json:"project_id,omitempty"`
	ProposalKind    ConfigProposalKind   `json:"proposal_kind"`
	SourceIntentID  string               `json:"source_intent_id"`
	TargetRef       string               `json:"target_ref"`
	ProposedChange  map[string]any       `json:"proposed_change"`
	RiskSummary     string               `json:"risk_summary"`
	Status          ConfigProposalStatus `json:"status"`
}

type ApprovalTicket struct {
	ApprovalID       string         `json:"approval_id"`
	ProjectID        string         `json:"project_id,omitempty"`
	RunID            string         `json:"run_id"`
	ActionKind       string         `json:"action_kind"`
	RequestedEffect  string         `json:"requested_effect"`
	RiskSummary      string         `json:"risk_summary"`
	Status           ApprovalStatus `json:"status"`
	ExpiresAt        time.Time      `json:"expires_at"`
}

type MemoryRecord struct {
	MemoryID        string      `json:"memory_id"`
	Scope           MemoryScope `json:"scope"`
	ProjectID       string      `json:"project_id,omitempty"`
	RepositoryID    string      `json:"repository_id,omitempty"`
	MemoryType      MemoryType  `json:"memory_type"`
	Summary         string      `json:"summary"`
	ContentRef      string      `json:"content_ref"`
	Source          string      `json:"source"`
	Confidence      float64     `json:"confidence"`
	Freshness       Freshness   `json:"freshness"`
	LastVerifiedAt  time.Time   `json:"last_verified_at"`
	Tags            []string    `json:"tags"`
	Archived        bool        `json:"archived"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type AcceptancePolicy struct {
	MetricTargets       map[string]float64 `json:"metric_targets"`
	Tolerance           map[string]float64 `json:"tolerance"`
	RegressionGuards    map[string]float64 `json:"regression_guards"`
	MaxRetryRounds      int                `json:"max_retry_rounds"`
	ManualGateRequired  bool               `json:"manual_gate_required"`
	ComparisonBaseline  string             `json:"comparison_baseline"`
	MinSampleSize       int                `json:"min_sample_size"`
	DeterminismPolicy   string             `json:"determinism_policy"`
}

type EvalTask struct {
	TaskSpec
	EvalID             string `json:"eval_id"`
	PRID               string `json:"pr_id"`
	CommitSHA          string `json:"commit_sha"`
	BaselineRef        string `json:"baseline_ref"`
	DatasetRef         string `json:"dataset_ref"`
	EnvironmentRef     string `json:"environment_ref"`
	BenchmarkSuiteRef  string `json:"benchmark_suite_ref"`
	SeedPolicy         string `json:"seed_policy"`
	AggregationRule    AggregationRule `json:"aggregation_rule"`
	EvalMatrix         []EvalShard `json:"eval_matrix"`
}

type EvalShard struct {
	ShardID        string        `json:"shard_id"`
	Required       bool          `json:"required"`
	EnvironmentRef string        `json:"environment_ref"`
	DatasetRef     string        `json:"dataset_ref"`
	MaxRetries     int           `json:"max_retries"`
	Timeout        time.Duration `json:"timeout"`
	Weight         float64       `json:"weight"`
}

type AggregationRule struct {
	RequiredShardIDs []string `json:"required_shard_ids"`
	InformationalShardIDs []string `json:"informational_shard_ids"`
	AllowNeutral      bool     `json:"allow_neutral"`
	Mode              string   `json:"mode"`
}

type BaselineSnapshot struct {
	BaselineID         string            `json:"baseline_id"`
	ProjectID          string            `json:"project_id"`
	SourceKind         string            `json:"source_kind"`
	CodeRef            string            `json:"code_ref"`
	DatasetRef         string            `json:"dataset_ref"`
	BenchmarkSuiteRef  string            `json:"benchmark_suite_ref"`
	EnvironmentRef     string            `json:"environment_ref"`
	MetricSchema       map[string]string `json:"metric_schema"`
	SeedPolicy         string            `json:"seed_policy"`
	SampleSize         int               `json:"sample_size"`
	MetricSnapshot     map[string]float64 `json:"metric_snapshot"`
	VerifiedAt         time.Time         `json:"verified_at"`
}

type EvalReport struct {
	EvalID             string             `json:"eval_id"`
	RunID              string             `json:"run_id"`
	Comparable         bool               `json:"comparable"`
	MetricDeltas       map[string]float64 `json:"metric_deltas"`
	RiskFindings       []string           `json:"risk_findings"`
	ArtifactRefs       []string           `json:"artifact_refs"`
	RecommendedAction  string             `json:"recommended_action"`
	Confidence         float64            `json:"confidence"`
}

type ExecutorCapability struct {
	ExecutorID         string        `json:"executor_id"`
	SupportsShell      bool          `json:"supports_shell"`
	SupportsGPU        bool          `json:"supports_gpu"`
	SupportsSlurm      bool          `json:"supports_slurm"`
	SupportsNetwork    bool          `json:"supports_network"`
	WorkspaceRoot      string        `json:"workspace_root"`
	ConcurrencyLimit   int           `json:"concurrency_limit"`
	MaxRuntime         time.Duration `json:"max_runtime"`
	AllowedProjects    []string      `json:"allowed_projects"`
}

type LockRecord struct {
	LockID       string     `json:"lock_id"`
	LockType     LockType   `json:"lock_type"`
	LockKey      string     `json:"lock_key"`
	OwnerRunID   string     `json:"owner_run_id"`
	LeaseToken   int64      `json:"lease_token"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	Status       LockStatus `json:"status"`
}

type ActionIntent struct {
	ActionID        string         `json:"action_id"`
	RunID           string         `json:"run_id"`
	ProjectID       string         `json:"project_id"`
	Kind            string         `json:"kind"`
	TargetRef       string         `json:"target_ref"`
	Payload         map[string]any `json:"payload"`
	IdempotencyKey  string         `json:"idempotency_key"`
	LeaseToken      int64          `json:"lease_token"`
	Status          string         `json:"status"`
	Attempt         int            `json:"attempt"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type TaskClassifiedResult struct {
	RoutingDecision      RoutingPath `json:"routing_decision"`
	RequiredCapabilities []string    `json:"required_capabilities"`
	NeedsNetwork         bool        `json:"needs_network"`
	NeedsSimpleExecution bool        `json:"needs_simple_execution"`
	NeedsRepoContext     bool        `json:"needs_repo_context"`
	NeedsMemoryRecall    bool        `json:"needs_memory_recall"`
	RiskLevel            string      `json:"risk_level"`
	SuggestedBudgetTier  string      `json:"suggested_budget_tier"`
	SuggestedRuntime     string      `json:"suggested_runtime"`
	SuggestedRoles       []string    `json:"suggested_roles"`
	Reason               string      `json:"reason"`
}

type ContextPacket struct {
	Goal             string         `json:"goal"`
	Constraints      []string       `json:"constraints"`
	CurrentState     map[string]any `json:"current_state"`
	RelevantArtifacts []string      `json:"relevant_artifacts"`
	MemoryContext    []MemoryRecord `json:"memory_context"`
	NextAction       string         `json:"next_action"`
	Role             string         `json:"role"`
}

type RuntimeResult struct {
	Role               string         `json:"role"`
	Summary            string         `json:"summary"`
	ProposedActions    []string       `json:"proposed_actions"`
	Artifacts          []string       `json:"artifacts"`
	MemoryCandidates   []MemoryRecord `json:"memory_candidates"`
	FollowupHint       string         `json:"followup_hint"`
	RequiresEscalation bool           `json:"requires_escalation"`
}

type ManualSignal struct {
	SignalType string         `json:"signal_type"`
	RunID      string         `json:"run_id"`
	Payload    map[string]any `json:"payload"`
}
