package automation

import (
	"context"
	"sync"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/messaging"
	"github.com/go-co-op/gocron/v2"
)

type Sender = messaging.AutomationSender

// SessionActivityChecker checks whether a session is currently processing a
// user message. The automation engine uses this to skip task execution when
// the target session is busy, avoiding interruption of user conversations.
type SessionActivityChecker interface {
	IsSessionActive(sessionKey string) bool
}

// SessionActivityGate extends SessionActivityChecker with the ability to
// register an automation task run as an active session entry, so that:
//   - subsequent IsSessionActive checks (including from other ticks) see the
//     session as busy
//   - incoming user messages can interrupt the task via the provided cancel
//     function (using the same version-based interruption mechanism used for
//     user-initiated LLM runs)
type SessionActivityGate interface {
	SessionActivityChecker
	TryAcquireSession(sessionKey string, cancel context.CancelCauseFunc) bool
	ReleaseSession(sessionKey string)
}

type LLMRunner interface {
	Run(ctx context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error)
}

type SystemTaskFunc func(ctx context.Context)
type UserTaskCompletionHook func(task Task, err error)

const defaultUserTaskTimeout = 10 * time.Minute
const defaultMaxConcurrentTasks = 4

type Engine struct {
	store              *Store
	sender             Sender
	runtimeMu          sync.RWMutex
	llmRunner          LLMRunner
	userTaskHook       UserTaskCompletionHook
	sessionChecker     SessionActivityChecker
	runEnv             map[string]string
	userTaskTimeout    time.Duration
	tick               time.Duration
	maxClaim           int
	now                func() time.Time
	systemsMu          sync.Mutex
	systemTasks        map[string]*systemTaskRuntime
	schedulerMu        sync.Mutex
	scheduler          gocron.Scheduler
	lastSkipLog        sync.Map // task.ID -> time.Time; used to rate-limit "session busy" log
	taskSem            chan struct{}
	maxConcurrentTasks int
	watchdogMu         sync.Mutex
	watchdogLastAlert  map[string]time.Time
}

type taskDispatch struct {
	text           string
	nextThreadID   string
	firstMessageID string
	finalSent      bool
}

type systemTaskRuntime struct {
	name     string
	interval time.Duration
	run      SystemTaskFunc
	running  bool
}

func NewEngine(store *Store, sender Sender) *Engine {
	return &Engine{
		store:              store,
		sender:             sender,
		userTaskTimeout:    defaultUserTaskTimeout,
		tick:               time.Second,
		maxClaim:           32,
		now:                time.Now,
		systemTasks:        make(map[string]*systemTaskRuntime),
		maxConcurrentTasks: defaultMaxConcurrentTasks,
		taskSem:            make(chan struct{}, defaultMaxConcurrentTasks),
	}
}

func (e *Engine) Run(ctx context.Context) {
	if e == nil {
		return
	}
	if err := e.startSystemScheduler(ctx); err != nil {
		logging.Errorf("automation start system scheduler failed: %v", err)
	}
	defer e.stopSystemScheduler()

	ticker := time.NewTicker(e.tickDuration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := e.nowTime()
			e.runUserTasks(ctx, now)
		}
	}
}
