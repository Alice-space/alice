package ops

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"alice/internal/domain"

	"github.com/gin-gonic/gin"
)

// RegisterRoutesGin registers routes with gin router.
func (m *HTTPManager) RegisterRoutesGin(r gin.IRouter) {
	v1 := r.Group("/v1")
	{
		v1.GET("/ops/overview", m.handleOverview)

		v1.GET("/events", m.handleEvents)
		v1.GET("/events/:id", m.handleEventByID)
		v1.GET("/requests", m.handleRequests)
		v1.GET("/requests/:id", m.handleRequestByID)
		v1.GET("/tasks", m.handleTasks)
		v1.GET("/tasks/:id", m.handleTaskByID)
		v1.GET("/schedules", m.handleSchedules)
		v1.GET("/schedules/:id", m.handleScheduleByID)
		v1.GET("/approvals", m.handleApprovals)
		v1.GET("/approvals/:id", m.handleApprovalByID)
		v1.GET("/human-waits", m.handleHumanWaits)
		v1.GET("/human-waits/:id", m.handleHumanWaitByID)
		v1.GET("/deadletters", m.handleDeadletters)
		v1.GET("/deadletters/:id", m.handleDeadletterByID)
		v1.GET("/human-actions", m.handleHumanActions)

		admin := v1.Group("/admin")
		{
			admin.POST("/submit/events", m.handleSubmitEvent)
			admin.POST("/submit/fires", m.handleSubmitFire)
			admin.POST("/resolve/approval", m.handleResolveApproval)
			admin.POST("/resolve/wait", m.handleResolveWait)
			admin.POST("/reconcile/outbox", m.wrapAdminHook(m.hooks.ReconcileOutbox))
			admin.POST("/reconcile/schedules", m.wrapAdminHook(m.hooks.ReconcileSchedules))
			admin.POST("/rebuild/indexes", m.wrapAdminHook(m.hooks.RebuildIndexes))
			admin.POST("/replay/from/:hlc", m.handleReplayFrom)
			admin.POST("/tasks/:id/cancel", m.handleTaskCancel)
			admin.POST("/deadletters/:id/redrive", m.handleDeadletterRedrive)
		}
	}
}

// loadReadModel loads the read model with optional wait.
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
