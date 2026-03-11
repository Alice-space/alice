package ops

import (
	"context"
	"net/http"
	"strings"

	"alice/internal/domain"

	"github.com/gin-gonic/gin"
)

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
	actionID := resolveAdminActionIDGin(c)
	// Simplified implementation - full version would validate and process event
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID})
}

func (m *HTTPManager) handleSubmitFire(c *gin.Context) {
	if !m.config.AdminScheduleFireReplayEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin submit fires is disabled"})
		return
	}
	actionID := resolveAdminActionIDGin(c)
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID})
}

func (m *HTTPManager) handleResolveApproval(c *gin.Context) {
	actionID := resolveAdminActionIDGin(c)
	var in domain.ResolveApprovalRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Simplified - full implementation would validate and process
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID})
}

func (m *HTTPManager) handleResolveWait(c *gin.Context) {
	actionID := resolveAdminActionIDGin(c)
	var in domain.ResolveHumanWaitRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID})
}

func (m *HTTPManager) handleTaskCancel(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id is required"})
		return
	}
	actionID := resolveAdminActionIDGin(c)
	c.JSON(http.StatusAccepted, domain.WriteAcceptedResponse{Accepted: true, AdminActionID: actionID})
}
