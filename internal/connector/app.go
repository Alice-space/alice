package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
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
					logging.Warnf("flush runtime state failed: %v", err)
				}
				continue
			}
			if err := a.processor.FlushSessionStateIfDirty(); err != nil {
				logging.Warnf("flush session state failed: %v", err)
			}
			if err := a.FlushRuntimeStateIfDirty(); err != nil {
				logging.Warnf("flush runtime state failed: %v", err)
			}
		}
	}
}

func (a *App) flushSessionState() {
	if a.processor == nil {
		return
	}
	if err := a.processor.FlushSessionStateIfDirty(); err != nil {
		logging.Warnf("flush session state on exit failed: %v", err)
	}
}

func (a *App) flushRuntimeState() {
	if err := a.FlushRuntimeStateIfDirty(); err != nil {
		logging.Warnf("flush runtime state on exit failed: %v", err)
	}
}

func (a *App) workerLoop(ctx context.Context, idx int) {
	logging.Infof("worker started id=%d", idx)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.queue:
			if ctx.Err() != nil {
				logging.Infof(
					"worker stopping with queued job preserved event_id=%s session=%s version=%d",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
				)
				return
			}
			sessionKey := normalizeJobSessionKey(job)
			if sessionKey == "" {
				logging.Warnf("drop invalid job event_id=%s session=%s version=%d", job.EventID, job.SessionKey, job.SessionVersion)
				a.completePendingJob(job)
				continue
			}
			if a.isSupersededJob(sessionKey, job.SessionVersion) {
				logging.Infof(
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
				logging.Infof(
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
					logging.Infof(
						"job interrupted, keep pending for restart notification event_id=%s session=%s version=%d state=%s",
						job.EventID,
						job.SessionKey,
						job.SessionVersion,
						result,
					)
					continue
				}
				logging.Warnf(
					"job interrupted, drop in-progress event_id=%s session=%s version=%d state=%s",
					job.EventID,
					job.SessionKey,
					job.SessionVersion,
					result,
				)
				a.completePendingJob(job)
			default:
				logging.Warnf(
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
		logging.Warnf("build job failed: %v", err)
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
		logging.Warnf("queue full, drop event_id=%s", job.EventID)
		return nil
	}
	if cancelActive != nil {
		cancelActive()
		logging.Infof(
			"interrupt active job session=%s canceled_event_id=%s new_event_id=%s new_version=%d",
			job.SessionKey,
			canceledEventID,
			job.EventID,
			job.SessionVersion,
		)
	}
	logging.Infof(
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
