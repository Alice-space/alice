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
// Thread isolation: if either the query key or an active key carries a
// thread-specific token (seed or thread), only sessions in the *same* thread
// are considered a match. This prevents a conversation in one Feishu work-thread
// from blocking automation tasks that target a different thread in the same group.
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
	queryThread := sessionkey.ThreadScope(sessionKey)

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	for activeKey := range a.state.active {
		if sessionkey.VisibilityKey(activeKey) != queryVis {
			continue
		}
		// Both keys share the same base chat. When either side carries a
		// thread scope (seed/thread token), require an exact thread match so
		// that activity in one thread does not block tasks in another thread.
		activeThread := sessionkey.ThreadScope(activeKey)
		if queryThread != "" || activeThread != "" {
			if queryThread != activeThread {
				continue
			}
		}
		return true
	}
	return false
}
