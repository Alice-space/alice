package ops

import (
	"net/http"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/store"

	"github.com/gin-gonic/gin"
)

func (m *HTTPManager) handleOverview(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var overview store.OpsOverview
	if m.indexes != nil {
		overview, _ = m.indexes.Overview(c.Request.Context())
	}
	c.JSON(http.StatusOK, domain.GetResponse[OpsOverviewView]{
		Item: OpsOverviewView{
			OpenRequests:  overview.OpenRequests,
			ActiveTasks:   overview.ActiveTasks,
			PendingOutbox: overview.PendingOutbox,
			ApprovalQueue: overview.ApprovalQueue,
			HumanQueue:    overview.HumanQueue,
		},
		VisibleHLC: model.VisibleHLC,
	})
}

func (m *HTTPManager) handleEvents(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.eventList()
	items = applyEventFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[EventView]{Items: page, NextCursor: next, OrderBy: "global_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleEventByID(c *gin.Context) {
	eventID := strings.TrimSpace(c.Param("id"))
	if eventID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.eventByID(eventID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[EventView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleRequests(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.requestList()
	items = applyRequestFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[RequestView]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleRequestByID(c *gin.Context) {
	requestID := strings.TrimSpace(c.Param("id"))
	if requestID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.requestByID(requestID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[RequestView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleTasks(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.taskList()
	items = applyTaskFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[TaskView]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleTaskByID(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.taskByID(taskID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[TaskView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleSchedules(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.scheduleList()
	items = applyScheduleFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[ScheduleView]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleScheduleByID(c *gin.Context) {
	scheduledTaskID := strings.TrimSpace(c.Param("id"))
	if scheduledTaskID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.scheduleByID(scheduledTaskID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[ScheduleView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleApprovals(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.approvalList()
	items = applyApprovalFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[ApprovalView]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleApprovalByID(c *gin.Context) {
	approvalRequestID := strings.TrimSpace(c.Param("id"))
	if approvalRequestID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.approvalByID(approvalRequestID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[ApprovalView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleHumanWaits(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.humanWaitList()
	items = applyHumanWaitFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[HumanWaitView]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleHumanWaitByID(c *gin.Context) {
	humanWaitID := strings.TrimSpace(c.Param("id"))
	if humanWaitID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.humanWaitByID(humanWaitID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[HumanWaitView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleDeadletters(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
	items := model.deadletterList()
	items = applyDeadletterFiltersGin(items, c)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[DeadletterView]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleDeadletterByID(c *gin.Context) {
	deadletterID := strings.TrimSpace(c.Param("id"))
	if deadletterID == "" {
		c.Status(http.StatusNotFound)
		return
	}
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	item, ok := model.deadletterByID(deadletterID)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, domain.GetResponse[DeadletterView]{Item: item, VisibleHLC: model.VisibleHLC})
}

func (m *HTTPManager) handleHumanActions(c *gin.Context) {
	minHLC, waitTimeout := parseReadWaitGin(c)
	model, err := m.loadReadModel(c.Request.Context(), minHLC, waitTimeout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limit, offset := parseLimitAndCursor(c.Query("limit"), c.Query("cursor"))
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
	items = applyHumanActionFiltersGin(items, c)
	sortHumanActions(items)
	page, next := paginateSlice(items, offset, limit)
	c.JSON(http.StatusOK, domain.ListResponse[humanActionQueueEntry]{Items: page, NextCursor: next, OrderBy: "updated_hlc desc", VisibleHLC: model.VisibleHLC})
}
