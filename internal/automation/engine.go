package automation

import (
	"context"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/messaging"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/go-co-op/gocron/v2"
)

type Sender = messaging.AutomationSender

type LLMRunner interface {
	Run(ctx context.Context, req llm.RunRequest) (llm.RunResult, error)
}

type SystemTaskFunc func(ctx context.Context)
type UserTaskCompletionHook func(task Task, err error)

const defaultUserTaskTimeout = 10 * time.Minute
const defaultWorkflowTaskTimeout = 24 * time.Hour

const taskSignalNeedsHuman = "needs_human"

type Engine struct {
	store           *Store
	sender          Sender
	runtimeMu       sync.RWMutex
	llmRunner       LLMRunner
	workflowRunner  WorkflowRunner
	userTaskHook    UserTaskCompletionHook
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
	text        string
	cardContent string
	forceCard   bool
	signal      *taskSignal
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
