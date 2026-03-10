package domain

import (
	"encoding/json"
	"time"
)

type WriteAcceptedResponse struct {
	Accepted        bool   `json:"accepted"`
	AdminActionID   string `json:"admin_action_id,omitempty"`
	EventID         string `json:"event_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	RouteTargetKind string `json:"route_target_kind,omitempty"`
	RouteTargetID   string `json:"route_target_id,omitempty"`
	CommitHLC       string `json:"commit_hlc"`
}

type GetResponse[T any] struct {
	Item       T      `json:"item"`
	VisibleHLC string `json:"visible_hlc"`
}

type ListResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	OrderBy    string `json:"order_by"`
	VisibleHLC string `json:"visible_hlc"`
}

type CLIMessageSubmitRequest struct {
	Text           string `json:"text"`
	ActorRef       string `json:"actor_ref,omitempty"`
	SourceRef      string `json:"source_ref,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	ThreadID       string `json:"thread_id,omitempty"`
	RepoRef        string `json:"repo_ref,omitempty"`
	ReplyToEventID string `json:"reply_to_event_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
}

type ExternalEventInput struct {
	InputKind         string          `json:"input_kind"`
	ActorRef          string          `json:"actor_ref,omitempty"`
	SourceRef         string          `json:"source_ref,omitempty"`
	IdempotencyKey    string          `json:"idempotency_key,omitempty"`
	ConversationID    string          `json:"conversation_id,omitempty"`
	ThreadID          string          `json:"thread_id,omitempty"`
	RepoRef           string          `json:"repo_ref,omitempty"`
	IssueRef          string          `json:"issue_ref,omitempty"`
	PRRef             string          `json:"pr_ref,omitempty"`
	ReplyToEventID    string          `json:"reply_to_event_id,omitempty"`
	ScheduledTaskID   string          `json:"scheduled_task_id,omitempty"`
	ControlObjectRef  string          `json:"control_object_ref,omitempty"`
	WorkflowObjectRef string          `json:"workflow_object_ref,omitempty"`
	BodySchemaID      string          `json:"body_schema_id"`
	Body              json.RawMessage `json:"body,omitempty"`
	TraceID           string          `json:"trace_id,omitempty"`
}

type ScheduleFireReplayRequest struct {
	ScheduledTaskID    string    `json:"scheduled_task_id"`
	ScheduledForWindow time.Time `json:"scheduled_for_window"`
	ActorRef           string    `json:"actor_ref,omitempty"`
	IdempotencyKey     string    `json:"idempotency_key,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	TraceID            string    `json:"trace_id,omitempty"`
}

type ResolveApprovalRequest struct {
	ApprovalRequestID string `json:"approval_request_id"`
	TaskID            string `json:"task_id"`
	StepExecutionID   string `json:"step_execution_id"`
	Decision          string `json:"decision"`
	ActorRef          string `json:"actor_ref,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	Note              string `json:"note,omitempty"`
	TraceID           string `json:"trace_id,omitempty"`
}

type ResolveHumanWaitRequest struct {
	HumanWaitID     string          `json:"human_wait_id"`
	TaskID          string          `json:"task_id"`
	StepExecutionID string          `json:"step_execution_id,omitempty"`
	WaitingReason   string          `json:"waiting_reason"`
	Decision        string          `json:"decision"`
	TargetStepID    string          `json:"target_step_id,omitempty"`
	ActorRef        string          `json:"actor_ref,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	InputPatch      json.RawMessage `json:"input_patch,omitempty"`
	Note            string          `json:"note,omitempty"`
	TraceID         string          `json:"trace_id,omitempty"`
}

type CancelTaskRequest struct {
	TaskID          string `json:"task_id"`
	StepExecutionID string `json:"step_execution_id,omitempty"`
	ActorRef        string `json:"actor_ref,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
	Reason          string `json:"reason,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
}
