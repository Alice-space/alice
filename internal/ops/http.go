package ops

import (
	"context"
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
	"alice/internal/store"
)

type AdminHooks struct {
	ReconcileOutbox    func(r *http.Request) error
	ReconcileSchedules func(r *http.Request) error
	RebuildIndexes     func(r *http.Request) error
	ReplayFromHLC      func(r *http.Request, hlc string) error
	RedriveDeadletter  func(r *http.Request, deadletterID string) error
	CancelTask         func(r *http.Request, taskID string) error
}

type SurfaceConfig struct {
	AdminEventInjectionEnabled     bool
	AdminScheduleFireReplayEnabled bool
}

type HTTPManager struct {
	store     *store.Store
	indexes   *store.BoltIndexStore
	runtime   *bus.Runtime
	reception bus.Reception
	hooks     AdminHooks
	config    SurfaceConfig
}

type adminActionIDKey struct{}

type humanActionQueueEntry struct {
	EntryID       string `json:"entry_id"`
	EntryKind     string `json:"entry_kind"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	UpdatedHLC    string `json:"updated_hlc,omitempty"`
	ApprovalID    string `json:"approval_request_id,omitempty"`
	HumanWaitID   string `json:"human_wait_id,omitempty"`
	WaitingReason string `json:"waiting_reason,omitempty"`
}

func NewHTTPManager(st *store.Store, runtime *bus.Runtime, reception bus.Reception, hooks AdminHooks, cfg SurfaceConfig) *HTTPManager {
	var indexes *store.BoltIndexStore
	if st != nil {
		indexes = st.Indexes
	}
	return &HTTPManager{
		store:     st,
		indexes:   indexes,
		runtime:   runtime,
		reception: reception,
		hooks:     hooks,
		config:    cfg,
	}
}

func (m *HTTPManager) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/ops/overview", m.handleOverview)

	mux.HandleFunc("/v1/events", m.handleEvents)
	mux.HandleFunc("/v1/events/", m.handleEventByID)
	mux.HandleFunc("/v1/requests", m.handleRequests)
	mux.HandleFunc("/v1/requests/", m.handleRequestByID)
	mux.HandleFunc("/v1/tasks", m.handleTasks)
	mux.HandleFunc("/v1/tasks/", m.handleTaskByID)
	mux.HandleFunc("/v1/schedules", m.handleSchedules)
	mux.HandleFunc("/v1/schedules/", m.handleScheduleByID)
	mux.HandleFunc("/v1/approvals", m.handleApprovals)
	mux.HandleFunc("/v1/approvals/", m.handleApprovalByID)
	mux.HandleFunc("/v1/human-waits", m.handleHumanWaits)
	mux.HandleFunc("/v1/human-waits/", m.handleHumanWaitByID)
	mux.HandleFunc("/v1/deadletters", m.handleDeadletters)
	mux.HandleFunc("/v1/deadletters/", m.handleDeadletterByID)
	mux.HandleFunc("/v1/human-actions", m.handleHumanActions)

	mux.HandleFunc("/v1/admin/submit/events", m.handleSubmitEvent)
	mux.HandleFunc("/v1/admin/submit/fires", m.handleSubmitFire)
	mux.HandleFunc("/v1/admin/resolve/approval", m.handleResolveApproval)
	mux.HandleFunc("/v1/admin/resolve/wait", m.handleResolveWait)
	mux.HandleFunc("/v1/admin/reconcile/outbox", m.wrapAdminHook(m.hooks.ReconcileOutbox))
	mux.HandleFunc("/v1/admin/reconcile/schedules", m.wrapAdminHook(m.hooks.ReconcileSchedules))
	mux.HandleFunc("/v1/admin/rebuild/indexes", m.wrapAdminHook(m.hooks.RebuildIndexes))
	mux.HandleFunc("/v1/admin/replay/from/", m.handleReplayFrom)
	mux.HandleFunc("/v1/admin/tasks/", m.handleTaskCancel)
	mux.HandleFunc("/v1/admin/deadletters/", m.handleDeadletterRedrive)
}

func (m *HTTPManager) handleOverview(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var overview store.OpsOverview
	if m.indexes != nil {
		overview, err = m.indexes.Overview(req.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	resp := domain.GetResponse[OpsOverviewView]{
		Item: OpsOverviewView{
			OpenRequests:  overview.OpenRequests,
			ActiveTasks:   overview.ActiveTasks,
			PendingOutbox: overview.PendingOutbox,
			ApprovalQueue: overview.ApprovalQueue,
			HumanQueue:    overview.HumanQueue,
		},
		VisibleHLC: model.VisibleHLC,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (m *HTTPManager) handleHumanActions(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := make([]humanActionQueueEntry, 0, len(model.approvals)+len(model.humanWaits))
	for _, approval := range model.approvalList() {
		if strings.ToLower(strings.TrimSpace(approval.Status)) != "open" {
			continue
		}
		entry := humanActionQueueEntry{
			EntryID:    "approval:" + approval.ApprovalRequestID,
			EntryKind:  "approval",
			TaskID:     approval.TaskID,
			Status:     approval.Status,
			UpdatedHLC: approval.UpdatedHLC,
			ApprovalID: approval.ApprovalRequestID,
		}
		if !approval.ExpiresAt.IsZero() {
			entry.ExpiresAt = approval.ExpiresAt.UTC().Format(time.RFC3339)
		}
		items = append(items, entry)
	}
	for _, wait := range model.humanWaitList() {
		if strings.ToLower(strings.TrimSpace(wait.Status)) != "open" {
			continue
		}
		entry := humanActionQueueEntry{
			EntryID:       "wait:" + wait.HumanWaitID,
			EntryKind:     "human-wait",
			TaskID:        wait.TaskID,
			Status:        wait.Status,
			UpdatedHLC:    wait.UpdatedHLC,
			HumanWaitID:   wait.HumanWaitID,
			WaitingReason: wait.WaitingReason,
		}
		if !wait.ExpiresAt.IsZero() {
			entry.ExpiresAt = wait.ExpiresAt.UTC().Format(time.RFC3339)
		}
		items = append(items, entry)
	}
	items = applyHumanActionFilters(items, req.URL.Query())
	sortHumanActions(items)
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[humanActionQueueEntry]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleEvents(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.eventList()
	items = applyEventFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[EventView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "global_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleEventByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	eventID := strings.TrimPrefix(req.URL.Path, "/v1/events/")
	eventID = strings.TrimSpace(strings.Trim(eventID, "/"))
	if eventID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.eventByID(eventID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[EventView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleRequests(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.requestList()
	items = applyRequestFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[RequestView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleRequestByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requestID := strings.TrimPrefix(req.URL.Path, "/v1/requests/")
	requestID = strings.TrimSpace(strings.Trim(requestID, "/"))
	if requestID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.requestByID(requestID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[RequestView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleTasks(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.taskList()
	items = applyTaskFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[TaskView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleTaskByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	taskID := strings.TrimPrefix(req.URL.Path, "/v1/tasks/")
	taskID = strings.TrimSpace(strings.Trim(taskID, "/"))
	if taskID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.taskByID(taskID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[TaskView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleSchedules(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.scheduleList()
	items = applyScheduleFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[ScheduleView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleScheduleByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scheduledTaskID := strings.TrimPrefix(req.URL.Path, "/v1/schedules/")
	scheduledTaskID = strings.TrimSpace(strings.Trim(scheduledTaskID, "/"))
	if scheduledTaskID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.scheduleByID(scheduledTaskID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[ScheduleView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleApprovals(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.approvalList()
	items = applyApprovalFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[ApprovalView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleApprovalByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	approvalRequestID := strings.TrimPrefix(req.URL.Path, "/v1/approvals/")
	approvalRequestID = strings.TrimSpace(strings.Trim(approvalRequestID, "/"))
	if approvalRequestID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.approvalByID(approvalRequestID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[ApprovalView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleHumanWaits(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.humanWaitList()
	items = applyHumanWaitFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[HumanWaitView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleHumanWaitByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	humanWaitID := strings.TrimPrefix(req.URL.Path, "/v1/human-waits/")
	humanWaitID = strings.TrimSpace(strings.Trim(humanWaitID, "/"))
	if humanWaitID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.humanWaitByID(humanWaitID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[HumanWaitView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleDeadletters(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit, offset := parseLimitAndCursor(req.URL.Query().Get("limit"), req.URL.Query().Get("cursor"))
	items := model.deadletterList()
	items = applyDeadletterFilters(items, req.URL.Query())
	page, next := paginateSlice(items, offset, limit)
	writeJSON(w, http.StatusOK, domain.ListResponse[DeadletterView]{
		Items:      page,
		NextCursor: next,
		OrderBy:    "updated_hlc desc",
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleDeadletterByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deadletterID := strings.TrimPrefix(req.URL.Path, "/v1/deadletters/")
	deadletterID = strings.TrimSpace(strings.Trim(deadletterID, "/"))
	if deadletterID == "" {
		http.NotFound(w, req)
		return
	}
	minHLC, waitTimeout := parseReadWait(req)
	model, err := m.loadReadModel(req.Context(), minHLC, waitTimeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.deadletterByID(deadletterID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeJSON(w, http.StatusOK, domain.GetResponse[DeadletterView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) wrapAdminHook(hook func(r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		actionID := resolveAdminActionID(req)
		req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
		if hook != nil {
			if err := hook(req); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		commitHLC, _ := m.currentVisibleHLC(req.Context())
		writeJSON(w, http.StatusAccepted, domain.WriteAcceptedResponse{
			Accepted:      true,
			AdminActionID: actionID,
			CommitHLC:     commitHLC,
		})
	}
}

func (m *HTTPManager) handleReplayFrom(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hlc := strings.TrimPrefix(req.URL.Path, "/v1/admin/replay/from/")
	hlc = strings.TrimSpace(strings.Trim(hlc, "/"))
	if hlc == "" {
		http.Error(w, "from hlc is required", http.StatusBadRequest)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
	if m.hooks.ReplayFromHLC != nil {
		if err := m.hooks.ReplayFromHLC(req, hlc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	commitHLC, _ := m.currentVisibleHLC(req.Context())
	writeJSON(w, http.StatusAccepted, domain.WriteAcceptedResponse{
		Accepted:      true,
		AdminActionID: actionID,
		CommitHLC:     commitHLC,
	})
}

func (m *HTTPManager) handleDeadletterRedrive(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(req.URL.Path, "/v1/admin/deadletters/")
	if !strings.HasSuffix(path, "/redrive") {
		http.NotFound(w, req)
		return
	}
	deadletterID := strings.TrimSuffix(path, "/redrive")
	deadletterID = strings.TrimSpace(strings.Trim(deadletterID, "/"))
	if deadletterID == "" {
		http.Error(w, "deadletter id is required", http.StatusBadRequest)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
	if m.hooks.RedriveDeadletter == nil {
		http.Error(w, "deadletter redrive is not configured", http.StatusNotImplemented)
		return
	}
	model, err := m.loadReadModel(req.Context(), "", 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, ok := model.deadletterByID(deadletterID)
	if !ok {
		http.Error(w, "deadletter not found", http.StatusNotFound)
		return
	}
	if !item.Retryable {
		http.Error(w, "deadletter is not retryable", http.StatusConflict)
		return
	}
	if err := m.hooks.RedriveDeadletter(req, deadletterID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	commitHLC, _ := m.currentVisibleHLC(req.Context())
	writeJSON(w, http.StatusAccepted, domain.WriteAcceptedResponse{
		Accepted:      true,
		AdminActionID: actionID,
		CommitHLC:     commitHLC,
	})
}

func (m *HTTPManager) handleSubmitEvent(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !m.config.AdminEventInjectionEnabled {
		http.Error(w, "admin submit events is disabled", http.StatusForbidden)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
	if m.runtime == nil {
		http.Error(w, "runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	bodyMap, err := readJSONMap(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	forbidden := []string{
		"event_id",
		"received_at",
		"verified",
		"fire_id",
		"source_schedule_revision",
		"approval_request_id",
		"human_wait_id",
	}
	for _, key := range forbidden {
		if _, ok := bodyMap[key]; ok {
			http.Error(w, "field is forbidden: "+key, http.StatusUnprocessableEntity)
			return
		}
	}
	var in domain.ExternalEventInput
	if err := decodeMapToStruct(bodyMap, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sourceKind, commentRef, err := validateExternalEventInput(in, bodyMap)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	actorRef := strings.TrimSpace(in.ActorRef)
	if actorRef == "" {
		actorRef = strings.TrimSpace(req.Header.Get("X-Admin-Actor"))
	}
	sourceRef := strings.TrimSpace(in.SourceRef)
	if sourceRef == "" {
		sourceRef = "admin:submit_event:" + actionID
	}
	threadID := strings.TrimSpace(in.ThreadID)
	if threadID == "" {
		threadID = "root"
	}
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = actionID
	}
	if err := m.recordAdminAudit(req.Context(), req, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "submit_event",
		ActorRef:      actorRef,
		TargetKind:    "event",
		TargetID:      idempotencyKey,
	}); err != nil {
		writeAdminError(w, err)
		return
	}
	evt := domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        sourceKind,
		TransportKind:     "cli_admin_injected",
		SourceRef:         sourceRef,
		ActorRef:          actorRef,
		ConversationID:    strings.TrimSpace(in.ConversationID),
		ThreadID:          threadID,
		RepoRef:           strings.TrimSpace(in.RepoRef),
		IssueRef:          strings.TrimSpace(in.IssueRef),
		PRRef:             strings.TrimSpace(in.PRRef),
		CommentRef:        commentRef,
		ReplyToEventID:    strings.TrimSpace(in.ReplyToEventID),
		ScheduledTaskID:   strings.TrimSpace(in.ScheduledTaskID),
		ControlObjectRef:  strings.TrimSpace(in.ControlObjectRef),
		WorkflowObjectRef: strings.TrimSpace(in.WorkflowObjectRef),
		CausationID:       actionID,
		TraceID:           strings.TrimSpace(in.TraceID),
		IdempotencyKey:    idempotencyKey,
		PayloadRef:        "schema:" + strings.TrimSpace(in.BodySchemaID),
		Verified:          true,
		ReceivedAt:        time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(req.Context(), evt, m.reception)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleSubmitFire(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !m.config.AdminScheduleFireReplayEnabled {
		http.Error(w, "admin submit fires is disabled", http.StatusForbidden)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
	if m.runtime == nil {
		http.Error(w, "runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	var in domain.ScheduleFireReplayRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in.ScheduledTaskID = strings.TrimSpace(in.ScheduledTaskID)
	if in.ScheduledTaskID == "" {
		http.Error(w, "scheduled_task_id is required", http.StatusBadRequest)
		return
	}
	if _, err := m.runtime.RequireEnabledScheduleSource(req.Context(), in.ScheduledTaskID); err != nil {
		writeAdminError(w, err)
		return
	}
	window := in.ScheduledForWindow.UTC()
	if window.IsZero() {
		window = time.Now().UTC().Truncate(time.Minute)
	}
	fireID := domain.ComputeFireID(in.ScheduledTaskID, window)
	if err := m.recordAdminAudit(req.Context(), req, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "submit_fire",
		ActorRef:      strings.TrimSpace(in.ActorRef),
		TargetKind:    "scheduled_task",
		TargetID:      in.ScheduledTaskID,
	}); err != nil {
		writeAdminError(w, err)
		return
	}
	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeScheduleTriggered,
		SourceKind:      "scheduler",
		TransportKind:   "cli_admin_fire_replay",
		SourceRef:       window.Format(time.RFC3339),
		ActorRef:        strings.TrimSpace(in.ActorRef),
		ScheduledTaskID: in.ScheduledTaskID,
		CausationID:     actionID,
		TraceID:         strings.TrimSpace(in.TraceID),
		IdempotencyKey:  fireID,
		PayloadRef:      "admin:submit_fire:" + actionID,
		Verified:        true,
		ReceivedAt:      time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(req.Context(), evt, nil)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleResolveApproval(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
	if m.runtime == nil || m.indexes == nil {
		http.Error(w, "runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	var in domain.ResolveApprovalRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in.ApprovalRequestID = strings.TrimSpace(in.ApprovalRequestID)
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.StepExecutionID = strings.TrimSpace(in.StepExecutionID)
	if in.ApprovalRequestID == "" || in.TaskID == "" || in.StepExecutionID == "" {
		http.Error(w, "approval_request_id/task_id/step_execution_id are required", http.StatusBadRequest)
		return
	}
	decision := domain.NormalizeHumanActionKind(in.Decision)
	switch decision {
	case domain.HumanActionApprove, domain.HumanActionReject, domain.HumanActionConfirm, domain.HumanActionResumeBudget:
	default:
		http.Error(w, "invalid decision", http.StatusUnprocessableEntity)
		return
	}
	approval, ok, err := m.indexes.GetApprovalRequest(req.Context(), in.ApprovalRequestID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "approval request is not active", http.StatusNotFound)
		return
	}
	if approval.TaskID != in.TaskID || approval.StepExecutionID != in.StepExecutionID {
		http.Error(w, "approval task/step mismatch", http.StatusConflict)
		return
	}
	if !isApprovalDecisionAllowed(approval.GateType, decision) {
		http.Error(w, "decision is not allowed for gate_type="+approval.GateType, http.StatusUnprocessableEntity)
		return
	}
	actorRef := strings.TrimSpace(in.ActorRef)
	if actorRef == "" {
		actorRef = strings.TrimSpace(req.Header.Get("X-Admin-Actor"))
	}
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = actionID
	}
	if err := m.recordAdminAudit(req.Context(), req, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "resolve_approval",
		ActorRef:      actorRef,
		TargetKind:    "approval_request",
		TargetID:      in.ApprovalRequestID,
	}); err != nil {
		writeAdminError(w, err)
		return
	}
	evt := domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "human_action",
		TransportKind:     "cli_admin",
		SourceRef:         "admin:resolve_approval:" + actionID,
		ActorRef:          actorRef,
		ActionKind:        string(decision),
		TaskID:            in.TaskID,
		ApprovalRequestID: in.ApprovalRequestID,
		StepExecutionID:   in.StepExecutionID,
		CausationID:       actionID,
		TraceID:           strings.TrimSpace(in.TraceID),
		IdempotencyKey:    idempotencyKey,
		PayloadRef:        strings.TrimSpace(in.Note),
		Verified:          true,
		ReceivedAt:        time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(req.Context(), evt, nil)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleResolveWait(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))
	if m.runtime == nil || m.indexes == nil {
		http.Error(w, "runtime is unavailable", http.StatusServiceUnavailable)
		return
	}
	var in domain.ResolveHumanWaitRequest
	if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	in.HumanWaitID = strings.TrimSpace(in.HumanWaitID)
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.StepExecutionID = strings.TrimSpace(in.StepExecutionID)
	in.TargetStepID = strings.TrimSpace(in.TargetStepID)
	if in.HumanWaitID == "" || in.TaskID == "" {
		http.Error(w, "human_wait_id/task_id are required", http.StatusBadRequest)
		return
	}
	wait, ok, err := m.indexes.GetHumanWait(req.Context(), in.HumanWaitID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "human wait is not active", http.StatusNotFound)
		return
	}
	if wait.TaskID != in.TaskID {
		http.Error(w, "human wait task mismatch", http.StatusConflict)
		return
	}
	if in.StepExecutionID != "" && wait.StepExecutionID != "" && in.StepExecutionID != wait.StepExecutionID {
		http.Error(w, "step_execution_id mismatch", http.StatusConflict)
		return
	}
	waitingReason, ok := normalizeWaitingReason(in.WaitingReason)
	if !ok {
		http.Error(w, "waiting_reason must be WaitingInput or WaitingRecovery", http.StatusUnprocessableEntity)
		return
	}
	if !strings.EqualFold(wait.WaitingReason, waitingReason) {
		http.Error(w, "waiting_reason mismatch", http.StatusConflict)
		return
	}
	decision := domain.NormalizeHumanActionKind(in.Decision)
	if !isWaitDecisionAllowed(waitingReason, decision) {
		http.Error(w, "decision is not allowed for waiting_reason="+waitingReason, http.StatusUnprocessableEntity)
		return
	}
	if decision == domain.HumanActionRewind && in.TargetStepID == "" {
		http.Error(w, "target_step_id is required for rewind", http.StatusUnprocessableEntity)
		return
	}
	if decision == domain.HumanActionProvideInput && len(strings.TrimSpace(string(in.InputPatch))) == 0 {
		http.Error(w, "input_patch is required for provide-input", http.StatusUnprocessableEntity)
		return
	}
	if len(strings.TrimSpace(string(in.InputPatch))) > 0 {
		if _, err := domain.ApplyHumanWaitInputPatch(wait, in.InputPatch); err != nil {
			writeAdminError(w, err)
			return
		}
	}
	actorRef := strings.TrimSpace(in.ActorRef)
	if actorRef == "" {
		actorRef = strings.TrimSpace(req.Header.Get("X-Admin-Actor"))
	}
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = actionID
	}
	if err := m.recordAdminAudit(req.Context(), req, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "resolve_wait",
		ActorRef:      actorRef,
		TargetKind:    "human_wait",
		TargetID:      in.HumanWaitID,
	}); err != nil {
		writeAdminError(w, err)
		return
	}
	stepExecutionID := in.StepExecutionID
	if stepExecutionID == "" {
		stepExecutionID = wait.StepExecutionID
	}
	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human_action",
		TransportKind:   "cli_admin",
		SourceRef:       "admin:resolve_wait:" + actionID,
		ActorRef:        actorRef,
		ActionKind:      string(decision),
		TaskID:          in.TaskID,
		HumanWaitID:     in.HumanWaitID,
		StepExecutionID: stepExecutionID,
		TargetStepID:    in.TargetStepID,
		WaitingReason:   waitingReason,
		InputSchemaID:   wait.InputSchemaID,
		InputPatch:      in.InputPatch,
		CausationID:     actionID,
		TraceID:         strings.TrimSpace(in.TraceID),
		IdempotencyKey:  idempotencyKey,
		PayloadRef:      strings.TrimSpace(in.Note),
		Verified:        true,
		ReceivedAt:      time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(req.Context(), evt, nil)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) handleTaskCancel(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(req.URL.Path, "/v1/admin/tasks/")
	if !strings.HasSuffix(path, "/cancel") {
		http.NotFound(w, req)
		return
	}
	taskID := strings.TrimSuffix(path, "/cancel")
	taskID = strings.TrimSpace(strings.Trim(taskID, "/"))
	if taskID == "" {
		http.Error(w, "task id is required", http.StatusBadRequest)
		return
	}
	actionID := resolveAdminActionID(req)
	req = req.WithContext(context.WithValue(req.Context(), adminActionIDKey{}, actionID))

	if m.runtime == nil {
		http.Error(w, "runtime is unavailable", http.StatusServiceUnavailable)
		return
	}

	cancelReq, _ := readCancelTaskRequest(req, taskID)
	if cancelReq.TaskID != "" && cancelReq.TaskID != taskID {
		http.Error(w, "task_id mismatch with url path", http.StatusUnprocessableEntity)
		return
	}
	stepExecutionID := strings.TrimSpace(cancelReq.StepExecutionID)
	if stepExecutionID == "" {
		stepExecutionID = strings.TrimSpace(req.URL.Query().Get("step_execution_id"))
	}
	actorRef := strings.TrimSpace(cancelReq.ActorRef)
	if actorRef == "" {
		actorRef = strings.TrimSpace(req.Header.Get("X-Admin-Actor"))
	}
	idempotencyKey := strings.TrimSpace(cancelReq.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = actionID
	}
	if err := m.recordAdminAudit(req.Context(), req, domain.AdminAuditRecordedPayload{
		AdminActionID: actionID,
		Operation:     "cancel_task",
		ActorRef:      actorRef,
		TargetKind:    "task",
		TargetID:      taskID,
	}); err != nil {
		writeAdminError(w, err)
		return
	}
	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human_action",
		TransportKind:   "cli_admin",
		SourceRef:       "admin:task_cancel:" + actionID,
		ActorRef:        actorRef,
		ActionKind:      string(domain.HumanActionCancel),
		TaskID:          taskID,
		StepExecutionID: stepExecutionID,
		CausationID:     actionID,
		TraceID:         strings.TrimSpace(cancelReq.TraceID),
		IdempotencyKey:  idempotencyKey,
		PayloadRef:      strings.TrimSpace(cancelReq.Reason),
		Verified:        true,
		ReceivedAt:      time.Now().UTC(),
	}
	result, err := m.runtime.IngestExternalEvent(req.Context(), evt, nil)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, writeAcceptedFromRuntime(actionID, result))
}

func (m *HTTPManager) recordAdminAudit(ctx context.Context, req *http.Request, payload domain.AdminAuditRecordedPayload) error {
	if m.runtime == nil {
		return fmt.Errorf("runtime is unavailable")
	}
	if req != nil {
		payload.RequestPath = req.URL.Path
		payload.RequestMethod = req.Method
	}
	return m.runtime.RecordAdminAudit(ctx, payload)
}

func writeAdminError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), mapAdminErrorStatus(err))
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

func (m *HTTPManager) loadReadModel(ctx context.Context, minHLC string, waitTimeout time.Duration) (*readModel, error) {
	start := time.Now()
	for {
		model, err := buildReadModel(ctx, m.store)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(minHLC) == "" || compareHLC(model.VisibleHLC, minHLC) >= 0 || waitTimeout <= 0 || time.Since(start) >= waitTimeout {
			return model, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (m *HTTPManager) currentVisibleHLC(ctx context.Context) (string, error) {
	model, err := buildReadModel(ctx, m.store)
	if err != nil {
		return "", err
	}
	return model.VisibleHLC, nil
}

func AdminActionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(adminActionIDKey{}).(string)
	return strings.TrimSpace(v)
}

func resolveAdminActionID(req *http.Request) string {
	if req == nil {
		return newAdminActionID()
	}
	if v := strings.TrimSpace(req.Header.Get("X-Admin-Action-ID")); v != "" {
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

func parseReadWait(req *http.Request) (string, time.Duration) {
	minHLC := strings.TrimSpace(req.URL.Query().Get("min_hlc"))
	waitRaw := strings.TrimSpace(req.URL.Query().Get("wait_timeout_ms"))
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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

func decodeMapToStruct[T any](input map[string]json.RawMessage, out *T) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func readJSONMap(req *http.Request) (map[string]json.RawMessage, error) {
	body, err := io.ReadAll(req.Body)
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

func validateExternalEventInput(in domain.ExternalEventInput, top map[string]json.RawMessage) (sourceKind string, commentRef string, err error) {
	kind := strings.ToLower(strings.TrimSpace(in.InputKind))
	schema := strings.TrimSpace(in.BodySchemaID)
	spec, ok := externalEventInputSpecs[kind]
	if !ok {
		return "", "", fmt.Errorf("unsupported input_kind: %s", in.InputKind)
	}
	if schema == "" {
		return "", "", fmt.Errorf("body_schema_id is required")
	}
	if schema != spec.SchemaID {
		return "", "", fmt.Errorf("body_schema_id mismatch for input_kind=%s: want=%s", in.InputKind, spec.SchemaID)
	}
	bodyMap := map[string]json.RawMessage{}
	if len(in.Body) > 0 {
		if err := json.Unmarshal(in.Body, &bodyMap); err != nil {
			return "", "", fmt.Errorf("body must be a json object")
		}
	}
	forbiddenBody := []string{"event_id", "received_at", "verified", "fire_id", "source_schedule_revision", "approval_request_id", "human_wait_id"}
	for _, key := range forbiddenBody {
		if _, ok := bodyMap[key]; ok {
			return "", "", fmt.Errorf("body field is forbidden: %s", key)
		}
	}
	routeCritical := map[string]string{
		"reply_to_event_id":   strings.TrimSpace(in.ReplyToEventID),
		"scheduled_task_id":   strings.TrimSpace(in.ScheduledTaskID),
		"control_object_ref":  strings.TrimSpace(in.ControlObjectRef),
		"workflow_object_ref": strings.TrimSpace(in.WorkflowObjectRef),
	}
	for key := range routeCritical {
		if _, ok := bodyMap[key]; ok {
			return "", "", fmt.Errorf("body must not include route-critical field: %s", key)
		}
	}
	switch kind {
	case "web_form_message", "control_plane_message":
		text, ok := requiredString(bodyMap, "text")
		if !ok || strings.TrimSpace(text) == "" {
			return "", "", fmt.Errorf("body.text is required")
		}
	case "repo_issue_comment", "repo_pr_comment":
		commentText, ok := requiredString(bodyMap, "comment_text")
		if !ok || strings.TrimSpace(commentText) == "" {
			return "", "", fmt.Errorf("body.comment_text is required")
		}
		commentRef = optionalString(bodyMap, "comment_ref")
	}
	forbiddenTop := []string{"event_id", "received_at", "verified", "fire_id", "source_schedule_revision", "approval_request_id", "human_wait_id"}
	for _, key := range forbiddenTop {
		if _, ok := top[key]; ok {
			return "", "", fmt.Errorf("field is forbidden: %s", key)
		}
	}
	return spec.SourceKind, strings.TrimSpace(commentRef), nil
}

type externalEventInputSpec struct {
	SchemaID   string
	SourceKind string
}

var externalEventInputSpecs = map[string]externalEventInputSpec{
	"web_form_message":      {SchemaID: "web-form-message.v1", SourceKind: "direct_input"},
	"repo_issue_comment":    {SchemaID: "repo-issue-comment.v1", SourceKind: "repo_comment"},
	"repo_pr_comment":       {SchemaID: "repo-pr-comment.v1", SourceKind: "repo_comment"},
	"control_plane_message": {SchemaID: "control-plane-message.v1", SourceKind: "control_plane"},
}

func requiredString(body map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := body[key]
	if !ok {
		return "", false
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", false
	}
	return out, true
}

func optionalString(body map[string]json.RawMessage, key string) string {
	raw, ok := body[key]
	if !ok {
		return ""
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return ""
	}
	return out
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

func applyRequestFilters(items []RequestView, q url.Values) []RequestView {
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	conversationID := strings.TrimSpace(q.Get("conversation_id"))
	actor := strings.TrimSpace(q.Get("actor"))
	updatedSince := parseTimeValue(q.Get("updated_since"))
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
		if !updatedSince.IsZero() {
			updatedAt, _ := parseHLC(item.UpdatedHLC)
			if updatedAt.IsZero() || updatedAt.Before(updatedSince) {
				continue
			}
		}
		out = append(out, item)
	}
	return out
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

func applyHumanActionFilters(items []humanActionQueueEntry, q url.Values) []humanActionQueueEntry {
	entryKind := strings.ToLower(strings.TrimSpace(q.Get("entry_kind")))
	taskID := strings.TrimSpace(q.Get("task_id"))
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	expiresBefore := parseTimeValue(q.Get("expires_before"))
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
		if !expiresBefore.IsZero() {
			expireAt := parseTimeValue(item.ExpiresAt)
			if expireAt.IsZero() || expireAt.After(expiresBefore) {
				continue
			}
		}
		out = append(out, item)
	}
	return out
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

func parseTimeValue(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
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

func readCancelTaskRequest(req *http.Request, taskID string) (domain.CancelTaskRequest, error) {
	var out domain.CancelTaskRequest
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(string(body)) == "" {
		return domain.CancelTaskRequest{TaskID: taskID}, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	if strings.TrimSpace(out.TaskID) == "" {
		out.TaskID = taskID
	}
	return out, nil
}

func sortHumanActions(items []humanActionQueueEntry) {
	sort.Slice(items, func(i, j int) bool {
		return compareHLC(items[i].UpdatedHLC, items[j].UpdatedHLC) > 0
	})
}
