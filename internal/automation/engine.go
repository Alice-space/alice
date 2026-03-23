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

const taskSignalNeedsHuman = "needs_human"

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

	dispatch, err := e.executeUserTask(runCtx, task)
	if err != nil {
		logging.Errorf("run automation task failed id=%s scope=%s:%s err=%v", task.ID, task.Scope.Kind, task.Scope.ID, err)
	}
	if e.store != nil {
		now := e.nowTime()
		var recordErr error
		switch {
		case err != nil:
			recordErr = e.store.RecordTaskResult(task.ID, now, err)
		case dispatch.signal != nil:
			recordErr = e.store.RecordTaskSignal(
				task.ID,
				now,
				dispatch.signal.kind,
				dispatch.signal.message,
				dispatch.signal.pause,
			)
		default:
			recordErr = e.store.RecordTaskResult(task.ID, now, nil)
		}
		if recordErr != nil {
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

func (e *Engine) executeUserTask(ctx context.Context, task Task) (taskDispatch, error) {
	if e.sender == nil {
		return taskDispatch{}, errors.New("automation sender is nil")
	}
	task = NormalizeTask(task)
	if strings.TrimSpace(task.Route.ReceiveIDType) == "" || strings.TrimSpace(task.Route.ReceiveID) == "" {
		return taskDispatch{}, errors.New("task route is incomplete")
	}
	dispatch, err := e.buildTaskDispatch(ctx, task)
	if err != nil {
		return taskDispatch{}, err
	}
	if dispatch.forceCard {
		if err := e.sender.SendCard(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.cardContent); err == nil {
			return dispatch, nil
		}
		if strings.TrimSpace(dispatch.text) == "" {
			return dispatch, errors.New("warning card send failed and no text fallback is available")
		}
		return dispatch, e.sender.SendText(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.text)
	}
	if taskPrefersCard(task) {
		return dispatch, e.sender.SendCard(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, buildTaskCardContent(task, dispatch.text))
	}
	return dispatch, e.sender.SendText(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.text)
}

func (e *Engine) buildTaskDispatch(ctx context.Context, task Task) (taskDispatch, error) {
	task = NormalizeTask(task)
	runAt := e.nowTime()
	switch task.Action.Type {
	case ActionTypeSendText:
		rendered, err := renderActionTemplate(task.Action.Text, runAt)
		if err != nil {
			return taskDispatch{}, err
		}
		text, err := BuildDispatchText(Action{
			Type:           ActionTypeSendText,
			Text:           rendered,
			MentionUserIDs: task.Action.MentionUserIDs,
		})
		if err != nil {
			return taskDispatch{}, err
		}
		return taskDispatch{text: text}, nil
	case ActionTypeRunLLM:
		runner := e.llmRunnerValue()
		if runner == nil {
			return taskDispatch{}, errors.New("automation llm runner is nil")
		}
		prompt, err := renderActionTemplate(task.Action.Prompt, runAt)
		if err != nil {
			return taskDispatch{}, err
		}
		if prompt == "" {
			return taskDispatch{}, errors.New("action prompt is empty for run_llm")
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
			return taskDispatch{}, err
		}
		reply := strings.TrimSpace(result.Reply)
		if reply == "" {
			return taskDispatch{}, errors.New("llm reply is empty")
		}
		prefix, err := renderActionTemplate(task.Action.Text, runAt)
		if err != nil {
			return taskDispatch{}, err
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
			return taskDispatch{}, err
		}
		return taskDispatch{text: text}, nil
	case ActionTypeRunWorkflow:
		runner := e.workflowRunnerValue()
		if runner == nil {
			return taskDispatch{}, errors.New("automation workflow runner is nil")
		}
		prompt, err := renderActionTemplate(task.Action.Prompt, runAt)
		if err != nil {
			return taskDispatch{}, err
		}
		if prompt == "" {
			return taskDispatch{}, errors.New("action prompt is empty for run_workflow")
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
			return taskDispatch{}, err
		}
		signal := detectWorkflowSignal(result.Commands)
		reply := strings.TrimSpace(result.Message)
		if reply == "" {
			reply = fallbackWorkflowReply(signal)
		}
		if reply == "" {
			return taskDispatch{}, errors.New("workflow reply is empty")
		}
		prefix, err := renderActionTemplate(task.Action.Text, runAt)
		if err != nil {
			return taskDispatch{}, err
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
			return taskDispatch{}, err
		}
		dispatch := taskDispatch{
			text:   text,
			signal: signal,
		}
		if signal != nil && signal.kind == taskSignalNeedsHuman {
			dispatch.forceCard = true
			dispatch.cardContent = buildTaskWarningCardContent(task, text, signal.message)
		}
		return dispatch, nil
	default:
		return taskDispatch{}, fmt.Errorf("unsupported action type %q", task.Action.Type)
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

func buildTaskCardContent(task Task, markdown string) string {
	task = NormalizeTask(task)
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
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": taskCardTitle(task),
			},
			"template": "blue",
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":     "markdown",
					"content": reply,
				},
			},
		},
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}

func buildTaskWarningCardContent(task Task, markdown string, reason string) string {
	reply := strings.TrimSpace(markdown)
	if reply == "" {
		reply = "自动任务已暂停，等待人工介入。"
	}
	elements := []any{
		taskCardMarkdown("**状态**\n自动任务已暂停，等待人工介入。"),
		taskCardMarkdown("**任务**\n" + taskCardTitle(task)),
	}
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		elements = append(elements, taskCardMarkdown("**原因**\n"+trimmedReason))
	}
	elements = append(elements, taskCardMarkdown("**上下文**\n"+reply))
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "需要人工介入",
			},
			"template": "orange",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}

func taskCardMarkdown(content string) map[string]any {
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func taskCardTitle(task Task) string {
	task = NormalizeTask(task)
	if task.Title != "" {
		return task.Title
	}
	if task.ID != "" {
		return task.ID
	}
	return "自动任务"
}

func detectWorkflowSignal(commands []WorkflowCommand) *taskSignal {
	for _, command := range commands {
		if reason, ok := parseNeedsHumanCommand(command.Text); ok {
			return &taskSignal{
				kind:    taskSignalNeedsHuman,
				message: reason,
				pause:   true,
			}
		}
	}
	return nil
}

func parseNeedsHumanCommand(command string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 || fields[0] != "/alice" {
		return "", false
	}
	keyword := strings.ToLower(strings.ReplaceAll(fields[1], "_", "-"))
	switch {
	case keyword == "needs-human" || keyword == "needshuman":
		reason := strings.TrimSpace(strings.Join(fields[2:], " "))
		if reason == "" {
			reason = "workflow requested human intervention"
		}
		return reason, true
	case keyword == "needs" && len(fields) >= 3 && strings.EqualFold(fields[2], "human"):
		reason := strings.TrimSpace(strings.Join(fields[3:], " "))
		if reason == "" {
			reason = "workflow requested human intervention"
		}
		return reason, true
	default:
		return "", false
	}
}

func fallbackWorkflowReply(signal *taskSignal) string {
	if signal == nil {
		return ""
	}
	if signal.kind != taskSignalNeedsHuman {
		return ""
	}
	if strings.TrimSpace(signal.message) == "" {
		return "需要人工介入，自动任务已暂停。"
	}
	return "需要人工介入，自动任务已暂停。\n\n原因：" + signal.message
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
