package ops

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"

	"github.com/gin-gonic/gin"
)

func resolveAdminActionIDGin(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-Admin-Action-ID")); v != "" {
		return v
	}
	return newAdminActionID()
}

func newAdminActionID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("admin_%d", time.Now().UTC().UnixNano())
	}
	return "admin_" + hex.EncodeToString(buf[:])
}

func parseReadWaitGin(c *gin.Context) (string, time.Duration) {
	minHLC := strings.TrimSpace(c.Query("min_hlc"))
	waitRaw := strings.TrimSpace(c.Query("wait_timeout_ms"))
	if waitRaw == "" {
		return minHLC, 0
	}
	n, err := strconv.Atoi(waitRaw)
	if err != nil || n <= 0 {
		return minHLC, 0
	}
	if n > 120000 {
		n = 120000
	}
	return minHLC, time.Duration(n) * time.Millisecond
}

func parseLimitAndCursor(limitStr, cursorStr string) (int, int) {
	limit := 50
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
		limit = v
		if limit > 1000 {
			limit = 1000
		}
	}
	offset := 0
	if v, err := strconv.Atoi(cursorStr); err == nil && v > 0 {
		offset = v
	}
	return limit, offset
}

func paginateSlice[T any](items []T, offset, limit int) ([]T, string) {
	if offset >= len(items) {
		return []T{}, ""
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	nextCursor := ""
	if end < len(items) {
		nextCursor = strconv.Itoa(end)
	}
	return items[offset:end], nextCursor
}

func sortHumanActions(items []humanActionQueueEntry) {
	sort.Slice(items, func(i, j int) bool {
		return compareHLC(items[i].UpdatedHLC, items[j].UpdatedHLC) > 0
	})
}

func compareHLC(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	return strings.Compare(a, b)
}

func writeAcceptedFromRuntime(actionID string, result *bus.ProcessResult) domain.WriteAcceptedResponse {
	resp := domain.WriteAcceptedResponse{
		Accepted:      true,
		AdminActionID: strings.TrimSpace(actionID),
	}
	if result == nil {
		return resp
	}
	resp.EventID = strings.TrimSpace(result.EventID)
	resp.RequestID = strings.TrimSpace(result.RequestID)
	resp.TaskID = strings.TrimSpace(result.TaskID)
	resp.RouteTargetKind = strings.TrimSpace(result.RouteTargetKind)
	resp.RouteTargetID = strings.TrimSpace(result.RouteTargetID)
	if resp.RouteTargetKind == "" {
		if resp.TaskID != "" {
			resp.RouteTargetKind = string(domain.RouteTargetTask)
			resp.RouteTargetID = resp.TaskID
		} else if resp.RequestID != "" {
			resp.RouteTargetKind = string(domain.RouteTargetRequest)
			resp.RouteTargetID = resp.RequestID
		}
	}
	resp.CommitHLC = strings.TrimSpace(result.CommitHLC)
	return resp
}

func mapAdminErrorStatus(err error) int {
	if err == nil {
		return http.StatusBadRequest
	}
	if errors.Is(err, domain.ErrTerminalObjectNotRoutable) || errors.Is(err, bus.ErrScheduleSourceDisabled) {
		return http.StatusPreconditionFailed
	}
	if errors.Is(err, bus.ErrScheduleSourceNotFound) {
		return http.StatusNotFound
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "not found"), strings.Contains(message, "is not active"):
		return http.StatusNotFound
	case strings.Contains(message, "mismatch"), strings.Contains(message, "conflict"):
		return http.StatusConflict
	case strings.Contains(message, "requires"), strings.Contains(message, "precondition"),
		strings.Contains(message, "not allowed"), strings.Contains(message, "disabled"),
		strings.Contains(message, "terminal object"), strings.Contains(message, "input draft is unavailable"),
		strings.Contains(message, "must not be empty"):
		return http.StatusPreconditionFailed
	default:
		return http.StatusBadRequest
	}
}

func writeAdminErrorGin(c *gin.Context, err error) {
	c.JSON(mapAdminErrorStatus(err), gin.H{"error": err.Error()})
}

