package domain

import (
	"encoding/json"
	"time"
)

type ExternalEventIngestedPayload struct {
	Event ExternalEvent `json:"event"`
}

type EphemeralRequestOpenedPayload struct {
	RequestID          string    `json:"request_id"`
	OpenedByEventID    string    `json:"opened_by_event_id"`
	RouteSnapshotRef   string    `json:"route_snapshot_ref"`
	ActivatedRouteKeys []string  `json:"activated_route_keys"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type PromotionAssessedPayload struct {
	RequestID          string    `json:"request_id"`
	DecisionID         string    `json:"decision_id"`
	Result             string    `json:"result"`
	SelectedWorkflowID string    `json:"selected_workflow_id"`
	ReasonCodes        []string  `json:"reason_codes"`
	Confidence         float64   `json:"confidence"`
	AssessedAt         time.Time `json:"assessed_at"`
}

type RequestPromotedPayload struct {
	RequestID        string    `json:"request_id"`
	TaskID           string    `json:"task_id"`
	RouteSnapshotRef string    `json:"route_snapshot_ref"`
	RevokedRouteKeys []string  `json:"revoked_route_keys"`
	PromotedAt       time.Time `json:"promoted_at"`
}

type RequestAnsweredPayload struct {
	RequestID        string    `json:"request_id"`
	FinalReplyID     string    `json:"final_reply_id"`
	RevokedRouteKeys []string  `json:"revoked_route_keys"`
	AnsweredAt       time.Time `json:"answered_at"`
}

type ContextPackRecordedPayload struct {
	ContextPackID string            `json:"context_pack_id"`
	OwnerKind     string            `json:"owner_kind"`
	OwnerID       string            `json:"owner_id"`
	SummaryRef    string            `json:"summary_ref"`
	ArtifactIDs   []string          `json:"artifact_ids"`
	ExternalRefs  map[string]string `json:"external_refs"`
	ContextDigest string            `json:"context_digest"`
	CreatedAt     time.Time         `json:"created_at"`
}

type AgentDispatchRecordedPayload struct {
	DispatchID        string    `json:"dispatch_id"`
	OwnerKind         string    `json:"owner_kind"`
	OwnerID           string    `json:"owner_id"`
	ParentExecutionID string    `json:"parent_execution_id"`
	RequestedRole     string    `json:"requested_role"`
	Goal              string    `json:"goal"`
	ContextPackID     string    `json:"context_pack_id"`
	AllowedTools      []string  `json:"allowed_tools"`
	AllowedMCP        []string  `json:"allowed_mcp"`
	WriteScopeRef     string    `json:"write_scope_ref"`
	ReturnToRef       string    `json:"return_to_ref"`
	DeadlineAt        time.Time `json:"deadline_at"`
}

type AgentDispatchCheckpointedPayload struct {
	DispatchID         string    `json:"dispatch_id"`
	Attempt            uint32    `json:"attempt"`
	Status             string    `json:"status"`
	RemoteExecutionRef string    `json:"remote_execution_ref"`
	CheckpointRef      string    `json:"checkpoint_ref"`
	ResumeToken        string    `json:"resume_token"`
	LastHeartbeatAt    time.Time `json:"last_heartbeat_at"`
}

type AgentDispatchCompletedPayload struct {
	DispatchID         string    `json:"dispatch_id"`
	Status             string    `json:"status"`
	OutputArtifactRefs []string  `json:"output_artifact_refs"`
	FailureCode        string    `json:"failure_code"`
	FailureMessage     string    `json:"failure_message"`
	CompletedAt        time.Time `json:"completed_at"`
}

type ToolCallRecordedPayload struct {
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

type TaskPromotedAndBoundPayload struct {
	RequestID          string    `json:"request_id"`
	TaskID             string    `json:"task_id"`
	BindingID          string    `json:"binding_id"`
	WorkflowID         string    `json:"workflow_id"`
	WorkflowSource     string    `json:"workflow_source"`
	WorkflowRev        string    `json:"workflow_rev"`
	ManifestDigest     string    `json:"manifest_digest"`
	EntryStepID        string    `json:"entry_step_id"`
	ReplyToEventID     string    `json:"reply_to_event_id"`
	RepoRef            string    `json:"repo_ref"`
	IssueRef           string    `json:"issue_ref"`
	PRRef              string    `json:"pr_ref"`
	ScheduledTaskID    string    `json:"scheduled_task_id"`
	ControlObjectRef   string    `json:"control_object_ref"`
	WorkflowObjectRef  string    `json:"workflow_object_ref"`
	ActivatedRouteKeys []string  `json:"activated_route_keys"`
	RouteSnapshotRef   string    `json:"route_snapshot_ref"`
	PromotedAt         time.Time `json:"promoted_at"`
}

type TaskWaitingHumanMarkedPayload struct {
	TaskID          string    `json:"task_id"`
	WaitingReason   string    `json:"waiting_reason"`
	StepExecutionID string    `json:"step_execution_id"`
	WaitRef         string    `json:"wait_ref"`
	EnteredAt       time.Time `json:"entered_at"`
}

type TaskResumedPayload struct {
	TaskID          string    `json:"task_id"`
	WaitingReason   string    `json:"waiting_reason"`
	StepExecutionID string    `json:"step_execution_id"`
	ResumeDecision  string    `json:"resume_decision"`
	ResumePointRef  string    `json:"resume_point_ref"`
	ResumedAt       time.Time `json:"resumed_at"`
}

type StepExecutionStartedPayload struct {
	ExecutionID        string    `json:"execution_id"`
	TaskID             string    `json:"task_id"`
	BindingID          string    `json:"binding_id"`
	StepID             string    `json:"step_id"`
	Attempt            uint32    `json:"attempt"`
	ParentDispatchID   string    `json:"parent_dispatch_id"`
	InputArtifactIDs   []string  `json:"input_artifact_ids"`
	LeaseOwner         string    `json:"lease_owner"`
	LeaseExpiresAt     time.Time `json:"lease_expires_at"`
	RemoteExecutionRef string    `json:"remote_execution_ref"`
}

type StepExecutionCheckpointedPayload struct {
	ExecutionID        string    `json:"execution_id"`
	Attempt            uint32    `json:"attempt"`
	CheckpointRef      string    `json:"checkpoint_ref"`
	ResumeToken        string    `json:"resume_token"`
	RemoteExecutionRef string    `json:"remote_execution_ref"`
	LastHeartbeatAt    time.Time `json:"last_heartbeat_at"`
}

type StepExecutionCompletedPayload struct {
	ExecutionID        string    `json:"execution_id"`
	Attempt            uint32    `json:"attempt"`
	OutputArtifactRefs []string  `json:"output_artifact_refs"`
	SummaryRef         string    `json:"summary_ref"`
	CompletedAt        time.Time `json:"completed_at"`
}

type StepExecutionFailedPayload struct {
	ExecutionID    string    `json:"execution_id"`
	Attempt        uint32    `json:"attempt"`
	FailureCode    string    `json:"failure_code"`
	FailureMessage string    `json:"failure_message"`
	Retryable      bool      `json:"retryable"`
	FailedAt       time.Time `json:"failed_at"`
}

type StepExecutionCancelledPayload struct {
	ExecutionID        string    `json:"execution_id"`
	Attempt            uint32    `json:"attempt"`
	ReasonCode         string    `json:"reason_code"`
	RemoteExecutionRef string    `json:"remote_execution_ref"`
	CancelledAt        time.Time `json:"cancelled_at"`
}

type StepExecutionRewoundPayload struct {
	TaskID          string    `json:"task_id"`
	FromExecutionID string    `json:"from_execution_id"`
	ToStepID        string    `json:"to_step_id"`
	DecisionRef     string    `json:"decision_ref"`
	RewoundAt       time.Time `json:"rewound_at"`
}

type ApprovalRequestOpenedPayload struct {
	ApprovalRequestID string    `json:"approval_request_id"`
	TaskID            string    `json:"task_id"`
	StepExecutionID   string    `json:"step_execution_id"`
	GateType          string    `json:"gate_type"`
	TargetVersionRef  string    `json:"target_version_ref"`
	RequiredSlots     []string  `json:"required_slots"`
	DeadlineAt        time.Time `json:"deadline_at"`
}

type ApprovalRequestResolvedPayload struct {
	ApprovalRequestID string    `json:"approval_request_id"`
	Resolution        string    `json:"resolution"`
	ResolvedByActor   string    `json:"resolved_by_actor"`
	ResolutionRef     string    `json:"resolution_ref"`
	ResolvedAt        time.Time `json:"resolved_at"`
}

type HumanWaitRecordedPayload struct {
	HumanWaitID     string          `json:"human_wait_id"`
	TaskID          string          `json:"task_id"`
	StepExecutionID string          `json:"step_execution_id"`
	WaitingReason   string          `json:"waiting_reason"`
	InputSchemaID   string          `json:"input_schema_id"`
	InputDraft      json.RawMessage `json:"input_draft,omitempty"`
	ResumeOptions   []string        `json:"resume_options"`
	PromptRef       string          `json:"prompt_ref"`
	DeadlineAt      time.Time       `json:"deadline_at"`
}

type AdminAuditRecordedPayload struct {
	AdminActionID string    `json:"admin_action_id"`
	Operation     string    `json:"operation"`
	ActorRef      string    `json:"actor_ref,omitempty"`
	TargetKind    string    `json:"target_kind,omitempty"`
	TargetID      string    `json:"target_id,omitempty"`
	RequestPath   string    `json:"request_path,omitempty"`
	RequestMethod string    `json:"request_method,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type HumanWaitResolvedPayload struct {
	HumanWaitID     string    `json:"human_wait_id"`
	WaitingReason   string    `json:"waiting_reason"`
	Resolution      string    `json:"resolution"`
	ResolvedByActor string    `json:"resolved_by_actor"`
	ResolutionRef   string    `json:"resolution_ref"`
	ResolvedAt      time.Time `json:"resolved_at"`
}

