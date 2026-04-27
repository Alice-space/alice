package connector

import (
	"context"
	"strings"
	"sync"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/logging"
)

func (a *App) resolveJobSessionKey(job *Job, message *larkim.EventMessage) {
	if job == nil {
		return
	}
	candidates := buildSessionKeyCandidatesForMessage(job.ReceiveIDType, job.ReceiveID, message)
	if len(candidates) == 0 {
		return
	}

	resolved := a.findExistingSessionKey(candidates)
	if resolved == "" {
		resolved = candidates[0]
	}
	if resolved == "" {
		return
	}
	if a.processor != nil {
		a.processor.rememberSessionAliases(resolved, candidates...)
	}

	original := strings.TrimSpace(job.SessionKey)
	if original == resolved {
		return
	}
	job.SessionKey = resolved
	logging.Debugf(
		"job session key normalized event_id=%s original=%s resolved=%s candidates=%q",
		job.EventID,
		original,
		resolved,
		candidates,
	)
}

func (a *App) findExistingSessionKey(candidates []string) string {
	normalized := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := strings.TrimSpace(candidate)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	if len(normalized) == 0 {
		return ""
	}

	a.state.mu.Lock()
	latest := make(map[string]struct{}, len(a.state.latest))
	for key := range a.state.latest {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		latest[trimmed] = struct{}{}
	}
	pending := make(map[string]struct{}, len(a.state.pending))
	for _, pendingJob := range a.state.pending {
		trimmed := strings.TrimSpace(pendingJob.SessionKey)
		if trimmed == "" {
			continue
		}
		pending[trimmed] = struct{}{}
	}
	a.state.mu.Unlock()

	if a.processor != nil {
		for _, candidate := range normalized {
			if resolved := strings.TrimSpace(a.processor.resolveCanonicalSessionKey(candidate)); resolved != "" {
				return resolved
			}
		}
	}
	for _, candidate := range normalized {
		if _, ok := latest[candidate]; ok {
			return candidate
		}
		if _, ok := pending[candidate]; ok {
			return candidate
		}
	}
	return ""
}

func (a *App) enqueueJob(job *Job) (queued bool, cancelActive context.CancelFunc, canceledEventID string) {
	if job == nil {
		return false, nil, ""
	}

	if strings.TrimSpace(job.SessionKey) == "" {
		job.SessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if strings.TrimSpace(job.ResourceScopeKey) == "" {
		job.ResourceScopeKey = resourceScopeKeyForJob(*job)
	}
	job.WorkflowPhase = normalizeJobWorkflowPhase(job.WorkflowPhase)

	a.state.mu.Lock()
	defer a.state.mu.Unlock()

	nextVersion := a.state.latest[job.SessionKey] + 1
	job.SessionVersion = nextVersion
	active, interruptActive := a.state.active[job.SessionKey]
	interruptActive = interruptActive && active.cancel != nil && active.version < nextVersion
	if interruptActive && isBuiltinCommandText(job.Text) && !isStopCommand(job.Text) {
		interruptActive = false
	}
	supersedeQueued := false
	for _, pendingJob := range a.state.pending {
		if strings.TrimSpace(pendingJob.SessionKey) != job.SessionKey {
			continue
		}
		if pendingJob.SessionVersion < nextVersion {
			supersedeQueued = true
			break
		}
	}

	select {
	case a.queue <- *job:
		if interruptActive {
			cancelCause := errSessionInterrupted
			if isStopCommand(job.Text) {
				cancelCause = errSessionStopped
			}
			cancelActive = func() {
				active.cancel(cancelCause)
			}
			canceledEventID = active.eventID
		}
		if interruptActive || supersedeQueued {
			if nextVersion > a.state.superseded[job.SessionKey] {
				a.state.superseded[job.SessionKey] = nextVersion
			}
			a.removeOlderPendingJobsLocked(job.SessionKey, nextVersion)
		}
		a.state.latest[job.SessionKey] = nextVersion
		a.rememberPendingJobLocked(*job)
		return true, cancelActive, canceledEventID
	default:
		return false, nil, ""
	}
}

func normalizeJobSessionKey(job Job) string {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey != "" {
		return sessionKey
	}
	return buildSessionKey(job.ReceiveIDType, job.ReceiveID)
}

func (a *App) setActiveRun(sessionKey string, version uint64, eventID string, cancel context.CancelCauseFunc) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || version == 0 || cancel == nil {
		return
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	a.state.active[sessionKey] = activeSessionRun{
		eventID: strings.TrimSpace(eventID),
		version: version,
		cancel:  cancel,
	}
}

func (a *App) clearActiveRun(sessionKey string, version uint64) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || version == 0 {
		return
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	active, ok := a.state.active[sessionKey]
	if !ok || active.version != version {
		return
	}
	delete(a.state.active, sessionKey)
}

func (a *App) isSupersededJob(sessionKey string, version uint64) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || version == 0 {
		return false
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	cutoff := a.state.superseded[sessionKey]
	return cutoff != 0 && version < cutoff
}

func (a *App) sessionMutex(sessionKey string) *sync.Mutex {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	if mu, ok := a.state.sessionMu[sessionKey]; ok && mu != nil {
		return mu
	}
	mu := &sync.Mutex{}
	a.state.sessionMu[sessionKey] = mu
	return mu
}

func parseLogLevel(level string) larkcore.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return larkcore.LogLevelDebug
	case "warn", "warning":
		return larkcore.LogLevelWarn
	case "error":
		return larkcore.LogLevelError
	default:
		return larkcore.LogLevelInfo
	}
}

func (a *App) findActiveWorkSessionKey(receiveIDType, receiveID string) string {
	receiveIDType = strings.TrimSpace(receiveIDType)
	receiveID = strings.TrimSpace(receiveID)
	if receiveIDType == "" || receiveID == "" {
		return ""
	}
	baseKey := buildSessionKey(receiveIDType, receiveID)
	if baseKey == "" {
		return ""
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	for sessionKey := range a.state.active {
		if strings.HasPrefix(strings.TrimSpace(sessionKey), baseKey) && isWorkSceneSessionKey(sessionKey) {
			return sessionKey
		}
	}
	return ""
}
