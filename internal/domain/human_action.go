package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

// HumanActionTokenClaims is an alias for HumanActionClaims for backward compatibility.
type HumanActionTokenClaims = HumanActionClaims

// HumanActionClaims represents JWT claims for human action tokens.
// Uses golang-jwt/jwt/v5.
type HumanActionClaims struct {
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
	Nonce             string    `json:"nonce"`
	ExpiresAt         time.Time `json:"exp"`
}

// Valid implements jwt.Claims interface for validation.
func (c HumanActionClaims) Valid() error {
	if c.ActionKind == "" {
		return fmt.Errorf("missing action_kind")
	}
	if c.DecisionHash == "" {
		return fmt.Errorf("missing decision_hash")
	}
	if c.Nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	return nil
}

// ToMapClaims converts to jwt.MapClaims for token signing.
func (c HumanActionClaims) ToMapClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"action_kind":         c.ActionKind,
		"request_id":          c.RequestID,
		"task_id":             c.TaskID,
		"reply_to_event_id":   c.ReplyToEventID,
		"approval_request_id": c.ApprovalRequestID,
		"human_wait_id":       c.HumanWaitID,
		"step_execution_id":   c.StepExecutionID,
		"target_step_id":      c.TargetStepID,
		"waiting_reason":      c.WaitingReason,
		"scheduled_task_id":   c.ScheduledTaskID,
		"control_object_ref":  c.ControlObjectRef,
		"workflow_object_ref": c.WorkflowObjectRef,
		"decision_hash":       c.DecisionHash,
		"nonce":               c.Nonce,
		"exp":                 c.ExpiresAt.Unix(),
	}
}

// HumanActionClaimsFromMap parses HumanActionClaims from jwt.MapClaims.
func HumanActionClaimsFromMap(claims jwt.MapClaims) (HumanActionClaims, error) {
	var c HumanActionClaims

	if v, ok := claims["action_kind"].(string); ok {
		c.ActionKind = v
	}
	if v, ok := claims["request_id"].(string); ok {
		c.RequestID = v
	}
	if v, ok := claims["task_id"].(string); ok {
		c.TaskID = v
	}
	if v, ok := claims["reply_to_event_id"].(string); ok {
		c.ReplyToEventID = v
	}
	if v, ok := claims["approval_request_id"].(string); ok {
		c.ApprovalRequestID = v
	}
	if v, ok := claims["human_wait_id"].(string); ok {
		c.HumanWaitID = v
	}
	if v, ok := claims["step_execution_id"].(string); ok {
		c.StepExecutionID = v
	}
	if v, ok := claims["target_step_id"].(string); ok {
		c.TargetStepID = v
	}
	if v, ok := claims["waiting_reason"].(string); ok {
		c.WaitingReason = v
	}
	if v, ok := claims["scheduled_task_id"].(string); ok {
		c.ScheduledTaskID = v
	}
	if v, ok := claims["control_object_ref"].(string); ok {
		c.ControlObjectRef = v
	}
	if v, ok := claims["workflow_object_ref"].(string); ok {
		c.WorkflowObjectRef = v
	}
	if v, ok := claims["decision_hash"].(string); ok {
		c.DecisionHash = v
	}
	if v, ok := claims["nonce"].(string); ok {
		c.Nonce = v
	}
	if exp, ok := claims["exp"].(float64); ok {
		c.ExpiresAt = time.Unix(int64(exp), 0)
	}

	return c, nil
}

func NormalizeHumanActionKind(v string) HumanActionKind {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, "-", "_")
	return HumanActionKind(s)
}

func ValidateHumanActionClaims(claims HumanActionClaims) error {
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
