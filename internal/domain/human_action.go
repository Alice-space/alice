package domain

import (
	"fmt"
	"strings"
	"time"
)

type HumanActionKind string

const (
	HumanActionApprove        HumanActionKind = "approve"
	HumanActionReject         HumanActionKind = "reject"
	HumanActionConfirm        HumanActionKind = "confirm"
	HumanActionCancel         HumanActionKind = "cancel"
	HumanActionProvideInput   HumanActionKind = "provide_input"
	HumanActionResumeBudget   HumanActionKind = "resume_budget"
	HumanActionResumeRecovery HumanActionKind = "resume_recovery"
	HumanActionRewind         HumanActionKind = "rewind"
)

type HumanActionTokenClaims struct {
	ActionKind        string    `json:"action_kind"`
	RequestID         string    `json:"request_id"`
	TaskID            string    `json:"task_id"`
	ReplyToEventID    string    `json:"reply_to_event_id"`
	ApprovalRequestID string    `json:"approval_request_id"`
	HumanWaitID       string    `json:"human_wait_id"`
	StepExecutionID   string    `json:"step_execution_id"`
	TargetStepID      string    `json:"target_step_id"`
	WaitingReason     string    `json:"waiting_reason"`
	ScheduledTaskID   string    `json:"scheduled_task_id"`
	ControlObjectRef  string    `json:"control_object_ref"`
	WorkflowObjectRef string    `json:"workflow_object_ref"`
	DecisionHash      string    `json:"decision_hash"`
	ExpiresAt         time.Time `json:"expires_at"`
	Nonce             string    `json:"nonce"`
}

func NormalizeHumanActionKind(v string) HumanActionKind {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, "-", "_")
	return HumanActionKind(s)
}

func ValidateHumanActionClaims(claims HumanActionTokenClaims) error {
	kind := NormalizeHumanActionKind(claims.ActionKind)
	if kind == "" {
		return fmt.Errorf("missing action_kind")
	}
	switch kind {
	case HumanActionApprove, HumanActionReject, HumanActionConfirm:
		if claims.ApprovalRequestID == "" || claims.TaskID == "" || claims.StepExecutionID == "" {
			return fmt.Errorf("%s requires approval_request_id/task_id/step_execution_id", kind)
		}
	case HumanActionResumeBudget:
		if claims.ApprovalRequestID == "" || claims.TaskID == "" || claims.StepExecutionID == "" {
			return fmt.Errorf("%s requires approval_request_id/task_id/step_execution_id", kind)
		}
		if claims.WaitingReason != string(WaitingReasonBudget) {
			return fmt.Errorf("%s requires waiting_reason=%s", kind, WaitingReasonBudget)
		}
	case HumanActionProvideInput:
		if claims.WaitingReason != string(WaitingReasonInput) {
			return fmt.Errorf("%s requires waiting_reason=%s", kind, WaitingReasonInput)
		}
		if claims.TaskID == "" && claims.ReplyToEventID == "" {
			return fmt.Errorf("%s requires task_id or reply_to_event_id", kind)
		}
		if claims.TaskID != "" && claims.HumanWaitID == "" {
			return fmt.Errorf("%s on task route requires human_wait_id", kind)
		}
	case HumanActionResumeRecovery:
		if claims.HumanWaitID == "" || claims.TaskID == "" || claims.StepExecutionID == "" {
			return fmt.Errorf("%s requires human_wait_id/task_id/step_execution_id", kind)
		}
		if claims.WaitingReason != string(WaitingReasonRecovery) {
			return fmt.Errorf("%s requires waiting_reason=%s", kind, WaitingReasonRecovery)
		}
	case HumanActionRewind:
		if claims.TaskID == "" || claims.StepExecutionID == "" || claims.TargetStepID == "" {
			return fmt.Errorf("%s requires task_id/step_execution_id/target_step_id", kind)
		}
	case HumanActionCancel:
		if claims.TaskID == "" {
			return fmt.Errorf("%s requires task_id", kind)
		}
	default:
		return fmt.Errorf("unsupported action_kind=%s", claims.ActionKind)
	}
	return nil
}
