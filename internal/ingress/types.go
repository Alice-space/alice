package ingress

import (
	"encoding/json"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/feishu"
)

// NormalizedEvent represents a normalized event from any source.
type NormalizedEvent struct {
	EventType         domain.EventType `json:"event_type"`
	SourceKind        string           `json:"source_kind"`
	TransportKind     string           `json:"transport_kind"`
	SourceRef         string           `json:"source_ref"`
	ActorRef          string           `json:"actor_ref"`
	ActionKind        string           `json:"action_kind"`
	RequestID         string           `json:"request_id"`
	TaskID            string           `json:"task_id"`
	ApprovalRequestID string           `json:"approval_request_id"`
	HumanWaitID       string           `json:"human_wait_id"`
	StepExecutionID   string           `json:"step_execution_id"`
	TargetStepID      string           `json:"target_step_id"`
	WaitingReason     string           `json:"waiting_reason"`
	ReplyToEventID    string           `json:"reply_to_event_id"`
	ConversationID    string           `json:"conversation_id"`
	ThreadID          string           `json:"thread_id"`
	RepoRef           string           `json:"repo_ref"`
	IssueRef          string           `json:"issue_ref"`
	PRRef             string           `json:"pr_ref"`
	CommentRef        string           `json:"comment_ref"`
	ScheduledTaskID   string           `json:"scheduled_task_id"`
	ControlObjectRef  string           `json:"control_object_ref"`
	WorkflowObjectRef string           `json:"workflow_object_ref"`
	CoalescingKey     string           `json:"coalescing_key"`
	PayloadRef        string           `json:"payload_ref"`
	Verified          bool             `json:"verified"`
	IdempotencyKey    string           `json:"idempotency_key"`
	DecisionHash      string           `json:"decision_hash"`
	TraceID           string           `json:"trace_id,omitempty"`
	InputSchemaID     string           `json:"input_schema_id,omitempty"`
	InputPatch        json.RawMessage  `json:"input_patch,omitempty"`
}

// SchedulerFireRequest represents a scheduler fire request.
type SchedulerFireRequest struct {
	ScheduledTaskID    string    `json:"scheduled_task_id"`
	ScheduledForWindow time.Time `json:"scheduled_for_window"`
}

// WebhookAuthConfig holds webhook authentication configuration.
type WebhookAuthConfig struct {
	GitHubSecret    string
	GitLabSecret    string
	SchedulerSecret string
}

// HTTPIngress handles HTTP ingress for events.
type HTTPIngress struct {
	runtime           *bus.Runtime
	reception         bus.Reception
	feishu            *feishu.Service
	humanActionSecret []byte
	gitHubSecret      []byte
	gitLabSecret      string
	schedulerSecret   string
}

// NewHTTPIngress creates a new HTTPIngress.
func NewHTTPIngress(runtime *bus.Runtime, reception bus.Reception, humanActionSecret string, feishuService *feishu.Service, webhookAuth ...WebhookAuthConfig) *HTTPIngress {
	ing := &HTTPIngress{
		runtime:           runtime,
		reception:         reception,
		feishu:            feishuService,
		humanActionSecret: []byte(humanActionSecret),
	}
	if len(webhookAuth) > 0 {
		ing.gitHubSecret = []byte(webhookAuth[0].GitHubSecret)
		ing.gitLabSecret = webhookAuth[0].GitLabSecret
		ing.schedulerSecret = webhookAuth[0].SchedulerSecret
	}
	return ing
}