type OutboxQueuedPayload struct {
	ActionID       string    `json:"action_id"`
	Domain         string    `json:"domain"`
	ActionType     string    `json:"action_type"`
	TargetRef      string    `json:"target_ref"`
	IdempotencyKey string    `json:"idempotency_key"`
	PayloadRef     string    `json:"payload_ref"`
	DeadlineAt     time.Time `json:"deadline_at"`
}

type OutboxReceiptRecordedPayload struct {
	TaskID          string    `json:"task_id,omitempty"`
	ActionID        string    `json:"action_id"`
	ReceiptSource   string    `json:"receipt_source"`
	ReceiptKind     string    `json:"receipt_kind"`
	ReceiptStatus   string    `json:"receipt_status"`
	RemoteRequestID string    `json:"remote_request_id"`
	ExternalRef     string    `json:"external_ref"`
	ErrorCode       string    `json:"error_code"`
	ErrorMessage    string    `json:"error_message"`
	RecordedAt      time.Time `json:"recorded_at"`
}

type ReplyRecordedPayload struct {
	ReplyID        string    `json:"reply_id"`
	OwnerKind      string    `json:"owner_kind"`
	OwnerID        string    `json:"owner_id"`
	ReplyChannel   string    `json:"reply_channel"`
	ReplyToEventID string    `json:"reply_to_event_id"`
	PayloadRef     string    `json:"payload_ref"`
	Final          bool      `json:"final"`
	DeliveredAt    time.Time `json:"delivered_at"`
}

