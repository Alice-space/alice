package ops

import (
	"time"

	"alice/internal/store"
)

// EventExternalView represents external event metadata.
type EventExternalView struct {
	SourceKind     string `json:"source_kind,omitempty"`
	TransportKind  string `json:"transport_kind,omitempty"`
	SourceRef      string `json:"source_ref,omitempty"`
	ActorRef       string `json:"actor_ref,omitempty"`
	ReplyToEventID string `json:"reply_to_event_id,omitempty"`
	PayloadRef     string `json:"payload_ref,omitempty"`
}

// EventView represents an event in the read model.
type EventView struct {
	EventID         string             `json:"event_id"`
	EventType       string             `json:"event_type"`
	AggregateKind   string             `json:"aggregate_kind"`
	AggregateID     string             `json:"aggregate_id"`
	CausationID     string             `json:"causation_id,omitempty"`
	TraceID         string             `json:"trace_id,omitempty"`
	PayloadSchemaID string             `json:"payload_schema_id"`
	PayloadVersion  string             `json:"payload_version"`
	GlobalHLC       string             `json:"global_hlc"`
	External        *EventExternalView `json:"external,omitempty"`
}

// RequestView represents a request in the read model.
type RequestView struct {
	RequestID          string   `json:"request_id"`
	UpdatedHLC         string   `json:"updated_hlc"`
	Status             string   `json:"status,omitempty"`
	ConversationID     string   `json:"conversation_id,omitempty"`
	ActorRef           string   `json:"actor_ref,omitempty"`
	PromotionDecision  string   `json:"promotion_decision,omitempty"`
	ContextPacks       []string `json:"context_packs,omitempty"`
	AgentDispatches    []string `json:"agent_dispatches,omitempty"`
	ToolCalls          []string `json:"toolcalls,omitempty"`
	Reply              string   `json:"reply,omitempty"`
	TerminalResult     string   `json:"terminal_result,omitempty"`
	RouteTargetTaskID  string   `json:"task_id,omitempty"`
	RouteSnapshotRef   string   `json:"route_snapshot_ref,omitempty"`
	OpenedByEventID    string   `json:"opened_by_event_id,omitempty"`
	LastTerminalStatus string   `json:"terminal_status,omitempty"`
}

// TaskView represents a task in the read model.
type TaskView struct {
	TaskID           string                           `json:"task_id"`
	UpdatedHLC       string                           `json:"updated_hlc"`
	Status           string                           `json:"status,omitempty"`
	WaitingReason    string                           `json:"waiting_reason,omitempty"`
	Binding          map[string]string                `json:"binding,omitempty"`
	Steps            []string                         `json:"steps,omitempty"`
	Artifacts        []string                         `json:"artifacts,omitempty"`
	Outbox           []store.PendingOutboxIndexRecord `json:"outbox,omitempty"`
	Usage            []string                         `json:"usage,omitempty"`
	OpenApprovalIDs  []string                         `json:"open_approvals,omitempty"`
	OpenHumanWaitIDs []string                         `json:"open_human_waits,omitempty"`
	CurrentExecution string                           `json:"current_step_execution_id,omitempty"`
	SourceRequestID  string                           `json:"source_request_id,omitempty"`
	WorkflowID       string                           `json:"workflow_id,omitempty"`
	RepoRef          string                           `json:"repo_ref,omitempty"`
}

// ScheduleView represents a schedule in the read model.
type ScheduleView struct {
	ScheduledTaskID      string    `json:"scheduled_task_id"`
	UpdatedHLC           string    `json:"updated_hlc"`
	Enabled              bool      `json:"enabled"`
	SpecKind             string    `json:"spec_kind,omitempty"`
	SpecText             string    `json:"spec_text,omitempty"`
	Timezone             string    `json:"timezone,omitempty"`
	TargetWorkflowID     string    `json:"target_workflow_id,omitempty"`
	TargetWorkflowSource string    `json:"target_workflow_source,omitempty"`
	TargetWorkflowRev    string    `json:"target_workflow_rev,omitempty"`
	ScheduleRevision     string    `json:"schedule_revision,omitempty"`
	NextFireAt           time.Time `json:"next_fire_at,omitempty"`
	LastFireAt           time.Time `json:"last_fire_at,omitempty"`
}

// ApprovalView represents an approval request in the read model.
type ApprovalView struct {
	ApprovalRequestID string    `json:"approval_request_id"`
	UpdatedHLC        string    `json:"updated_hlc"`
	TaskID            string    `json:"task_id"`
	StepExecutionID   string    `json:"step_execution_id"`
	GateType          string    `json:"gate_type,omitempty"`
	Status            string    `json:"status"`
	AllowedDecisions  []string  `json:"allowed_decisions,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	Note              string    `json:"note,omitempty"`
}

// HumanWaitView represents a human wait in the read model.
type HumanWaitView struct {
	HumanWaitID      string    `json:"human_wait_id"`
	UpdatedHLC       string    `json:"updated_hlc"`
	TaskID           string    `json:"task_id"`
	StepExecutionID  string    `json:"step_execution_id,omitempty"`
	WaitingReason    string    `json:"waiting_reason,omitempty"`
	Status           string    `json:"status"`
	AllowedDecisions []string  `json:"allowed_decisions,omitempty"`
	RewindTargets    []string  `json:"rewind_targets,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	Note             string    `json:"note,omitempty"`
}

// DeadletterView represents a deadletter in the read model.
type DeadletterView struct {
	DeadletterID  string    `json:"deadletter_id"`
	UpdatedHLC    string    `json:"updated_hlc"`
	SourceEventID string    `json:"source_event_id,omitempty"`
	FailureStage  string    `json:"failure_stage,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	Retryable     bool      `json:"retryable"`
	FirstFailedAt time.Time `json:"first_failed_at,omitempty"`
	LastFailedAt  time.Time `json:"last_failed_at,omitempty"`
}

// OpsOverviewView represents operations overview.
type OpsOverviewView struct {
	OpenRequests  int `json:"open_requests"`
	ActiveTasks   int `json:"active_tasks"`
	PendingOutbox int `json:"pending_outbox"`
	ApprovalQueue int `json:"approval_queue"`
	HumanQueue    int `json:"human_queue"`
}
