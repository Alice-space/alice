package domain

import (
	"encoding/json"
	"time"
)

type ExternalEvent struct {
	EventID           string          `json:"event_id"`
	EventType         EventType       `json:"event_type"`
	SourceKind        string          `json:"source_kind"`
	TransportKind     string          `json:"transport_kind"`
	SourceRef         string          `json:"source_ref"`
	ActorRef          string          `json:"actor_ref"`
	ActionKind        string          `json:"action_kind"`
	RequestID         string          `json:"request_id"`
	TaskID            string          `json:"task_id"`
	ApprovalRequestID string          `json:"approval_request_id"`
	HumanWaitID       string          `json:"human_wait_id"`
	StepExecutionID   string          `json:"step_execution_id"`
	TargetStepID      string          `json:"target_step_id"`
	WaitingReason     string          `json:"waiting_reason"`
	DecisionHash      string          `json:"decision_hash"`
	ReplyToEventID    string          `json:"reply_to_event_id"`
	ConversationID    string          `json:"conversation_id"`
	ThreadID          string          `json:"thread_id"`
	RepoRef           string          `json:"repo_ref"`
	IssueRef          string          `json:"issue_ref"`
	PRRef             string          `json:"pr_ref"`
	CommentRef        string          `json:"comment_ref"`
	ScheduledTaskID   string          `json:"scheduled_task_id"`
	ControlObjectRef  string          `json:"control_object_ref"`
	WorkflowObjectRef string          `json:"workflow_object_ref"`
	CoalescingKey     string          `json:"coalescing_key"`
	ParentEventID     string          `json:"parent_event_id"`
	CausationID       string          `json:"causation_id"`
	TraceID           string          `json:"trace_id"`
	IdempotencyKey    string          `json:"idempotency_key"`
	Verified          bool            `json:"verified"`
	PayloadRef        string          `json:"payload_ref"`
	InputSchemaID     string          `json:"input_schema_id,omitempty"`
	InputPatch        json.RawMessage `json:"input_patch,omitempty"`
	ReceivedAt        time.Time       `json:"received_at"`
}

type RouteSnapshot struct {
	RouteKeys []string `json:"route_keys"`
	MatchedBy string   `json:"matched_by"`
}