type TerminalResultRecordedPayload struct {
	ResultID         string    `json:"result_id"`
	OwnerKind        string    `json:"owner_kind"`
	OwnerID          string    `json:"owner_id"`
	FinalStatus      string    `json:"final_status"`
	FinalReplyID     string    `json:"final_reply_id"`
	RevokedRouteKeys []string  `json:"revoked_route_keys"`
	ClosedAt         time.Time `json:"closed_at"`
}

type UsageLedgerRecordedPayload struct {
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

type ScheduledTaskRegisteredPayload struct {
	ScheduledTaskID      string    `json:"scheduled_task_id"`
	SpecKind             string    `json:"spec_kind"`
	SpecText             string    `json:"spec_text"`
	Timezone             string    `json:"timezone"`
	ScheduleRevision     string    `json:"schedule_revision"`
	TargetWorkflowID     string    `json:"target_workflow_id"`
	TargetWorkflowSource string    `json:"target_workflow_source"`
	TargetWorkflowRev    string    `json:"target_workflow_rev"`
	Enabled              bool      `json:"enabled"`
	NextFireAt           time.Time `json:"next_fire_at"`
	RegisteredAt         time.Time `json:"registered_at"`
}

type ScheduleFirePayload struct {
	FireID                 string    `json:"fire_id"`
	ScheduledTaskID        string    `json:"scheduled_task_id"`
	ScheduledForWindow     time.Time `json:"scheduled_for_window"`
	SourceScheduleRevision string    `json:"source_schedule_revision"`
}

type ScheduleTriggeredPayload struct {
	FireID                 string    `json:"fire_id"`
	ScheduledTaskID        string    `json:"scheduled_task_id"`
	ScheduledForWindow     time.Time `json:"scheduled_for_window"`
	SourceScheduleRevision string    `json:"source_schedule_revision"`
	TargetWorkflowID       string    `json:"target_workflow_id"`
	TargetWorkflowSource   string    `json:"target_workflow_source"`
	TargetWorkflowRev      string    `json:"target_workflow_rev"`
}
