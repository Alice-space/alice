package connector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/logging"
)

type App struct {
	cfg       config.Config
	queue     chan Job
	processor *Processor
	workerWG  sync.WaitGroup

	state            *runtimeStore
	now              func() time.Time
	automationMu     sync.Mutex
	automationRunner AutomationRunner
}

const (
	idleSummaryScanInterval   = 60 * time.Second
	sessionStateFlushInterval = 1 * time.Second
	defaultGroupContextWindow = 5 * time.Minute
	maxMediaWindowEntries     = 20
)

func NewApp(cfg config.Config, processor *Processor) *App {
	return &App{
		cfg:       cfg,
		queue:     make(chan Job, cfg.QueueCapacity),
		processor: processor,
		state:     newRuntimeStore(),
		now:       time.Now,
	}
}

func (a *App) Run(ctx context.Context) error {
	defer a.flushRuntimeState()
	defer a.flushSessionState()

	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()

	for i := 0; i < a.cfg.WorkerConcurrency; i++ {
		a.startWorker(workerCtx, i)
	}
	a.startBackgroundAutomation(workerCtx)

	eventHandler := larkdispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(a.onMessageReceive)
	wsClient := larkws.NewClient(
		a.cfg.FeishuAppID,
		a.cfg.FeishuAppSecret,
		larkws.WithDomain(a.cfg.FeishuBaseURL),
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(parseLogLevel(a.cfg.LogLevel)),
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- wsClient.Start(ctx)
	}()

	select {
	case <-ctx.Done():
		stopWorkers()
		a.waitWorkers()
		return nil
	case err := <-errCh:
		stopWorkers()
		a.waitWorkers()
		if err != nil {
			return fmt.Errorf("ws client stopped: %w", err)
		}
		return nil
	}
}

func (a *App) startWorker(ctx context.Context, idx int) {
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.workerLoop(ctx, idx)
	}()
}

func (a *App) waitWorkers() {
	a.workerWG.Wait()
}

func (a *App) idleSummaryLoop(ctx context.Context) {
	ticker := time.NewTicker(idleSummaryScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.processor == nil {
				continue
			}
			a.processor.RunIdleSummaryScan(ctx, a.cfg.IdleSummaryIdle)
		}
	}
}

func (a *App) sessionStateFlushLoop(ctx context.Context) {
	ticker := time.NewTicker(sessionStateFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.processor == nil {
				if err := a.FlushRuntimeStateIfDirty(); err != nil {
					log.Printf("flush runtime state failed: %v", err)
				}
				continue
			}
			if err := a.processor.FlushSessionStateIfDirty(); err != nil {
				log.Printf("flush session state failed: %v", err)
			}
			if err := a.FlushRuntimeStateIfDirty(); err != nil {
				log.Printf("flush runtime state failed: %v", err)
			}
		}
	}
}

func (a *App) flushSessionState() {
	if a.processor == nil {
		return
	}
	if err := a.processor.FlushSessionStateIfDirty(); err != nil {
		log.Printf("flush session state on exit failed: %v", err)
	}
}

func (a *App) flushRuntimeState() {
	if err := a.FlushRuntimeStateIfDirty(); err != nil {
		log.Printf("flush runtime state on exit failed: %v", err)
	}
}

func (a *App) workerLoop(ctx context.Context, idx int) {
	log.Printf("worker started id=%d", idx)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.queue:
			if ctx.Err() != nil {
				log.Printf(
					"worker stopping with queued job preserved event_id=%s session=%s version=%d",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
				)
				return
			}
			sessionKey := normalizeJobSessionKey(job)
			if sessionKey == "" {
				log.Printf("drop invalid job event_id=%s session=%s version=%d", job.EventID, job.SessionKey, job.SessionVersion)
				a.completePendingJob(job)
				continue
			}
			if a.isSupersededJob(sessionKey, job.SessionVersion) {
				log.Printf(
					"drop superseded job event_id=%s session=%s version=%d",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
				)
				a.completePendingJob(job)
				continue
			}

			sessionMu := a.sessionMutex(sessionKey)
			sessionMu.Lock()
			if a.isSupersededJob(sessionKey, job.SessionVersion) {
				sessionMu.Unlock()
				log.Printf(
					"drop superseded job after lock event_id=%s session=%s version=%d",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
				)
				a.completePendingJob(job)
				continue
			}
			runCtx, cancelRun := context.WithCancelCause(ctx)
			a.setActiveRun(sessionKey, job.SessionVersion, job.EventID, func() {
				cancelRun(errSessionInterrupted)
			})
			result := a.processor.ProcessJobState(runCtx, job)
			cancelRun(nil)
			a.clearActiveRun(sessionKey, job.SessionVersion)
			sessionMu.Unlock()
			switch result {
			case JobProcessCompleted:
				a.completePendingJob(job)
			case JobProcessPostRestartFinalize, JobProcessRetryAfterRestart:
				if ctx.Err() != nil {
					a.updatePendingJobWorkflowPhase(job, jobWorkflowPhaseRestartNotification)
					log.Printf(
						"job interrupted, keep pending for restart notification event_id=%s session=%s version=%d state=%s",
						job.EventID,
						job.SessionKey,
						job.SessionVersion,
						result,
					)
					continue
				}
				log.Printf(
					"job interrupted, drop in-progress event_id=%s session=%s version=%d state=%s",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
					result,
				)
				a.completePendingJob(job)
			default:
				log.Printf(
					"job state unknown, keep pending event_id=%s session=%s version=%d state=%s",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
					result,
				)
			}
		}
	}
}

