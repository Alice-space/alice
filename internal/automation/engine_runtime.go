package automation

import (
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/sessionctx"
	"github.com/Alice-space/alice/internal/sessionkey"
)

func (e *Engine) SetLLMRunner(runner LLMRunner) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	e.llmRunner = runner
}

func (e *Engine) SetUserTaskCompletionHook(hook UserTaskCompletionHook) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	e.userTaskHook = hook
}

func (e *Engine) SetSessionActivityChecker(checker SessionActivityChecker) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	e.sessionChecker = checker
}

func (e *Engine) sessionCheckerValue() SessionActivityChecker {
	if e == nil {
		return nil
	}
	e.runtimeMu.RLock()
	defer e.runtimeMu.RUnlock()
	return e.sessionChecker
}

func (e *Engine) SetRunEnv(env map[string]string) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	if len(env) == 0 {
		e.runEnv = nil
		return
	}
	e.runEnv = make(map[string]string, len(env))
	for key, value := range env {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		e.runEnv[key] = value
	}
}

func (e *Engine) SetUserTaskTimeout(timeout time.Duration) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	if timeout <= 0 {
		e.userTaskTimeout = defaultUserTaskTimeout
		return
	}
	e.userTaskTimeout = timeout
}

func (e *Engine) userTaskTimeoutDuration() time.Duration {
	if e == nil {
		return defaultUserTaskTimeout
	}
	e.runtimeMu.RLock()
	defer e.runtimeMu.RUnlock()
	if e.userTaskTimeout <= 0 {
		return defaultUserTaskTimeout
	}
	return e.userTaskTimeout
}

func (e *Engine) tickDuration() time.Duration {
	if e == nil || e.tick <= 0 {
		return time.Second
	}
	return e.tick
}

func (e *Engine) nowTime() time.Time {
	if e == nil || e.now == nil {
		return time.Now().Local()
	}
	now := e.now()
	if now.IsZero() {
		return time.Now().Local()
	}
	return now.Local()
}

func (e *Engine) buildTaskRunEnv(task Task) map[string]string {
	task = NormalizeTask(task)
	receiveIDType := task.Route.ReceiveIDType
	receiveID := task.Route.ReceiveID
	if receiveIDType == "source_message_id" {
		switch task.Scope.Kind {
		case ScopeKindChat:
			receiveIDType = "chat_id"
			receiveID = task.Scope.ID
		case ScopeKindUser:
			receiveIDType = "open_id"
			receiveID = task.Scope.ID
		}
	}
	ctx := sessionctx.SessionContext{
		ReceiveIDType: receiveIDType,
		ReceiveID:     receiveID,
		ActorUserID:   task.Creator.UserID,
		ActorOpenID:   task.Creator.OpenID,
		SessionKey:    taskSessionKey(task),
	}
	switch task.Scope.Kind {
	case ScopeKindChat:
		ctx.ChatType = "group"
	case ScopeKindUser:
		ctx.ChatType = "p2p"
	}
	if err := ctx.Validate(); err != nil {
		return nil
	}
	env := ctx.ToEnv()
	for key, value := range e.runEnvSnapshot() {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		env[key] = value
	}
	return env
}

func (e *Engine) llmRunnerValue() LLMRunner {
	if e == nil {
		return nil
	}
	e.runtimeMu.RLock()
	defer e.runtimeMu.RUnlock()
	return e.llmRunner
}

func (e *Engine) userTaskCompletionHookValue() UserTaskCompletionHook {
	if e == nil {
		return nil
	}
	e.runtimeMu.RLock()
	defer e.runtimeMu.RUnlock()
	return e.userTaskHook
}

func (e *Engine) runEnvSnapshot() map[string]string {
	if e == nil {
		return nil
	}
	e.runtimeMu.RLock()
	defer e.runtimeMu.RUnlock()
	if len(e.runEnv) == 0 {
		return nil
	}
	copied := make(map[string]string, len(e.runEnv))
	for key, value := range e.runEnv {
		copied[key] = value
	}
	return copied
}

func taskSessionKey(task Task) string {
	task = NormalizeTask(task)
	if sessionKey := task.SessionKey; sessionKey != "" {
		return sessionKey
	}
	return sessionkey.Build(task.Route.ReceiveIDType, task.Route.ReceiveID)
}

func taskScene(task Task) string {
	switch sessionKey := task.SessionKey; {
	case strings.Contains(sessionKey, "|scene:work"):
		return "work"
	case strings.Contains(sessionKey, "|scene:chat"):
		return "chat"
	default:
		return "chat"
	}
}