type EphemeralRequest struct {
	RequestID           string        `json:"request_id"`
	Status              RequestStatus `json:"status"`
	OpenedByEventID     string        `json:"opened_by_event_id"`
	LastEventID         string        `json:"last_event_id"`
	TraceID             string        `json:"trace_id"`
	IntentSummary       string        `json:"intent_summary"`
	RouteSnapshot       RouteSnapshot `json:"route_snapshot"`
	PromotionDecisionID string        `json:"promotion_decision_id"`
	ContextPackIDs      []string      `json:"context_pack_ids"`
	AgentDispatchIDs    []string      `json:"agent_dispatch_ids"`
	LastReplyID         string        `json:"last_reply_id"`
	TerminalResultID    string        `json:"terminal_result_id"`
	PromotedTaskID      string        `json:"promoted_task_id"`
	ExpiresAt           time.Time     `json:"expires_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

type PromotionDecision struct {
	DecisionID             string          `json:"decision_id"`
	RequestID              string          `json:"request_id"`
	IntentKind             string          `json:"intent_kind"`
	RequiredRefs           []string        `json:"required_refs"`
	RiskLevel              string          `json:"risk_level"`
	ExternalWrite          bool            `json:"external_write"`
	CreatePersistentObject bool            `json:"create_persistent_object"`
	Async                  bool            `json:"async"`
	MultiStep              bool            `json:"multi_step"`
	MultiAgent             bool            `json:"multi_agent"`
	ApprovalRequired       bool            `json:"approval_required"`
	BudgetRequired         bool            `json:"budget_required"`
	RecoveryRequired       bool            `json:"recovery_required"`
	ProposedWorkflowIDs    []string        `json:"proposed_workflow_ids"`
	SelectedWorkflowID     string          `json:"selected_workflow_id"`
	Result                 PromotionResult `json:"result"`
	ReasonCodes            []string        `json:"reason_codes"`
	Confidence             float64         `json:"confidence"`
	ProducedBy             string          `json:"produced_by"`
	ProducedAt             time.Time       `json:"produced_at"`
}

type DurableTask struct {
	TaskID                 string        `json:"task_id"`
	SourceRequestID        string        `json:"source_request_id"`
	OpenedByEventID        string        `json:"opened_by_event_id"`
	TraceID                string        `json:"trace_id"`
	Status                 TaskStatus    `json:"status"`
	WaitingReason          WaitingReason `json:"waiting_reason"`
	CurrentBindingID       string        `json:"current_binding_id"`
	CurrentStepExecutionID string        `json:"current_step_execution_id"`
	RiskLevel              string        `json:"risk_level"`
	BudgetPolicyRef        string        `json:"budget_policy_ref"`
	CancellationRef        string        `json:"cancellation_ref"`
	UpdatedAt              time.Time     `json:"updated_at"`
}

type WorkflowBinding struct {
	BindingID      string    `json:"binding_id"`
	TaskID         string    `json:"task_id"`
	WorkflowID     string    `json:"workflow_id"`
	WorkflowSource string    `json:"workflow_source"`
	WorkflowRev    string    `json:"workflow_rev"`
	ManifestDigest string    `json:"manifest_digest"`
	ManifestRef    string    `json:"manifest_ref"`
	EntryStepID    string    `json:"entry_step_id"`
	BoundByEventID string    `json:"bound_by_event_id"`
	BoundReason    string    `json:"bound_reason"`
	SupersededBy   string    `json:"superseded_by"`
	Active         bool      `json:"active"`
	BoundAt        time.Time `json:"bound_at"`
}

type StepExecution struct {
	ExecutionID        string     `json:"execution_id"`
	TaskID             string     `json:"task_id"`
	BindingID          string     `json:"binding_id"`
	StepID             string     `json:"step_id"`
	Role               Role       `json:"role"`
	Status             StepStatus `json:"status"`
	Attempt            uint32     `json:"attempt"`
	ParentDispatchID   string     `json:"parent_dispatch_id"`
	InputArtifactIDs   []string   `json:"input_artifact_ids"`
	OutputArtifactIDs  []string   `json:"output_artifact_ids"`
	CheckpointRef      string     `json:"checkpoint_ref"`
	ResumeToken        string     `json:"resume_token"`
	RemoteExecutionRef string     `json:"remote_execution_ref"`
	LeaseOwner         string     `json:"lease_owner"`
	LeaseExpiresAt     time.Time  `json:"lease_expires_at"`
	LastHeartbeatAt    time.Time  `json:"last_heartbeat_at"`
	FailureCode        string     `json:"failure_code"`
	FailureMessage     string     `json:"failure_message"`
	SupersededBy       string     `json:"superseded_by"`
	StartedAt          time.Time  `json:"started_at"`
	FinishedAt         time.Time  `json:"finished_at"`
}

type Artifact struct {
	ArtifactID    string    `json:"artifact_id"`
	TaskID        string    `json:"task_id"`
	BindingID     string    `json:"binding_id"`
	ExecutionID   string    `json:"execution_id"`
	Family        string    `json:"family"`
	SchemaID      string    `json:"schema_id"`
	SchemaVersion string    `json:"schema_version"`
	ContentRef    string    `json:"content_ref"`
	Summary       string    `json:"summary"`
	SupersededBy  string    `json:"superseded_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type ContextPack struct {
	ContextPackID        string            `json:"context_pack_id"`
	OwnerKind            string            `json:"owner_kind"`
	OwnerID              string            `json:"owner_id"`
	SummaryRef           string            `json:"summary_ref"`
	ConversationSliceRef string            `json:"conversation_slice_ref"`
	ArtifactIDs          []string          `json:"artifact_ids"`
	ExternalRefSnapshot  map[string]string `json:"external_ref_snapshot"`
	WorkingStateRef      string            `json:"working_state_ref"`
	ContextDigest        string            `json:"context_digest"`
	CreatedAt            time.Time         `json:"created_at"`
}

type AgentDispatch struct {
	DispatchID         string         `json:"dispatch_id"`
	OwnerKind          string         `json:"owner_kind"`
	OwnerID            string         `json:"owner_id"`
	ParentExecutionID  string         `json:"parent_execution_id"`
	InitiatorRole      Role           `json:"initiator_role"`
	AgentLabel         string         `json:"agent_label"`
	RequestedRole      Role           `json:"requested_role"`
	Goal               string         `json:"goal"`
	ContextPackID      string         `json:"context_pack_id"`
	InputRefs          []string       `json:"input_refs"`
	ExpectedOutputs    []string       `json:"expected_outputs"`
	AllowedTools       []string       `json:"allowed_tools"`
	AllowedMCP         []string       `json:"allowed_mcp"`
	SandboxTemplate    string         `json:"sandbox_template"`
	BudgetCapRef       string         `json:"budget_cap_ref"`
	DeadlineAt         time.Time      `json:"deadline_at"`
	WriteScopeRef      string         `json:"write_scope_ref"`
	ReturnToRef        string         `json:"return_to_ref"`
	IdempotencyKey     string         `json:"idempotency_key"`
	RunnerKind         string         `json:"runner_kind"`
	Attempt            uint32         `json:"attempt"`
	RemoteExecutionRef string         `json:"remote_execution_ref"`
	CheckpointRef      string         `json:"checkpoint_ref"`
	ResumeToken        string         `json:"resume_token"`
	LeaseOwner         string         `json:"lease_owner"`
	LeaseExpiresAt     time.Time      `json:"lease_expires_at"`
	LastHeartbeatAt    time.Time      `json:"last_heartbeat_at"`
	FailureCode        string         `json:"failure_code"`
	FailureMessage     string         `json:"failure_message"`
	Status             DispatchStatus `json:"status"`
	CreatedAt          time.Time      `json:"created_at"`
	CompletedAt        time.Time      `json:"completed_at"`
}

