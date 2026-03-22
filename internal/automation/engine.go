package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/go-co-op/gocron/v2"
)

type Sender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
	SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error
}

type LLMRunner interface {
	Run(ctx context.Context, req llm.RunRequest) (llm.RunResult, error)
}

type SystemTaskFunc func(ctx context.Context)

const defaultUserTaskTimeout = 10 * time.Minute

type Engine struct {
	store           *Store
	sender          Sender
	runtimeMu       sync.RWMutex
	llmRunner       LLMRunner
	workflowRunner  WorkflowRunner
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

func (e *Engine) SetLLMRunner(runner LLMRunner) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	e.llmRunner = runner
}

func (e *Engine) SetWorkflowRunner(runner WorkflowRunner) {
	if e == nil {
		return
	}
	e.runtimeMu.Lock()
	defer e.runtimeMu.Unlock()
	e.workflowRunner = runner
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

func (e *Engine) RegisterSystemTask(name string, interval time.Duration, run SystemTaskFunc) error {
	if e == nil {
		return errors.New("engine is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("system task name is empty")
	}
	if interval <= 0 {
		return errors.New("system task interval must be > 0")
	}
	if run == nil {
		return errors.New("system task function is nil")
	}

	e.systemsMu.Lock()
	defer e.systemsMu.Unlock()
	if _, exists := e.systemTasks[name]; exists {
		return errors.New("system task already exists")
	}
	e.systemTasks[name] = &systemTaskRuntime{
		name:     name,
		interval: interval,
		run:      run,
	}
	return nil
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

func (e *Engine) startSystemScheduler(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.schedulerMu.Lock()
	if e.scheduler != nil {
		e.schedulerMu.Unlock()
		return nil
	}
	e.schedulerMu.Unlock()

	e.systemsMu.Lock()
	tasks := make([]*systemTaskRuntime, 0, len(e.systemTasks))
	for _, task := range e.systemTasks {
		if task == nil || task.run == nil {
			continue
		}
		tasks = append(tasks, task)
	}
	e.systemsMu.Unlock()
	if len(tasks) == 0 {
		return nil
	}

	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		taskName := task.name
		taskRun := task.run
		_, err := scheduler.NewJob(
			gocron.DurationJob(task.interval),
			gocron.NewTask(func() {
				if !e.markSystemTaskRunning(taskName) {
					return
				}
				defer e.finishSystemTask(taskName)
				defer func() {
					if recovered := recover(); recovered != nil {
						logging.Errorf("automation system task panic name=%s err=%v", taskName, recovered)
					}
				}()
				taskRun(ctx)
			}),
		)
		if err != nil {
			_ = scheduler.Shutdown()
			return fmt.Errorf("register system task %q failed: %w", taskName, err)
		}
	}
	scheduler.Start()

	e.schedulerMu.Lock()
	e.scheduler = scheduler
	e.schedulerMu.Unlock()
	return nil
}

func (e *Engine) stopSystemScheduler() {
	if e == nil {
		return
	}
	e.schedulerMu.Lock()
	scheduler := e.scheduler
	e.scheduler = nil
	e.schedulerMu.Unlock()
	if scheduler != nil {
		_ = scheduler.Shutdown()
	}
}

func (e *Engine) markSystemTaskRunning(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	e.systemsMu.Lock()
	defer e.systemsMu.Unlock()
	task, ok := e.systemTasks[name]
	if !ok || task == nil || task.running {
		return false
	}
	task.running = true
	return true
}

func (e *Engine) finishSystemTask(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	e.systemsMu.Lock()
	defer e.systemsMu.Unlock()
	task, ok := e.systemTasks[name]
	if !ok || task == nil {
		return
	}
	task.running = false
}

func (e *Engine) runUserTasks(ctx context.Context, now time.Time) {
	if e.store == nil || e.sender == nil {
		return
	}
	claimed, err := e.store.ClaimDueTasks(now, e.maxClaim)
	if err != nil {
		logging.Errorf("claim automation tasks failed: %v", err)
		return
	}
	for _, task := range claimed {
		task := task
		go e.runUserTask(ctx, task)
	}
}

func (e *Engine) runUserTask(ctx context.Context, task Task) {
	runCtx, cancel := e.userTaskContext(ctx, task)
	defer cancel()

	err := e.executeUserTask(runCtx, task)
	if err != nil {
		logging.Errorf("run automation task failed id=%s scope=%s:%s err=%v", task.ID, task.Scope.Kind, task.Scope.ID, err)
	}
	if e.store != nil {
		if recordErr := e.store.RecordTaskResult(task.ID, e.nowTime(), err); recordErr != nil {
			logging.Errorf("record automation result failed id=%s err=%v", task.ID, recordErr)
		}
	}
}

func (e *Engine) userTaskContext(ctx context.Context, task Task) (context.Context, context.CancelFunc) {
	task = NormalizeTask(task)
	if task.Action.Type == ActionTypeRunWorkflow {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, e.userTaskTimeoutDuration())
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

func (e *Engine) executeUserTask(ctx context.Context, task Task) error {
	if e.sender == nil {
		return errors.New("automation sender is nil")
	}
	task = NormalizeTask(task)
	if strings.TrimSpace(task.Route.ReceiveIDType) == "" || strings.TrimSpace(task.Route.ReceiveID) == "" {
		return errors.New("task route is incomplete")
	}
	dispatch, err := e.buildTaskDispatch(ctx, task)
	if err != nil {
		return err
	}
	if taskPrefersCard(task) {
		return e.sender.SendCard(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, buildTaskCardContent(dispatch))
	}
	return e.sender.SendText(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch)
}

func (e *Engine) buildTaskDispatch(ctx context.Context, task Task) (string, error) {
	task = NormalizeTask(task)
	runAt := e.nowTime()
	switch task.Action.Type {
	case ActionTypeSendText:
		rendered, err := renderActionTemplate(task.Action.Text, runAt)
		if err != nil {
			return "", err
		}
		text, err := BuildDispatchText(Action{
			Type:           ActionTypeSendText,
			Text:           rendered,
			MentionUserIDs: task.Action.MentionUserIDs,
		})
		if err != nil {
			return "", err
		}
		return text, nil
	case ActionTypeRunLLM:
		runner := e.llmRunnerValue()
		if runner == nil {
			return "", errors.New("automation llm runner is nil")
		}
		prompt, err := renderActionTemplate(task.Action.Prompt, runAt)
		if err != nil {
			return "", err
		}
		if prompt == "" {
			return "", errors.New("action prompt is empty for run_llm")
		}
		result, err := runner.Run(ctx, llm.RunRequest{
			AgentName:       "scheduler",
			UserText:        prompt,
			Scene:           taskScene(task),
			Provider:        task.Action.Provider,
			Model:           task.Action.Model,
			Profile:         task.Action.Profile,
			ReasoningEffort: task.Action.ReasoningEffort,
			Personality:     task.Action.Personality,
			Env:             e.buildTaskRunEnv(task),
		})
		if err != nil {
			return "", err
		}
		reply := strings.TrimSpace(result.Reply)
		if reply == "" {
			return "", errors.New("llm reply is empty")
		}
		prefix, err := renderActionTemplate(task.Action.Text, runAt)
		if err != nil {
			return "", err
		}
		message := reply
		if prefix != "" {
			message = prefix + "\n" + reply
		}
		text, err := BuildDispatchText(Action{
			Type:           ActionTypeSendText,
			Text:           message,
			MentionUserIDs: task.Action.MentionUserIDs,
		})
		if err != nil {
			return "", err
		}
		return text, nil
	case ActionTypeRunWorkflow:
		runner := e.workflowRunnerValue()
		if runner == nil {
			return "", errors.New("automation workflow runner is nil")
		}
		prompt, err := renderActionTemplate(task.Action.Prompt, runAt)
		if err != nil {
			return "", err
		}
		if prompt == "" {
			return "", errors.New("action prompt is empty for run_workflow")
		}
		result, err := runner.Run(ctx, WorkflowRunRequest{
			Workflow:        task.Action.Workflow,
			TaskID:          task.ID,
			StateKey:        task.Action.StateKey,
			SessionKey:      task.Action.SessionKey,
			Scene:           taskScene(task),
			Prompt:          prompt,
			Provider:        task.Action.Provider,
			Model:           task.Action.Model,
			Profile:         task.Action.Profile,
			ReasoningEffort: task.Action.ReasoningEffort,
			Personality:     task.Action.Personality,
			Env:             e.buildTaskRunEnv(task),
		})
		if err != nil {
			return "", err
		}
		reply := strings.TrimSpace(result.Message)
		if reply == "" {
			return "", errors.New("workflow reply is empty")
		}
		prefix, err := renderActionTemplate(task.Action.Text, runAt)
		if err != nil {
			return "", err
		}
		message := reply
		if prefix != "" {
			message = prefix + "\n" + reply
		}
		text, err := BuildDispatchText(Action{
			Type:           ActionTypeSendText,
			Text:           message,
			MentionUserIDs: task.Action.MentionUserIDs,
		})
		if err != nil {
			return "", err
		}
		return text, nil
	default:
		return "", fmt.Errorf("unsupported action type %q", task.Action.Type)
	}
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

func taskPrefersCard(task Task) bool {
	task = NormalizeTask(task)
	return strings.Contains(task.Action.SessionKey, "|scene:work")
}

func buildTaskCardContent(markdown string) string {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = " "
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":     "markdown",
					"content": "**回复**\n" + reply,
				},
			},
		},
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}

func renderActionTemplate(raw string, now time.Time) (string, error) {
	template := strings.TrimSpace(raw)
	if template == "" {
		return "", nil
	}
	if now.IsZero() {
		now = time.Now().Local()
	}
	now = now.Local()
	template = strings.NewReplacer(
		"{{now}}", now.Format(time.RFC3339),
		"{{date}}", now.Format("2006-01-02"),
		"{{time}}", now.Format("15:04:05"),
		"{{unix}}", strconv.FormatInt(now.Unix(), 10),
	).Replace(template)
	rendered, err := actionTemplateRenderer.RenderString("automation-action", template, map[string]any{
		"Now":  now,
		"Date": now.Format("2006-01-02"),
		"Time": now.Format("15:04:05"),
		"Unix": now.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("render action template failed: %w", err)
	}
	return strings.TrimSpace(rendered), nil
}

func (e *Engine) buildTaskRunEnv(task Task) map[string]string {
	task = NormalizeTask(task)
	ctx := mcpbridge.SessionContext{
		ReceiveIDType: task.Route.ReceiveIDType,
		ReceiveID:     task.Route.ReceiveID,
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

func (e *Engine) workflowRunnerValue() WorkflowRunner {
	if e == nil {
		return nil
	}
	e.runtimeMu.RLock()
	defer e.runtimeMu.RUnlock()
	return e.workflowRunner
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
	if sessionKey := strings.TrimSpace(task.Action.SessionKey); sessionKey != "" {
		return sessionKey
	}
	if strings.TrimSpace(task.Route.ReceiveIDType) == "" || strings.TrimSpace(task.Route.ReceiveID) == "" {
		return ""
	}
	return strings.TrimSpace(task.Route.ReceiveIDType) + ":" + strings.TrimSpace(task.Route.ReceiveID)
}

func taskScene(task Task) string {
	switch sessionKey := strings.TrimSpace(task.Action.SessionKey); {
	case strings.Contains(sessionKey, "|scene:work"):
		return "work"
	case strings.Contains(sessionKey, "|scene:chat"):
		return "chat"
	default:
		return "chat"
	}
}
