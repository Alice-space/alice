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
	mu        sync.Mutex
	latest    map[string]uint64
	active    map[string]activeSession
}

const (
	idleSummaryScanInterval   = 60 * time.Second
	sessionStateFlushInterval = 1 * time.Second
)

type activeSession struct {
	version uint64
	cancel  context.CancelFunc
	eventID string
}

func NewApp(cfg config.Config, processor *Processor) *App {
	return &App{
		cfg:       cfg,
		queue:     make(chan Job, cfg.QueueCapacity),
		processor: processor,
		latest:    make(map[string]uint64),
		active:    make(map[string]activeSession),
	}
}

func (a *App) Run(ctx context.Context) error {
	defer a.flushSessionState()

	for i := 0; i < a.cfg.WorkerConcurrency; i++ {
		go a.workerLoop(ctx, i)
	}
	if a.processor != nil {
		go a.idleSummaryLoop(ctx)
		go a.sessionStateFlushLoop(ctx)
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
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("ws client stopped: %w", err)
		}
		return nil
	}
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
				continue
			}
			if err := a.processor.FlushSessionStateIfDirty(); err != nil {
				log.Printf("flush session state failed: %v", err)
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

func (a *App) workerLoop(ctx context.Context, idx int) {
	log.Printf("worker started id=%d", idx)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.queue:
			if !a.shouldProcessJob(job) {
				log.Printf("drop stale job event_id=%s session=%s version=%d", job.EventID, job.SessionKey, job.SessionVersion)
				continue
			}

			jobCtx, cancel := context.WithCancel(ctx)
			if !a.markSessionActive(job, cancel) {
				cancel()
				log.Printf("skip stale job before run event_id=%s session=%s version=%d", job.EventID, job.SessionKey, job.SessionVersion)
				continue
			}

			a.processor.ProcessJob(jobCtx, job)
			cancel()
			a.clearSessionActive(job)
		}
	}
}

func (a *App) onMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	logIncomingEventDebug(event)
	if !shouldProcessIncomingMessage(event, a.cfg.FeishuBotOpenID, a.cfg.FeishuBotUserID) {
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
			logging.Debugf("incoming message ignored source=feishu_im event_id=%s", eventID(event))
			return nil
		}
		log.Printf("build job failed: %v", err)
		logging.Debugf("incoming message rejected source=feishu_im event_id=%s err=%v", eventID(event), err)
		return nil
	}

	queued, cancelActive, canceledEventID := a.enqueueJob(job)
	if !queued {
		log.Printf("queue full, drop event_id=%s", job.EventID)
		return nil
	}
	if cancelActive != nil {
		cancelActive()
		log.Printf("steer active job canceled old_event_id=%s new_event_id=%s session=%s", canceledEventID, job.EventID, job.SessionKey)
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

func (a *App) enqueueJob(job *Job) (queued bool, cancelActive context.CancelFunc, canceledEventID string) {
	if job == nil {
		return false, nil, ""
	}

	if strings.TrimSpace(job.SessionKey) == "" {
		job.SessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	nextVersion := a.latest[job.SessionKey] + 1
	job.SessionVersion = nextVersion

	select {
	case a.queue <- *job:
		a.latest[job.SessionKey] = nextVersion
		if active, ok := a.active[job.SessionKey]; ok {
			cancelActive = active.cancel
			canceledEventID = active.eventID
		}
		return true, cancelActive, canceledEventID
	default:
		return false, nil, ""
	}
}

func (a *App) shouldProcessJob(job Job) bool {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" || job.SessionVersion == 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.latest[sessionKey] == job.SessionVersion
}

func (a *App) markSessionActive(job Job, cancel context.CancelFunc) bool {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" || job.SessionVersion == 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.latest[sessionKey] != job.SessionVersion {
		return false
	}
	a.active[sessionKey] = activeSession{
		version: job.SessionVersion,
		cancel:  cancel,
		eventID: job.EventID,
	}
	return true
}

func (a *App) clearSessionActive(job Job) {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	active, ok := a.active[sessionKey]
	if !ok {
		return
	}
	if active.version == job.SessionVersion {
		delete(a.active, sessionKey)
	}
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