func (a *App) onMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	logIncomingEventDebug(event)
	accepted := shouldProcessIncomingMessage(
		event,
		a.cfg.TriggerMode,
		a.cfg.TriggerPrefix,
		a.cfg.FeishuBotOpenID,
		a.cfg.FeishuBotUserID,
	)
	a.cacheGroupContextWindow(ctx, event, accepted)
	if !accepted {
		logging.Debugf(
			"incoming message ignored source=feishu_im event_id=%s reason=group_trigger_unmatched trigger_mode=%s chat_type=%s",
			eventID(event),
			normalizedTriggerMode(a.cfg.TriggerMode),
			strings.TrimSpace(deref(event.Event.Message.ChatType)),
		)
		return nil
	}

	job, err := BuildJob(event)
	if err != nil {
		if errors.Is(err, ErrIgnoreMessage) {
			if syntheticJob, ok := a.buildSyntheticMentionJob(event); ok {
				job = syntheticJob
				err = nil
			}
		}
	}
	if err != nil {
		if errors.Is(err, ErrIgnoreMessage) {
			logging.Debugf("incoming message ignored source=feishu_im event_id=%s", eventID(event))
			return nil
		}
		log.Printf("build job failed: %v", err)
		logging.Debugf("incoming message rejected source=feishu_im event_id=%s err=%v", eventID(event), err)
		return nil
	}
	normalizeIncomingGroupJobTextForTriggerMode(job, a.cfg.TriggerMode, a.cfg.TriggerPrefix)
	if event != nil && event.Event != nil {
		a.resolveJobSessionKey(job, event.Event.Message)
	}
	a.mergeRecentGroupContextWindow(job)
	job.BotOpenID = strings.TrimSpace(a.cfg.FeishuBotOpenID)
	job.BotUserID = strings.TrimSpace(a.cfg.FeishuBotUserID)

	queued, cancelActive, canceledEventID := a.enqueueJob(job)
	if !queued {
		log.Printf("queue full, drop event_id=%s", job.EventID)
		return nil
	}
	if cancelActive != nil {
		cancelActive()
		log.Printf(
			"interrupt active job session=%s canceled_event_id=%s new_event_id=%s new_version=%d",
			job.SessionKey,
			canceledEventID,
			job.EventID,
			job.SessionVersion,
		)
	}
	log.Printf(
		"job queued event_id=%s receive_id_type=%s session=%s version=%d",
		job.EventID,
		job.ReceiveIDType,
		job.SessionKey,
		job.SessionVersion,
	)
	logging.Debugf(
		"job accepted event_id=%s channel=%s receive_id_type=%s receive_id=%s source_message_id=%s normalized_text=%q",
		job.EventID,
		"feishu_im",
		job.ReceiveIDType,
		job.ReceiveID,
		job.SourceMessageID,
		job.Text,
	)

	return nil
}

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

	for _, candidate := range normalized {
		if _, ok := latest[candidate]; ok {
			return candidate
		}
		if _, ok := pending[candidate]; ok {
			return candidate
		}
	}

	if a.processor != nil {
		for _, candidate := range normalized {
			if strings.TrimSpace(a.processor.getThreadID(candidate)) != "" {
				return candidate
			}
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
	if strings.TrimSpace(job.MemoryScopeKey) == "" {
		job.MemoryScopeKey = memoryScopeKeyForJob(*job)
	}
	job.WorkflowPhase = normalizeJobWorkflowPhase(job.WorkflowPhase)

	a.state.mu.Lock()
	defer a.state.mu.Unlock()

	nextVersion := a.state.latest[job.SessionKey] + 1
	job.SessionVersion = nextVersion
	active, interruptActive := a.state.active[job.SessionKey]
	interruptActive = interruptActive && active.cancel != nil && active.version < nextVersion

	select {
	case a.queue <- *job:
		if interruptActive {
			cancelActive = active.cancel
			canceledEventID = active.eventID
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

func (a *App) setActiveRun(sessionKey string, version uint64, eventID string, cancel context.CancelFunc) {
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
