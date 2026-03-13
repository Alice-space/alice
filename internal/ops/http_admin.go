package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"alice/internal/domain"

	"github.com/gin-gonic/gin"
)

var submitEventKindSchema = map[string]string{
	"web_form_message":      "web-form-message.v1",
	"repo_issue_comment":    "repo-issue-comment.v1",
	"repo_pr_comment":       "repo-pr-comment.v1",
	"control_plane_message": "control-plane-message.v1",
}

var submitEventKindSource = map[string]string{
	"web_form_message":      "direct_input",
	"repo_issue_comment":    "repo_comment",
	"repo_pr_comment":       "repo_comment",
	"control_plane_message": "control_plane",
}

var submitEventForbiddenFields = map[string]struct{}{
	"event_id":                 {},
	"received_at":              {},
	"verified":                 {},
	"fire_id":                  {},
	"source_schedule_revision": {},
	"approval_request_id":      {},
	"human_wait_id":            {},
	"request_id":               {},
	"task_id":                  {},
	"step_execution_id":        {},
	"action_kind":              {},
	"event_type":               {},
	"source_kind":              {},
	"transport_kind":           {},
}

var routeCriticalFields = []string{
	"reply_to_event_id",
	"scheduled_task_id",
	"control_object_ref",
	"workflow_object_ref",
}

func (m *HTTPManager) wrapAdminHook(hook func(r *http.Request) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		actionID := resolveAdminActionIDGin(c)
		ctx := context.WithValue(c.Request.Context(), adminActionIDKey{}, actionID)
		c.Request = c.Request.WithContext(ctx)
		if hook != nil {
			if err := hook(c.Request); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		commitHLC, _ := m.currentVisibleHLC(c.Request.Context())
		c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID, CommitHLC: commitHLC})
	}
}

func (m *HTTPManager) handleReplayFrom(c *gin.Context) {
	hlc := strings.TrimSpace(c.Param("hlc"))
	if hlc == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from hlc is required"})
		return
	}
	actionID := resolveAdminActionIDGin(c)
	if m.hooks.ReplayFromHLC != nil {
		if err := m.hooks.ReplayFromHLC(c.Request, hlc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	commitHLC, _ := m.currentVisibleHLC(c.Request.Context())
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID, CommitHLC: commitHLC})
}

func (m *HTTPManager) handleDeadletterRedrive(c *gin.Context) {
	deadletterID := strings.TrimSpace(c.Param("id"))
	if deadletterID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deadletter id is required"})
		return
	}
	actionID := resolveAdminActionIDGin(c)
	if m.hooks.RedriveDeadletter == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "deadletter redrive is not configured"})
		return
	}
	model, err := m.loadReadModel(c.Request.Context(), "", 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.deadletterByID(deadletterID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "deadletter not found"})
		return
	}
	if !item.Retryable {
		c.JSON(http.StatusConflict, gin.H{"error": "deadletter is not retryable"})
		return
	}
	if err := m.hooks.RedriveDeadletter(c.Request, deadletterID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	commitHLC, _ := m.currentVisibleHLC(c.Request.Context())
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID, CommitHLC: commitHLC})
}

