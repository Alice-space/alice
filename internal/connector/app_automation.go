package connector

import (
	"context"
	"strings"

	"github.com/Alice-space/alice/internal/sessionkey"
)

type AutomationRunner interface {
	Run(ctx context.Context)
}

func (a *App) SetAutomationRunner(runner AutomationRunner) {
	if a == nil {
		return
	}
	a.automationMu.Lock()
	a.automationRunner = runner
	a.automationMu.Unlock()
}

func (a *App) startBackgroundAutomation(ctx context.Context) {
	if a == nil {
		return
	}
	a.automationMu.Lock()
	runner := a.automationRunner
	a.automationMu.Unlock()
	if runner != nil {
		go runner.Run(ctx)
		return
	}
	if a.processor != nil {
		go a.sessionStateFlushLoop(ctx)
	}
}

// IsSessionActive reports whether any session matching the given sessionKey's
// visibility prefix is currently processing a user message.
// The automation engine calls this before executing a scheduled task to skip
// execution when the user is actively conversing, avoiding interruption.
//
// Uses sessionkey.VisibilityKey to strip scene/seed suffixes, so a task
// targeting "chat_id:oc_xxx|scene:work" correctly detects an active run on
// "chat_id:oc_xxx" or "chat_id:oc_xxx|scene:chat".
func (a *App) IsSessionActive(sessionKey string) bool {
	if a == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}
	queryVis := sessionkey.VisibilityKey(sessionKey)
	if queryVis == "" {
		return false
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	for activeKey := range a.state.active {
		if sessionkey.VisibilityKey(activeKey) == queryVis {
			return true
		}
	}
	return false
}