type ApprovalRequest struct {
	ApprovalRequestID string     `json:"approval_request_id"`
	TaskID            string     `json:"task_id"`
	BindingID         string     `json:"binding_id"`
	StepExecutionID   string     `json:"step_execution_id"`
	GateType          GateType   `json:"gate_type"`
	Status            GateStatus `json:"status"`
	TargetVersionRef  string     `json:"target_version_ref"`
	RequiredSlots     []string   `json:"required_slots"`
	DeadlineAt        time.Time  `json:"deadline_at"`
	AggregationPolicy string     `json:"aggregation_policy"`
	OpenedByEventID   string     `json:"opened_by_event_id"`
	ResolvedByEventID string     `json:"resolved_by_event_id"`
}

type HumanWaitRecord struct {
	HumanWaitID       string        `json:"human_wait_id"`
	TaskID            string        `json:"task_id"`
	BindingID         string        `json:"binding_id"`
	StepExecutionID   string        `json:"step_execution_id"`
	WaitingReason     WaitingReason `json:"waiting_reason"`
	InputSchemaID     string        `json:"input_schema_id"`
	ResumeOptions     []string      `json:"resume_options"`
	PromptRef         string        `json:"prompt_ref"`
	Status            string        `json:"status"`
	OpenedByEventID   string        `json:"opened_by_event_id"`
	ResolvedByEventID string        `json:"resolved_by_event_id"`
	DeadlineAt        time.Time     `json:"deadline_at"`
}

type OutboxRecord struct {
	ActionID           string       `json:"action_id"`
	TaskID             string       `json:"task_id"`
	BindingID          string       `json:"binding_id"`
	ExecutionID        string       `json:"execution_id"`
	MCPDomain          string       `json:"mcp_domain"`
	ActionType         string       `json:"action_type"`
	ExternalTargetRef  string       `json:"external_target_ref"`
	IdempotencyKey     string       `json:"idempotency_key"`
	PayloadRef         string       `json:"payload_ref"`
	Status             OutboxStatus `json:"status"`
	RemoteRequestID    string       `json:"remote_request_id"`
	LastExternalRef    string       `json:"last_external_ref"`
	LastReceiptStatus  string       `json:"last_receipt_status"`
	ReceiptWindowUntil time.Time    `json:"receipt_window_until"`
	AttemptCount       uint32       `json:"attempt_count"`
	NextAttemptAt      time.Time    `json:"next_attempt_at"`
	LastErrorCode      string       `json:"last_error_code"`
	LastErrorMessage   string       `json:"last_error_message"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type ToolCallRecord struct {
	CallID      string    `json:"call_id"`
	OwnerKind   string    `json:"owner_kind"`
	OwnerID     string    `json:"owner_id"`
	ExecutionID string    `json:"execution_id"`
	DispatchID  string    `json:"dispatch_id"`
	ToolOrMCP   string    `json:"tool_or_mcp"`
	RequestRef  string    `json:"request_ref"`
	ResponseRef string    `json:"response_ref"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

type ReplyRecord struct {
	ReplyID        string    `json:"reply_id"`
	OwnerKind      string    `json:"owner_kind"`
	OwnerID        string    `json:"owner_id"`
	ReplyChannel   string    `json:"reply_channel"`
	ReplyToEventID string    `json:"reply_to_event_id"`
	PayloadRef     string    `json:"payload_ref"`
	Final          bool      `json:"final"`
	DeliveryStatus string    `json:"delivery_status"`
	DeliveredAt    time.Time `json:"delivered_at"`
}

type TerminalResult struct {
	ResultID     string    `json:"result_id"`
	OwnerKind    string    `json:"owner_kind"`
	OwnerID      string    `json:"owner_id"`
	FinalStatus  string    `json:"final_status"`
	SummaryRef   string    `json:"summary_ref"`
	FinalReplyID string    `json:"final_reply_id"`
	PrimaryRef   string    `json:"primary_ref"`
	ClosedAt     time.Time `json:"closed_at"`
}

type UsageLedgerEntry struct {
	EntryID         string    `json:"entry_id"`
	TaskID          string    `json:"task_id"`
	ExecutionID     string    `json:"execution_id"`
	Domain          string    `json:"domain"`
	Kind            string    `json:"kind"`
	TokenUsed       int64     `json:"token_used"`
	CostMicros      int64     `json:"cost_micros"`
	ResourceUnits   int64     `json:"resource_units"`
	BudgetRemaining int64     `json:"budget_remaining"`
	RecordedAt      time.Time `json:"recorded_at"`
}

type ScheduledTask struct {
	ScheduledTaskID      string    `json:"scheduled_task_id"`
	SpecKind             string    `json:"spec_kind"`
	SpecText             string    `json:"spec_text"`
	Timezone             string    `json:"timezone"`
	InputTemplate        string    `json:"input_template"`
	TargetWorkflowID     string    `json:"target_workflow_id"`
	TargetWorkflowSource string    `json:"target_workflow_source"`
	TargetWorkflowRev    string    `json:"target_workflow_rev"`
	ScheduleRevision     string    `json:"schedule_revision"`
	Enabled              bool      `json:"enabled"`
	NextFireAt           time.Time `json:"next_fire_at"`
	LastFireAt           time.Time `json:"last_fire_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}
