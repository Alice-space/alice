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
)

type TextSender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
}

type cardSender interface {
	SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error
}

type LLMRunner interface {
	Run(ctx context.Context, req llm.RunRequest) (llm.RunResult, error)
}

type SystemTaskFunc func(ctx context.Context)

const defaultUserTaskTimeout = 10 * time.Minute

type Engine struct {
	store           *Store
	sender          TextSender
	llmRunner       LLMRunner
	workflowRunner  WorkflowRunner
	userTaskTimeout time.Duration
	tick            time.Duration
	maxClaim        int
	now             func() time.Time
	systemsMu       sync.Mutex
	systemTasks     map[string]*systemTaskRuntime
}

type systemTaskRuntime struct {
	name     string
	interval time.Duration
	run      SystemTaskFunc
	nextRun  time.Time
	running  bool
}

var actionTemplateRenderer = prompting.NewLoader(".")

type taskDispatch struct {
	text        string
	cardContent string
}

func NewEngine(store *Store, sender TextSender) *Engine {
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
	e.llmRunner = runner
}

func (e *Engine) SetWorkflowRunner(runner WorkflowRunner) {
	if e == nil {
		return
	}
	e.workflowRunner = runner
}

func (e *Engine) SetUserTaskTimeout(timeout time.Duration) {
	if e == nil {
		return
	}
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
	firstRun := e.nowTime()
	e.systemTasks[name] = &systemTaskRuntime{
		name:     name,
		interval: interval,
		run:      run,
		nextRun:  firstRun,
	}
	return nil
}

func (e *Engine) Run(ctx context.Context) {
	if e == nil {
		return
	}
	ticker := time.NewTicker(e.tickDuration())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := e.nowTime()
			e.runSystemTasks(ctx, now)
			e.runUserTasks(ctx, now)
		}
	}
}

func (e *Engine) runSystemTasks(ctx context.Context, now time.Time) {
	e.systemsMu.Lock()
	due := make([]*systemTaskRuntime, 0, len(e.systemTasks))
	for _, task := range e.systemTasks {
		if task == nil || task.run == nil || task.running {
			continue
		}
		if task.nextRun.After(now) {
			continue
		}
		task.running = true
		task.nextRun = now.Add(task.interval)
		due = append(due, task)
	}
	e.systemsMu.Unlock()

	for _, task := range due {
		task := task
		go func() {
			defer e.finishSystemTask(task.name)
			defer func() {
				if recovered := recover(); recovered != nil {
					logging.Errorf("automation system task panic name=%s err=%v", task.name, recovered)
				}
			}()
			task.run(ctx)
		}()
	}
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
	if e == nil || e.userTaskTimeout <= 0 {
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
	if strings.TrimSpace(dispatch.cardContent) != "" {
		if sender, ok := e.sender.(cardSender); ok {
			if err := sender.SendCard(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.cardContent); err == nil {
				return nil
			}
		}
	}
	return e.sender.SendText(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.text)
}

func (e *Engine) buildTaskDispatch(ctx context.Context, task Task) (taskDispatch, error) {
	task = NormalizeTask(task)
	runAt := e.nowTime()
	switch task.Action.Type {
	case ActionTypeSendText:
		rendered := renderActionTemplate(task.Action.Text, runAt)
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
		if e.llmRunner == nil {
			return taskDispatch{}, errors.New("automation llm runner is nil")
		}
		prompt := renderActionTemplate(task.Action.Prompt, runAt)
		if prompt == "" {
			return taskDispatch{}, errors.New("action prompt is empty for run_llm")
		}
		result, err := e.llmRunner.Run(ctx, llm.RunRequest{
			AgentName: "scheduler",
			UserText:  prompt,
			Model:     task.Action.Model,
			Profile:   task.Action.Profile,
			Env:       buildTaskRunEnv(task),
		})
		if err != nil {
			return taskDispatch{}, err
		}
		reply := strings.TrimSpace(result.Reply)
		if reply == "" {
			return taskDispatch{}, errors.New("llm reply is empty")
		}
		prefix := renderActionTemplate(task.Action.Text, runAt)
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
		if e.workflowRunner == nil {
			return taskDispatch{}, errors.New("automation workflow runner is nil")
		}
		prompt := renderActionTemplate(task.Action.Prompt, runAt)
		if prompt == "" {
			return taskDispatch{}, errors.New("action prompt is empty for run_workflow")
		}
		result, err := e.workflowRunner.Run(ctx, WorkflowRunRequest{
			Workflow: task.Action.Workflow,
			TaskID:   task.ID,
			StateKey: task.Action.StateKey,
			Prompt:   prompt,
			Model:    task.Action.Model,
			Profile:  task.Action.Profile,
			Env:      buildTaskRunEnv(task),
		})
		if err != nil {
			return taskDispatch{}, err
		}
		reply := strings.TrimSpace(result.Message)
		if reply == "" {
			return taskDispatch{}, errors.New("workflow reply is empty")
		}
		prefix := renderActionTemplate(task.Action.Text, runAt)
		message := reply
		if prefix != "" {
			message = prefix + "\n\n" + reply
		}
		text, err := BuildDispatchText(Action{
			Type:           ActionTypeSendText,
			Text:           message,
			MentionUserIDs: task.Action.MentionUserIDs,
		})
		if err != nil {
			return taskDispatch{}, err
		}
		dispatch := taskDispatch{text: text}
		if task.Action.Workflow == WorkflowCodeArmy && len(task.Action.MentionUserIDs) == 0 {
			dispatch.cardContent = buildMarkdownCardContent(message)
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
		return time.Now().UTC()
	}
	now := e.now()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func renderActionTemplate(raw string, now time.Time) string {
	template := strings.TrimSpace(raw)
	if template == "" {
		return ""
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
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
		return ""
	}
	return strings.TrimSpace(rendered)
}

func buildTaskRunEnv(task Task) map[string]string {
	task = NormalizeTask(task)
	ctx := mcpbridge.SessionContext{
		ReceiveIDType: task.Route.ReceiveIDType,
		ReceiveID:     task.Route.ReceiveID,
		ActorUserID:   task.Creator.UserID,
		ActorOpenID:   task.Creator.OpenID,
		SessionKey:    task.Action.SessionKey,
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
	return ctx.ToEnv()
}

func buildMarkdownCardContent(markdown string) string {
	content := strings.TrimSpace(markdown)
	if content == "" {
		content = " "
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
					"content": content,
				},
			},
		},
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}
