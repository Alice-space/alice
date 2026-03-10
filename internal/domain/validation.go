package domain

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidDecision           = errors.New("invalid promotion decision")
	ErrInvalidPromoteAndBind     = errors.New("invalid promote-and-bind command")
	ErrTerminalObjectNotRoutable = errors.New("terminal object should not be routable")
)

func ValidatePromotionDecision(d PromotionDecision) error {
	if d.DecisionID == "" || d.RequestID == "" {
		return fmt.Errorf("%w: missing decision_id/request_id", ErrInvalidDecision)
	}
	if d.Result == PromotionResultPromote && d.SelectedWorkflowID == "" && len(d.ProposedWorkflowIDs) == 0 {
		return fmt.Errorf("%w: promote requires workflow candidate", ErrInvalidDecision)
	}
	if d.Confidence < 0 || d.Confidence > 1 {
		return fmt.Errorf("%w: confidence out of range", ErrInvalidDecision)
	}
	return nil
}

func ValidatePromoteAndBindCommand(cmd PromoteAndBindWorkflowCommand) error {
	if cmd.RequestID == "" || cmd.TaskID == "" || cmd.BindingID == "" {
		return fmt.Errorf("%w: missing request/task/binding id", ErrInvalidPromoteAndBind)
	}
	if cmd.WorkflowID == "" || cmd.WorkflowRev == "" || cmd.WorkflowSource == "" {
		return fmt.Errorf("%w: missing workflow identifiers", ErrInvalidPromoteAndBind)
	}
	if cmd.ManifestDigest == "" || cmd.EntryStepID == "" {
		return fmt.Errorf("%w: missing manifest digest/entry step", ErrInvalidPromoteAndBind)
	}
	return nil
}

func OutboxIdempotencyKey(taskID, causationID, actionType string) string {
	return fmt.Sprintf("%s:%s:%s", taskID, causationID, actionType)
}
