package domain

import "time"

// ApprovalRequestView represents an approval request in read models
type ApprovalRequestView struct {
	ApprovalRequestID string    `json:"approval_request_id"`
	TaskID            string    `json:"task_id"`
	StepExecutionID   string    `json:"step_execution_id"`
	GateType          string    `json:"gate_type"`
	Status            string    `json:"status"`
	DeadlineAt        time.Time `json:"deadline_at,omitempty"`
}

// HumanWaitView represents a human wait in read models
type HumanWaitView struct {
	HumanWaitID     string    `json:"human_wait_id"`
	TaskID          string    `json:"task_id"`
	StepExecutionID string    `json:"step_execution_id,omitempty"`
	WaitingReason   string    `json:"waiting_reason"`
	Status          string    `json:"status"`
	DeadlineAt      time.Time `json:"deadline_at,omitempty"`
}

// ScheduleSourceView represents a schedule source in read models
type ScheduleSourceView struct {
	ScheduledTaskID      string    `json:"scheduled_task_id"`
	SpecKind             string    `json:"spec_kind"`
	SpecText             string    `json:"spec_text"`
	Timezone             string    `json:"timezone"`
	ScheduleRevision     string    `json:"schedule_revision"`
	TargetWorkflowID     string    `json:"target_workflow_id"`
	TargetWorkflowSource string    `json:"target_workflow_source"`
	TargetWorkflowRev    string    `json:"target_workflow_rev"`
	Enabled              bool      `json:"enabled"`
	NextFireAt           time.Time `json:"next_fire_at,omitempty"`
	LastFireAt           time.Time `json:"last_fire_at,omitempty"`
}

// ScheduleFireView represents a schedule fire
type ScheduleFireView struct {
	FireID             string    `json:"fire_id"`
	ScheduledTaskID    string    `json:"scheduled_task_id"`
	ScheduledForWindow time.Time `json:"scheduled_for_window"`
}
