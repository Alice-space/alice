package runtimeapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/automation"
)

func (s *Server) handleAutomationTaskList(c *gin.Context) {
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
	limit := 20
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		if _, err := fmt.Sscanf(rawLimit, "%d", &limit); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
	}
	tasks, err := s.automation.ListTasks(scopeCtx.scope, c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "count": len(tasks), "tasks": tasks})
}

func (s *Server) handleAutomationTaskCreate(c *gin.Context) {
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
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	task, err := s.buildTaskFromRequest(req, scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := s.automation.CreateTask(task)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": created})
}

func (s *Server) handleAutomationTaskGet(c *gin.Context) {
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
	task, err := s.automation.GetTask(strings.TrimSpace(c.Param("taskID")))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if task.Scope != scopeCtx.scope {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found in current scope"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": task})
}

func (s *Server) handleAutomationTaskPatch(c *gin.Context) {
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
	taskID := strings.TrimSpace(c.Param("taskID"))
	current, err := s.automation.GetTask(taskID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if current.Scope != scopeCtx.scope {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found in current scope"})
		return
	}
	if !canManageTask(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for task update"})
		return
	}
	patchBytes, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := applyTaskPatch(current, patchBytes, c.ContentType(), scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persisted, err := s.automation.PatchTask(taskID, func(task *automation.Task) error {
		*task = updated
		return nil
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": persisted})
}

func (s *Server) handleAutomationTaskDelete(c *gin.Context) {
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
	taskID := strings.TrimSpace(c.Param("taskID"))
	deleted, err := s.automation.PatchTask(taskID, func(task *automation.Task) error {
		if task.Scope != scopeCtx.scope {
			return errors.New("task not found in current scope")
		}
		if !canManageTask(*task, scopeCtx.actorID) {
			return errors.New("permission denied for task delete")
		}
		task.Status = automation.TaskStatusDeleted
		task.NextRunAt = time.Time{}
		return nil
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": deleted})
}
