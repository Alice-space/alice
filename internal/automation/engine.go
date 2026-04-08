package automation

import (
	"context"
	"sync"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/messaging"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/go-co-op/gocron/v2"
)

type Sender = messaging.AutomationSender

// SessionActivityChecker checks whether a session is currently processing a
// user message. The automation engine uses this to skip task execution when
// the target session is busy, avoiding interruption of user conversations.
type SessionActivityChecker interface {
	IsSessionActive(sessionKey string) bool
}

type LLMRunner interface {
	Run(ctx context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error)
}

type SystemTaskFunc func(ctx context.Context)
type UserTaskCompletionHook func(task Task, err error)

const defaultUserTaskTimeout = 10 * time.Minute

type Engine struct {
	store           *Store
	sender          Sender
	runtimeMu       sync.RWMutex
	llmRunner       LLMRunner
	userTaskHook    UserTaskCompletionHook
	sessionChecker  SessionActivityChecker
	runEnv          map[string]string
	userTaskTimeout time.Duration
	tick            time.Duration
	maxClaim        int
	now             func() time.Time
	systemsMu       sync.Mutex
	systemTasks     map[string]*systemTaskRuntime
	schedulerMu     sync.Mutex
	scheduler       gocron.Scheduler
}

type taskSignal struct {
	kind    string
	message string
	pause   bool
}

type taskDispatch struct {
	text           string
	cardContent    string
	forceCard      bool
	signal         *taskSignal
	nextThreadID   string
	firstMessageID string // Feishu message ID of the first sent message; used to bootstrap source_message_id
}

type systemTaskRuntime struct {
	name     string
	interval time.Duration
	run      SystemTaskFunc
	running  bool
}

var actionTemplateRenderer = prompting.NewLoader(".")

func NewEngine(store *Store, sender Sender) *Engine {
	return &Engine{
		store:           store,
		sender:          sender,
		userTaskTimeout: defaultUserTaskTimeout,
		tick:            time.Second,
		maxClaim:        32,
		now:             time.Now,
		systemTasks:     make(map[string]*systemTaskRuntime),
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