func readJSONMapGin(c *gin.Context) (map[string]json.RawMessage, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return map[string]json.RawMessage{}, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func readCancelTaskRequestGin(c *gin.Context, taskID string) (domain.CancelTaskRequest, error) {
	var out domain.CancelTaskRequest
	if err := c.ShouldBindJSON(&out); err != nil {
		return domain.CancelTaskRequest{TaskID: taskID}, nil
	}
	if strings.TrimSpace(out.TaskID) == "" {
		out.TaskID = taskID
	}
	return out, nil
}

func applyRequestFilters(items []RequestView, q url.Values) []RequestView {
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	conversationID := strings.TrimSpace(q.Get("conversation_id"))
	actor := strings.TrimSpace(q.Get("actor"))
	out := make([]RequestView, 0, len(items))
	for _, item := range items {
		if status != "" && strings.ToLower(strings.TrimSpace(item.Status)) != status {
			continue
		}
		if conversationID != "" && strings.TrimSpace(item.ConversationID) != conversationID {
			continue
		}
		if actor != "" && strings.TrimSpace(item.ActorRef) != actor {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyRequestFiltersGin(items []RequestView, c *gin.Context) []RequestView {
	return applyRequestFilters(items, c.Request.URL.Query())
}

func applyTaskFilters(items []TaskView, q url.Values) []TaskView {
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	workflowID := strings.TrimSpace(q.Get("workflow_id"))
	repo := strings.TrimSpace(q.Get("repo"))
	waitingReason := strings.ToLower(strings.TrimSpace(q.Get("waiting_reason")))
	out := make([]TaskView, 0, len(items))
	for _, item := range items {
		if status != "" && strings.ToLower(strings.TrimSpace(item.Status)) != status {
			continue
		}
		if workflowID != "" && strings.TrimSpace(item.WorkflowID) != workflowID {
			continue
		}
		if repo != "" && strings.TrimSpace(item.RepoRef) != repo {
			continue
		}
		if waitingReason != "" && strings.ToLower(strings.TrimSpace(item.WaitingReason)) != waitingReason {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyTaskFiltersGin(items []TaskView, c *gin.Context) []TaskView {
	return applyTaskFilters(items, c.Request.URL.Query())
}

func applyScheduleFilters(items []ScheduleView, q url.Values) []ScheduleView {
	workflowID := strings.TrimSpace(q.Get("workflow_id"))
	timezone := strings.TrimSpace(q.Get("timezone"))
	enabledValue, hasEnabled := parseOptionalBool(q.Get("enabled"))
	out := make([]ScheduleView, 0, len(items))
	for _, item := range items {
		if hasEnabled && item.Enabled != enabledValue {
			continue
		}
		if workflowID != "" && strings.TrimSpace(item.TargetWorkflowID) != workflowID {
			continue
		}
		if timezone != "" && strings.TrimSpace(item.Timezone) != timezone {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyScheduleFiltersGin(items []ScheduleView, c *gin.Context) []ScheduleView {
	return applyScheduleFilters(items, c.Request.URL.Query())
}

func parseOptionalBool(value string) (bool, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, false
	}
	v, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}
	return v, true
}

func applyApprovalFilters(items []ApprovalView, q url.Values) []ApprovalView {
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	taskID := strings.TrimSpace(q.Get("task_id"))
	out := make([]ApprovalView, 0, len(items))
	for _, item := range items {
		if status != "" && strings.ToLower(strings.TrimSpace(item.Status)) != status {
			continue
		}
		if taskID != "" && strings.TrimSpace(item.TaskID) != taskID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyApprovalFiltersGin(items []ApprovalView, c *gin.Context) []ApprovalView {
	return applyApprovalFilters(items, c.Request.URL.Query())
}

func applyHumanWaitFilters(items []HumanWaitView, q url.Values) []HumanWaitView {
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	taskID := strings.TrimSpace(q.Get("task_id"))
	out := make([]HumanWaitView, 0, len(items))
	for _, item := range items {
		if status != "" && strings.ToLower(strings.TrimSpace(item.Status)) != status {
			continue
		}
		if taskID != "" && strings.TrimSpace(item.TaskID) != taskID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyHumanWaitFiltersGin(items []HumanWaitView, c *gin.Context) []HumanWaitView {
	return applyHumanWaitFilters(items, c.Request.URL.Query())
}

func applyHumanActionFilters(items []humanActionQueueEntry, q url.Values) []humanActionQueueEntry {
	entryKind := strings.ToLower(strings.TrimSpace(q.Get("entry_kind")))
	taskID := strings.TrimSpace(q.Get("task_id"))
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	out := make([]humanActionQueueEntry, 0, len(items))
	for _, item := range items {
		if entryKind != "" && strings.ToLower(strings.TrimSpace(item.EntryKind)) != entryKind {
			continue
		}
		if taskID != "" && strings.TrimSpace(item.TaskID) != taskID {
			continue
		}
		if status != "" && strings.ToLower(strings.TrimSpace(item.Status)) != status {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyHumanActionFiltersGin(items []humanActionQueueEntry, c *gin.Context) []humanActionQueueEntry {
	return applyHumanActionFilters(items, c.Request.URL.Query())
}

func applyDeadletterFilters(items []DeadletterView, q url.Values) []DeadletterView {
	failureStage := strings.ToLower(strings.TrimSpace(q.Get("failure_stage")))
	retryableValue, hasRetryable := parseOptionalBool(q.Get("retryable"))
	out := make([]DeadletterView, 0, len(items))
	for _, item := range items {
		if failureStage != "" && strings.ToLower(strings.TrimSpace(item.FailureStage)) != failureStage {
			continue
		}
		if hasRetryable && item.Retryable != retryableValue {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyDeadletterFiltersGin(items []DeadletterView, c *gin.Context) []DeadletterView {
	return applyDeadletterFilters(items, c.Request.URL.Query())
}

func applyEventFilters(items []EventView, q url.Values) []EventView {
	eventType := strings.ToLower(strings.TrimSpace(q.Get("event_type")))
	sourceKind := strings.ToLower(strings.TrimSpace(q.Get("source_kind")))
	traceID := strings.TrimSpace(q.Get("trace_id"))
	out := make([]EventView, 0, len(items))
	for _, item := range items {
		if eventType != "" && strings.ToLower(strings.TrimSpace(item.EventType)) != eventType {
			continue
		}
		if sourceKind != "" {
			currentSource := ""
			if item.External != nil {
				currentSource = strings.ToLower(strings.TrimSpace(item.External.SourceKind))
			}
			if currentSource != sourceKind {
				continue
			}
		}
		if traceID != "" && strings.TrimSpace(item.TraceID) != traceID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func applyEventFiltersGin(items []EventView, c *gin.Context) []EventView {
	return applyEventFilters(items, c.Request.URL.Query())
}

func isApprovalDecisionAllowed(gateType string, decision domain.HumanActionKind) bool {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case "approval":
		return decision == domain.HumanActionApprove || decision == domain.HumanActionReject
	case "confirmation", "evaluation":
		return decision == domain.HumanActionConfirm || decision == domain.HumanActionReject
	case "budget":
		return decision == domain.HumanActionResumeBudget || decision == domain.HumanActionReject
	default:
		return false
	}
}

func normalizeWaitingReason(in string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case strings.ToLower(string(domain.WaitingReasonInput)), "waiting_input", "input":
		return string(domain.WaitingReasonInput), true
	case strings.ToLower(string(domain.WaitingReasonRecovery)), "waiting_recovery", "recovery":
		return string(domain.WaitingReasonRecovery), true
	default:
		return "", false
	}
}

func isWaitDecisionAllowed(waitingReason string, decision domain.HumanActionKind) bool {
	switch waitingReason {
	case string(domain.WaitingReasonInput):
		return decision == domain.HumanActionProvideInput
	case string(domain.WaitingReasonRecovery):
		return decision == domain.HumanActionResumeRecovery || decision == domain.HumanActionRewind
	default:
		return false
	}
}
