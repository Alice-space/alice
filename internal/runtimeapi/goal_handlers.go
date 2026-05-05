package runtimeapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/oklog/ulid/v2"
)

func (s *Server) handleGoalGet(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	goal, err := s.automation.GetGoal(scopeCtx.scope)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrGoalNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "goal": goal})
}

func (s *Server) handleGoalCreate(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req CreateGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	goal, err := s.buildGoalFromRequest(req, scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := s.automation.ReplaceGoal(goal)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=goal_create actor=%s scope=%s:%s goal=%s", scopeCtx.actorID, created.Scope.Kind, created.Scope.ID, created.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "goal": created})
	if s.goalExecutor != nil {
		go s.goalExecutor.ExecuteGoal(c.Request.Context(), scopeCtx.scope)
	}
}

func (s *Server) handleGoalPause(c *gin.Context) {
	s.handleGoalStatusUpdate(c, automation.GoalStatusPaused, "goal_pause")
}

func (s *Server) handleGoalResume(c *gin.Context) {
	s.handleGoalStatusUpdate(c, automation.GoalStatusActive, "goal_resume")
}

func (s *Server) handleGoalComplete(c *gin.Context) {
	s.handleGoalStatusUpdate(c, automation.GoalStatusComplete, "goal_complete")
}

func (s *Server) handleGoalStatusUpdate(c *gin.Context, status automation.GoalStatus, action string) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := s.automation.PatchGoal(scopeCtx.scope, func(goal *automation.GoalTask) error {
		goal.Status = status
		return nil
	})
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, automation.ErrGoalNotFound) {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=%s actor=%s scope=%s:%s goal=%s", action, scopeCtx.actorID, updated.Scope.Kind, updated.Scope.ID, updated.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "goal": updated})
	if status == automation.GoalStatusActive && s.goalExecutor != nil {
		go s.goalExecutor.ExecuteGoal(c.Request.Context(), scopeCtx.scope)
	}
}

func (s *Server) handleGoalDelete(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.automation.DeleteGoal(scopeCtx.scope); err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, automation.ErrGoalNotFound) {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=goal_delete actor=%s scope=%s:%s", scopeCtx.actorID, scopeCtx.scope.Kind, scopeCtx.scope.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) buildGoalFromRequest(req CreateGoalRequest, scopeCtx automationScopeContext) (automation.GoalTask, error) {
	objective := strings.TrimSpace(req.Objective)
	if objective == "" {
		return automation.GoalTask{}, errors.New("objective is required")
	}
	now := time.Now().Local()
	deadlineIn := defaultGoalDeadline
	if ds := strings.TrimSpace(req.DeadlineIn); ds != "" {
		d, err := time.ParseDuration(ds)
		if err != nil {
			return automation.GoalTask{}, fmt.Errorf("invalid deadline_in %q: %w", ds, err)
		}
		if d <= 0 {
			return automation.GoalTask{}, errors.New("deadline_in must be positive")
		}
		deadlineIn = d
	}
	resumeThreadID := strings.TrimSpace(scopeCtx.session.ResumeThreadID)
	goalStatus := automation.GoalStatusActive
	if resumeThreadID == "" {
		goalStatus = automation.GoalStatusWaitingForSession
	}
	sessionKey := scopeSessionKey(scopeCtx.session)
	if !strings.Contains(sessionKey, "|work:") {
		return automation.GoalTask{}, errors.New("goal creation is only supported in work sessions; use @bot #work to start a work thread first")
	}
	goal := automation.GoalTask{
		ID:         "goal_" + strings.ToLower(ulid.Make().String()),
		Objective:  objective,
		Status:     goalStatus,
		ThreadID:   resumeThreadID,
		DeadlineAt: now.Add(deadlineIn),
		Scope:      scopeCtx.scope,
		Route:      scopeCtx.route,
		Creator:    scopeCtx.creator,
		SessionKey: scopeSessionKey(scopeCtx.session),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if sourceMsgID := strings.TrimSpace(scopeCtx.session.SourceMessageID); sourceMsgID != "" {
		goal.Route = automation.Route{ReceiveIDType: "source_message_id", ReceiveID: sourceMsgID}
	}
	return automation.NormalizeGoal(goal), nil
}

const defaultGoalDeadline = 48 * time.Hour
