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
	cfg         config.Config
	queue       chan Job
	processor   *Processor
	workerWG    sync.WaitGroup
	mu          sync.Mutex
	latest      map[string]uint64
	pending     map[string]Job
	mediaWindow map[string][]mediaWindowEntry
	sessionMu   map[string]*sync.Mutex

	runtimeStatePath           string
	runtimeStateVersion        uint64
	runtimeStateFlushedVersion uint64
	now                        func() time.Time
}

const (
	idleSummaryScanInterval   = 60 * time.Second
	sessionStateFlushInterval = 1 * time.Second
	defaultGroupContextWindow = 5 * time.Minute
	maxMediaWindowEntries     = 20
)

func NewApp(cfg config.Config, processor *Processor) *App {
	return &App{
		cfg:         cfg,
		queue:       make(chan Job, cfg.QueueCapacity),
		processor:   processor,
		latest:      make(map[string]uint64),
		pending:     make(map[string]Job),
		mediaWindow: make(map[string][]mediaWindowEntry),
		sessionMu:   make(map[string]*sync.Mutex),
		now:         time.Now,
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
	if a.processor != nil {
		go a.idleSummaryLoop(workerCtx)
		go a.sessionStateFlushLoop(workerCtx)
	}

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

			sessionMu := a.sessionMutex(sessionKey)
			sessionMu.Lock()
			result := a.processor.ProcessJobState(ctx, job)
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
	accepted := shouldProcessIncomingMessage(event, a.cfg.FeishuBotOpenID, a.cfg.FeishuBotUserID)
	a.cacheGroupContextWindow(event, accepted)
	if !accepted {
		logging.Debugf(
			"incoming message ignored source=feishu_im event_id=%s reason=group_without_bot_mention chat_type=%s",
			eventID(event),
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
	if event != nil && event.Event != nil {
		a.resolveJobSessionKey(job, event.Event.Message)
	}
	a.mergeRecentGroupContextWindow(job)
	job.BotOpenID = strings.TrimSpace(a.cfg.FeishuBotOpenID)
	job.BotUserID = strings.TrimSpace(a.cfg.FeishuBotUserID)

	queued, _, _ := a.enqueueJob(job)
	if !queued {
		log.Printf("queue full, drop event_id=%s", job.EventID)
		return nil
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

	a.mu.Lock()
	latest := make(map[string]struct{}, len(a.latest))
	for key := range a.latest {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		latest[trimmed] = struct{}{}
	}
	pending := make(map[string]struct{}, len(a.pending))
	for _, pendingJob := range a.pending {
		trimmed := strings.TrimSpace(pendingJob.SessionKey)
		if trimmed == "" {
			continue
		}
		pending[trimmed] = struct{}{}
	}
	a.mu.Unlock()

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
	job.WorkflowPhase = normalizeJobWorkflowPhase(job.WorkflowPhase)

	a.mu.Lock()
	defer a.mu.Unlock()

	nextVersion := a.latest[job.SessionKey] + 1
	job.SessionVersion = nextVersion

	select {
	case a.queue <- *job:
		a.latest[job.SessionKey] = nextVersion
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

func (a *App) sessionMutex(sessionKey string) *sync.Mutex {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if mu, ok := a.sessionMu[sessionKey]; ok && mu != nil {
		return mu
	}
	mu := &sync.Mutex{}
	a.sessionMu[sessionKey] = mu
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
