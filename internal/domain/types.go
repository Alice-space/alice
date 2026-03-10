package domain

import "time"

type RequestStatus string
type TaskStatus string
type WaitingReason string
type PromotionResult string
type GateType string
type GateStatus string
type StepStatus string
type DispatchStatus string
type OutboxStatus string
type EventType string
type Role string

const (
	RequestStatusOpen     RequestStatus = "Open"
	RequestStatusAnswered RequestStatus = "Answered"
	RequestStatusPromoted RequestStatus = "Promoted"
	RequestStatusExpired  RequestStatus = "Expired"
)

const (
	TaskStatusNewTask      TaskStatus = "NewTask"
	TaskStatusActive       TaskStatus = "Active"
	TaskStatusWaitingHuman TaskStatus = "WaitingHuman"
	TaskStatusSucceeded    TaskStatus = "Succeeded"
	TaskStatusFailed       TaskStatus = "Failed"
	TaskStatusCancelled    TaskStatus = "Cancelled"
)

const (
	WaitingReasonInput        WaitingReason = "WaitingInput"
	WaitingReasonConfirmation WaitingReason = "WaitingConfirmation"
	WaitingReasonBudget       WaitingReason = "WaitingBudget"
	WaitingReasonRecovery     WaitingReason = "WaitingRecovery"
)

const (
	PromotionResultDirectAnswer PromotionResult = "direct_answer"
	PromotionResultPromote      PromotionResult = "promote"
	PromotionResultAskFollowup  PromotionResult = "ask_followup"
	PromotionResultEscalate     PromotionResult = "escalate_human"
)

const (
	GateTypeApproval     GateType = "approval"
	GateTypeConfirmation GateType = "confirmation"
	GateTypeBudget       GateType = "budget"
	GateTypeEvaluation   GateType = "evaluation"
)

const (
	GateStatusOpen       GateStatus = "open"
	GateStatusApproved   GateStatus = "approved"
	GateStatusRejected   GateStatus = "rejected"
	GateStatusExpired    GateStatus = "expired"
	GateStatusSuperseded GateStatus = "superseded"
)

const (
	StepStatusReady      StepStatus = "ready"
	StepStatusRunning    StepStatus = "running"
	StepStatusSucceeded  StepStatus = "succeeded"
	StepStatusFailed     StepStatus = "failed"
	StepStatusSuperseded StepStatus = "superseded"
	StepStatusCancelled  StepStatus = "cancelled"
)

const (
	DispatchStatusCreated    DispatchStatus = "created"
	DispatchStatusDispatched DispatchStatus = "dispatched"
	DispatchStatusRunning    DispatchStatus = "running"
	DispatchStatusCompleted  DispatchStatus = "completed"
	DispatchStatusFailed     DispatchStatus = "failed"
	DispatchStatusCancelled  DispatchStatus = "cancelled"
	DispatchStatusExpired    DispatchStatus = "expired"
)

const (
	OutboxStatusPending     OutboxStatus = "pending"
	OutboxStatusDispatching OutboxStatus = "dispatching"
	OutboxStatusSucceeded   OutboxStatus = "succeeded"
	OutboxStatusRetryWait   OutboxStatus = "retry_wait"
	OutboxStatusDead        OutboxStatus = "dead"
)

const (
	RoleReception Role = "reception"
	RoleLeader    Role = "leader"
	RoleHelper    Role = "helper"
	RoleWorker    Role = "worker"
	RoleReviewer  Role = "reviewer"
	RoleEvaluator Role = "evaluator"
)

const (
	AggregateKindRequest = "request"
	AggregateKindTask    = "task"
	AggregateKindOther   = "other"
)

var terminalTaskStatuses = map[TaskStatus]struct{}{
	TaskStatusSucceeded: {},
	TaskStatusFailed:    {},
	TaskStatusCancelled: {},
}

func (s TaskStatus) IsTerminal() bool {
	_, ok := terminalTaskStatuses[s]
	return ok
}

func NowBucket5m(t time.Time) time.Time {
	return t.UTC().Truncate(5 * time.Minute)
}
