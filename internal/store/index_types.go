package store

import (
	"context"
	"time"

	"alice/internal/domain"
)

// Bucket names for BoltDB
const (
	bucketRequestsByRoute = "requests_by_route"
	bucketTasksByRoute    = "tasks_by_route"
	bucketOpenRequests    = "open_requests"
	bucketActiveTasks     = "active_tasks"
	bucketPendingOutbox   = "pending_outbox"
	bucketDedupeWindow    = "dedupe_window"
	bucketScheduleSources = "schedule_sources"
	bucketOutboxByRemote  = "outbox_by_remote"
	bucketApprovalQueue   = "approval_queue"
	bucketHumanQueue      = "human_action_queue"
	bucketOpsViews        = "ops_views"
)

// Buckets that must be updated synchronously
var criticalBuckets = []string{
	bucketRequestsByRoute,
	bucketTasksByRoute,
	bucketOpenRequests,
	bucketActiveTasks,
	bucketPendingOutbox,
	bucketDedupeWindow,
	bucketScheduleSources,
	bucketOutboxByRemote,
}

// Buckets that can be updated asynchronously
var laggingBuckets = []string{
	bucketApprovalQueue,
	bucketHumanQueue,
	bucketOpsViews,
}

// CriticalIndexStore for critical path operations
type CriticalIndexStore interface {
	ApplyCritical(ctx context.Context, events []domain.EventEnvelope) error
	RebuildCritical(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}

// ProjectionStore for lagging projections
type ProjectionStore interface {
	ApplyLagging(ctx context.Context, events []domain.EventEnvelope) error
	RebuildLagging(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}

// PendingOutboxIndexRecord represents a pending outbox action
type PendingOutboxIndexRecord struct {
	ActionID        string    `json:"action_id"`
	TaskID          string    `json:"task_id"`
	Domain          string    `json:"domain"`
	ActionType      string    `json:"action_type"`
	TargetRef       string    `json:"target_ref"`
	IdempotencyKey  string    `json:"idempotency_key"`
	RemoteRequestID string    `json:"remote_request_id"`
	PayloadRef      string    `json:"payload_ref"`
	AttemptCount    uint32    `json:"attempt_count"`
	NextAttemptAt   time.Time `json:"next_attempt_at"`
}

// DedupeRecord for idempotency
type DedupeRecord struct {
	CommitHLC       string    `json:"commit_hlc"`
	EventID         string    `json:"event_id,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	TaskID          string    `json:"task_id,omitempty"`
	RouteTargetKind string    `json:"route_target_kind,omitempty"`
	RouteTargetID   string    `json:"route_target_id,omitempty"`
	ReceivedAt      time.Time `json:"received_at,omitempty"`
}

// ScheduleSourceIndexRecord for schedule sources
type ScheduleSourceIndexRecord struct {
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
	LastFireAt           time.Time `json:"last_fire_at"`
}

// OpsOverview for operations dashboard
type OpsOverview struct {
	OpenRequests  int `json:"open_requests"`
	ActiveTasks   int `json:"active_tasks"`
	PendingOutbox int `json:"pending_outbox"`
	ApprovalQueue int `json:"approval_queue"`
	HumanQueue    int `json:"human_queue"`
}