func (m *HTTPManager) handleSubmitEvent(c *gin.Context) {
	if !m.config.AdminEventInjectionEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin submit events is disabled"})
		return
	}
	if m.runtime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime is unavailable"})
		return
	}

	raw, err := readJSONMapGin(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for key := range raw {
		if _, forbidden := submitEventForbiddenFields[key]; forbidden {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "forbidden field: " + key})
			return
		}
	}

	rawJSON, err := json.Marshal(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var in domain.ExternalEventInput
	if err := json.Unmarshal(rawJSON, &in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	kind := normalizeInputKind(in.InputKind)
	expectedSchema, ok := submitEventKindSchema[kind]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported input_kind: " + in.InputKind})
		return
	}
	if strings.TrimSpace(in.BodySchemaID) != expectedSchema {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "body_schema_id does not match input_kind"})
		return
	}

	body, err := decodeBodyObject(raw["body"])
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	for _, field := range routeCriticalFields {
		if _, exists := body[field]; exists {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "route-critical fields must be top-level only: " + field})
			return
		}
	}
	if err := validateSubmitEventBody(kind, body); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	actionID := resolveAdminActionIDGin(c)
	actor := adminActor(in.ActorRef)
	evt := domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        submitEventKindSource[kind],
		TransportKind:     "cli_admin_injected",
		SourceRef:         defaultString(in.SourceRef, "cli:"+actor),
		ActorRef:          actor,
		ReplyToEventID:    strings.TrimSpace(in.ReplyToEventID),
		ConversationID:    strings.TrimSpace(in.ConversationID),
		ThreadID:          strings.TrimSpace(in.ThreadID),
		RepoRef:           strings.TrimSpace(in.RepoRef),
		IssueRef:          strings.TrimSpace(in.IssueRef),
		PRRef:             strings.TrimSpace(in.PRRef),
		ScheduledTaskID:   strings.TrimSpace(in.ScheduledTaskID),
		ControlObjectRef:  strings.TrimSpace(in.ControlObjectRef),
		WorkflowObjectRef: strings.TrimSpace(in.WorkflowObjectRef),
		IdempotencyKey:    strings.TrimSpace(in.IdempotencyKey),
		TraceID:           strings.TrimSpace(in.TraceID),
		CausationID:       actionID,
		Verified:          true,
		InputSchemaID:     strings.TrimSpace(in.BodySchemaID),
		PayloadRef:        "admin_submit_event:" + strings.TrimSpace(in.BodySchemaID),
		ReceivedAt:        time.Now().UTC(),
	}
	if evt.ConversationID != "" && evt.ThreadID == "" {
		evt.ThreadID = "root"
	}
	if kind == "repo_issue_comment" || kind == "repo_pr_comment" {
		evt.CommentRef = mapStringField(body, "comment_ref")
	}

	if err := m.recordAdminAudit(c.Request.Context(), c.Request, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "submit_event",
		ActorRef:      actor,
		TargetKind:    "external_event",
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := m.runtime.IngestExternalEvent(c.Request.Context(), evt, m.reception)
	if err != nil {
		writeAdminErrorGin(c, err)
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleSubmitFire(c *gin.Context) {
	if !m.config.AdminScheduleFireReplayEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin submit fires is disabled"})
		return
	}
	if m.runtime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime is unavailable"})
		return
	}

	var in domain.ScheduleFireReplayRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in.ScheduledTaskID = strings.TrimSpace(in.ScheduledTaskID)
	if in.ScheduledTaskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scheduled_task_id is required"})
		return
	}
	window := in.ScheduledForWindow.UTC()
	if window.IsZero() {
		window = time.Now().UTC().Truncate(time.Minute)
	}
	if _, err := m.runtime.RequireEnabledScheduleSource(c.Request.Context(), in.ScheduledTaskID); err != nil {
		writeAdminErrorGin(c, err)
		return
	}

	actionID := resolveAdminActionIDGin(c)
	actor := adminActor(in.ActorRef)
	if err := m.recordAdminAudit(c.Request.Context(), c.Request, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "submit_fire",
		ActorRef:      actor,
		TargetKind:    "scheduled_task",
		TargetID:      in.ScheduledTaskID,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = domain.ComputeFireID(in.ScheduledTaskID, window)
	}
	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeScheduleTriggered,
		SourceKind:      "scheduler",
		TransportKind:   "cli_admin_fire_replay",
		SourceRef:       window.Format(time.RFC3339),
		ActorRef:        actor,
		ScheduledTaskID: in.ScheduledTaskID,
		IdempotencyKey:  idempotencyKey,
		TraceID:         strings.TrimSpace(in.TraceID),
		CausationID:     actionID,
		Verified:        true,
		PayloadRef:      "admin_submit_fire",
		ReceivedAt:      time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(c.Request.Context(), evt, nil)
	if err != nil {
		writeAdminErrorGin(c, err)
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleResolveApproval(c *gin.Context) {
	if m.runtime == nil || m.indexes == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime is unavailable"})
		return
	}

	var in domain.ResolveApprovalRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	in.ApprovalRequestID = strings.TrimSpace(in.ApprovalRequestID)
	if in.ApprovalRequestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "approval_request_id is required"})
		return
	}
	approval, ok, err := m.indexes.GetApprovalRequest(c.Request.Context(), in.ApprovalRequestID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		writeAdminErrorGin(c, fmt.Errorf("approval request is not active: %s", in.ApprovalRequestID))
		return
	}

	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(approval.TaskID)
	}
	if taskID == "" {
		writeAdminErrorGin(c, fmt.Errorf("task_id is required"))
		return
	}
	if approval.TaskID != "" && approval.TaskID != taskID {
		writeAdminErrorGin(c, fmt.Errorf("approval request task mismatch: approval_task=%s route_task=%s", approval.TaskID, taskID))
		return
	}
	stepExecutionID := strings.TrimSpace(in.StepExecutionID)
	if stepExecutionID == "" {
		stepExecutionID = strings.TrimSpace(approval.StepExecutionID)
	}
	if approval.StepExecutionID != "" && stepExecutionID != "" && approval.StepExecutionID != stepExecutionID {
		writeAdminErrorGin(c, fmt.Errorf("step execution mismatch for approval action"))
		return
	}

	decision := domain.NormalizeHumanActionKind(in.Decision)
	if !approvalDecisionAllowed(decision, approval.GateType) {
		writeAdminErrorGin(c, fmt.Errorf("action %s is not allowed for gate_type=%s", decision, approval.GateType))
		return
	}

	actionID := resolveAdminActionIDGin(c)
	actor := adminActor(in.ActorRef)
	if err := m.recordAdminAudit(c.Request.Context(), c.Request, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "resolve_approval",
		ActorRef:      actor,
		TargetKind:    "approval_request",
		TargetID:      in.ApprovalRequestID,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	evt := domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "human_action",
		TransportKind:     "cli_admin",
		SourceRef:         "cli:" + actor,
		ActorRef:          actor,
		ActionKind:        string(decision),
		TaskID:            taskID,
		ApprovalRequestID: in.ApprovalRequestID,
		StepExecutionID:   stepExecutionID,
		IdempotencyKey:    strings.TrimSpace(in.IdempotencyKey),
		TraceID:           strings.TrimSpace(in.TraceID),
		CausationID:       actionID,
		Verified:          true,
		PayloadRef:        "admin_resolve_approval",
		ReceivedAt:        time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(c.Request.Context(), evt, nil)
	if err != nil {
		writeAdminErrorGin(c, err)
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleResolveWait(c *gin.Context) {
	if m.runtime == nil || m.indexes == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime is unavailable"})
		return
	}

	var in domain.ResolveHumanWaitRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	in.HumanWaitID = strings.TrimSpace(in.HumanWaitID)
	if in.HumanWaitID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "human_wait_id is required"})
		return
	}

	wait, ok, err := m.indexes.GetHumanWait(c.Request.Context(), in.HumanWaitID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		writeAdminErrorGin(c, fmt.Errorf("human wait is not active: %s", in.HumanWaitID))
		return
	}

	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(wait.TaskID)
	}
	if taskID == "" {
		writeAdminErrorGin(c, fmt.Errorf("task_id is required"))
		return
	}
	if wait.TaskID != "" && wait.TaskID != taskID {
		writeAdminErrorGin(c, fmt.Errorf("human wait task mismatch: wait_task=%s route_task=%s", wait.TaskID, taskID))
		return
	}

	stepExecutionID := strings.TrimSpace(in.StepExecutionID)
	if stepExecutionID == "" {
		stepExecutionID = strings.TrimSpace(wait.StepExecutionID)
	}
	if wait.StepExecutionID != "" && stepExecutionID != "" && wait.StepExecutionID != stepExecutionID {
		writeAdminErrorGin(c, fmt.Errorf("step execution mismatch for human wait action"))
		return
	}

	if in.WaitingReason != "" && !strings.EqualFold(in.WaitingReason, wait.WaitingReason) {
		writeAdminErrorGin(c, fmt.Errorf("waiting_reason mismatch"))
		return
	}
	decision := domain.NormalizeHumanActionKind(in.Decision)
	if !waitDecisionAllowed(wait.WaitingReason, decision) {
		writeAdminErrorGin(c, fmt.Errorf("action %s is not allowed for waiting_reason=%s", decision, wait.WaitingReason))
		return
	}
	targetStepID := strings.TrimSpace(in.TargetStepID)
	if decision == domain.HumanActionRewind && targetStepID == "" {
		writeAdminErrorGin(c, fmt.Errorf("rewind requires target_step_id"))
		return
	}

	trimmedPatch := strings.TrimSpace(string(in.InputPatch))
	if decision == domain.HumanActionProvideInput && trimmedPatch == "" {
		writeAdminErrorGin(c, fmt.Errorf("input_patch is required for provide_input"))
		return
	}
	if trimmedPatch != "" {
		if _, err := domain.ApplyHumanWaitInputPatch(wait, in.InputPatch); err != nil {
			writeAdminErrorGin(c, err)
			return
		}
	}

	actionID := resolveAdminActionIDGin(c)
	actor := adminActor(in.ActorRef)
	if err := m.recordAdminAudit(c.Request.Context(), c.Request, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "resolve_wait",
		ActorRef:      actor,
		TargetKind:    "human_wait",
		TargetID:      in.HumanWaitID,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human_action",
		TransportKind:   "cli_admin",
		SourceRef:       "cli:" + actor,
		ActorRef:        actor,
		ActionKind:      string(decision),
		TaskID:          taskID,
		HumanWaitID:     in.HumanWaitID,
		StepExecutionID: stepExecutionID,
		TargetStepID:    targetStepID,
		WaitingReason:   defaultString(strings.TrimSpace(in.WaitingReason), strings.TrimSpace(wait.WaitingReason)),
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		TraceID:         strings.TrimSpace(in.TraceID),
		CausationID:     actionID,
		Verified:        true,
		InputSchemaID:   strings.TrimSpace(wait.InputSchemaID),
		InputPatch:      in.InputPatch,
		PayloadRef:      "admin_resolve_wait",
		ReceivedAt:      time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(c.Request.Context(), evt, nil)
	if err != nil {
		writeAdminErrorGin(c, err)
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleTaskCancel(c *gin.Context) {
	if m.runtime == nil || m.indexes == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "runtime is unavailable"})
		return
	}

	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id is required"})
		return
	}
	reqBody, err := readCancelTaskRequestGin(c, taskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	active, err := m.indexes.IsTaskActive(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !active {
		writeAdminErrorGin(c, fmt.Errorf("%w: task_id=%s", domain.ErrTerminalObjectNotRoutable, taskID))
		return
	}

	actionID := resolveAdminActionIDGin(c)
	actor := adminActor(reqBody.ActorRef)
	if err := m.recordAdminAudit(c.Request.Context(), c.Request, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "cancel_task",
		ActorRef:      actor,
		TargetKind:    "task",
		TargetID:      taskID,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human_action",
		TransportKind:   "cli_admin",
		SourceRef:       "admin:task_cancel:" + taskID,
		ActorRef:        actor,
		ActionKind:      string(domain.HumanActionCancel),
		TaskID:          taskID,
		StepExecutionID: strings.TrimSpace(reqBody.StepExecutionID),
		IdempotencyKey:  strings.TrimSpace(reqBody.IdempotencyKey),
		TraceID:         strings.TrimSpace(reqBody.TraceID),
		CausationID:     actionID,
		Verified:        true,
		PayloadRef:      "admin_cancel_task",
		ReceivedAt:      time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(c.Request.Context(), evt, nil)
	if err != nil {
		writeAdminErrorGin(c, err)
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func normalizeInputKind(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	return strings.ReplaceAll(v, "-", "_")
}

func decodeBodyObject(raw json.RawMessage) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("body must be a json object")
	}
	return obj, nil
}

func validateSubmitEventBody(kind string, body map[string]any) error {
	switch kind {
	case "web_form_message", "control_plane_message":
		if strings.TrimSpace(mapStringField(body, "text")) == "" {
			return fmt.Errorf("body.text is required")
		}
	case "repo_issue_comment", "repo_pr_comment":
		if strings.TrimSpace(mapStringField(body, "comment_text")) == "" {
			return fmt.Errorf("body.comment_text is required")
		}
	default:
		return fmt.Errorf("unsupported input_kind=%s", kind)
	}
	return nil
}

func mapStringField(obj map[string]any, key string) string {
	raw, ok := obj[key]
	if !ok {
		return ""
	}
	v, _ := raw.(string)
	return strings.TrimSpace(v)
}

func defaultString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func adminActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "admin"
	}
	return actor
}

func approvalDecisionAllowed(decision domain.HumanActionKind, gateType string) bool {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case "approval":
		return decision == domain.HumanActionApprove || decision == domain.HumanActionReject
	case "confirmation":
		return decision == domain.HumanActionConfirm || decision == domain.HumanActionReject
	case "budget":
		return decision == domain.HumanActionResumeBudget || decision == domain.HumanActionReject
	case "evaluation":
		return decision == domain.HumanActionConfirm || decision == domain.HumanActionReject
	default:
		return false
	}
}

func waitDecisionAllowed(waitingReason string, decision domain.HumanActionKind) bool {
	switch strings.ToLower(strings.TrimSpace(waitingReason)) {
	case strings.ToLower(string(domain.WaitingReasonInput)):
		return decision == domain.HumanActionProvideInput
	case strings.ToLower(string(domain.WaitingReasonRecovery)):
		return decision == domain.HumanActionResumeRecovery || decision == domain.HumanActionRewind
	default:
		return decision != ""
	}
}
