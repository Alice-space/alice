package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/logging"
)

type taskMessageSender interface {
	SendTextMessage(ctx context.Context, receiveIDType, receiveID, text string) (string, error)
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
		select {
		case e.taskSem <- struct{}{}:
		default:
			logging.Warnf("automation task deferred (max concurrency reached) id=%s", task.ID)
			if err := e.store.UnclaimTask(task.ID); err != nil {
				logging.Errorf("unclaim deferred task failed id=%s: %v", task.ID, err)
			}
			continue
		}
		go func() {
			defer func() { <-e.taskSem }()
			e.runUserTask(ctx, task)
		}()
	}
}

func (e *Engine) runUserTask(ctx context.Context, task Task) {
	sk := taskSessionKey(task)

	runCtx, cancel := e.userTaskContext(ctx, task)
	defer cancel(nil)

	if checker := e.sessionCheckerValue(); checker != nil {
		if gate, ok := checker.(SessionActivityGate); ok {
			if !gate.TryAcquireSession(sk, cancel) {
				e.logSessionBusy(task, sk)
				unclaimOrFallback(e, task)
				return
			}
			defer gate.ReleaseSession(sk)
		} else if checker.IsSessionActive(sk) {
			e.logSessionBusy(task, sk)
			unclaimOrFallback(e, task)
			return
		}
	}

	e.sendTaskStartNotification(runCtx, task)

	var (
		dispatch taskDispatch
		err      error
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("automation user task panic: %v", recovered)
			logging.Errorf("automation user task panic id=%s scope=%s:%s err=%v", task.ID, task.Scope.Kind, task.Scope.ID, recovered)
		}
		if err != nil && runCtx.Err() != nil {
			logging.Infof("automation task interrupted id=%s session=%s", task.ID, sk)
			unclaimOrFallback(e, task)
			return
		}
		e.recordUserTaskOutcome(task, dispatch, err)
		e.notifyUserTaskCompletion(task, err)
	}()

	dispatch, err = e.executeUserTask(runCtx, task)
	if err != nil {
		logging.Errorf("run automation task failed id=%s scope=%s:%s err=%v", task.ID, task.Scope.Kind, task.Scope.ID, err)
	}
}

func (e *Engine) recordUserTaskOutcome(task Task, dispatch taskDispatch, err error) {
	if e.store == nil {
		return
	}
	now := e.nowTime()
	var recordErr error
	if err != nil {
		recordErr = e.store.RecordTaskResult(task.ID, now, err)
	} else {
		recordErr = e.store.RecordTaskResult(task.ID, now, nil)
	}
	if recordErr != nil {
		logging.Errorf("record automation result failed id=%s err=%v", task.ID, recordErr)
	}
	if err != nil {
		return
	}
	if strings.TrimSpace(dispatch.nextThreadID) != "" &&
		strings.TrimSpace(dispatch.nextThreadID) != strings.TrimSpace(task.ResumeThreadID) {
		if patchErr := e.store.RecordTaskResumeThreadID(task.ID, dispatch.nextThreadID); patchErr != nil {
			logging.Warnf("persist resume_thread_id failed id=%s err=%v", task.ID, patchErr)
		}
	}
	if strings.TrimSpace(dispatch.firstMessageID) != "" &&
		strings.TrimSpace(task.SourceMessageID) == "" {
		if patchErr := e.store.RecordTaskSourceMessageID(task.ID, dispatch.firstMessageID); patchErr != nil {
			logging.Warnf("persist source_message_id failed id=%s err=%v", task.ID, patchErr)
		}
	}
}

func (e *Engine) notifyUserTaskCompletion(task Task, err error) {
	hook := e.userTaskCompletionHookValue()
	if hook == nil {
		return
	}
	task = NormalizeTask(task)
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.Errorf("automation user task completion hook panic id=%s err=%v", task.ID, recovered)
		}
	}()
	hook(task, err)
}

func (e *Engine) userTaskContext(ctx context.Context, task Task) (context.Context, context.CancelCauseFunc) {
	timeout := e.userTaskTimeoutDuration()
	timeoutCtx, timeoutCancel := context.WithTimeoutCause(ctx, timeout, errSessionInterrupted)
	runCtx, cancel := context.WithCancelCause(timeoutCtx)
	return runCtx, func(cause error) {
		timeoutCancel()
		cancel(cause)
	}
}

var errSessionInterrupted = errors.New("automation: session interrupted by user message")

func effectiveRoute(task Task) Route {
	task = NormalizeTask(task)
	if id := task.SourceMessageID; id != "" {
		return Route{ReceiveIDType: "source_message_id", ReceiveID: id}
	}
	return task.Route
}

func (e *Engine) executeUserTask(ctx context.Context, task Task) (taskDispatch, error) {
	if e.sender == nil {
		return taskDispatch{}, errors.New("automation sender is nil")
	}
	task = NormalizeTask(task)
	route := effectiveRoute(task)
	if strings.TrimSpace(route.ReceiveIDType) == "" || strings.TrimSpace(route.ReceiveID) == "" {
		return taskDispatch{}, errors.New("task route is incomplete")
	}
	dispatch, err := e.buildTaskDispatch(ctx, task, route)
	if err != nil {
		return taskDispatch{}, err
	}
	if dispatch.finalSent {
		return dispatch, nil
	}
	messageID, err := e.sendTextWithFallback(ctx, task, route, dispatch.text)
	if err != nil {
		return dispatch, err
	}
	dispatch.rememberFirstMessageID(messageID)
	return dispatch, nil
}

func (d *taskDispatch) rememberFirstMessageID(messageID string) {
	if d == nil || strings.TrimSpace(d.firstMessageID) != "" {
		return
	}
	d.firstMessageID = strings.TrimSpace(messageID)
}

func (e *Engine) sendTextWithFallback(ctx context.Context, task Task, route Route, text string) (string, error) {
	messageID, err := e.sendTextDispatch(ctx, route.ReceiveIDType, route.ReceiveID, text)
	if err != nil && route.ReceiveIDType == "source_message_id" {
		logging.Warnf("thread reply failed, falling back to task route id=%s err=%v", task.ID, err)
		return e.sendTextDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, text)
	}
	return messageID, err
}

func (e *Engine) sendTextDispatch(ctx context.Context, receiveIDType, receiveID, text string) (string, error) {
	if sender, ok := any(e.sender).(taskMessageSender); ok {
		return sender.SendTextMessage(ctx, receiveIDType, receiveID, text)
	}
	if err := e.sender.SendText(ctx, receiveIDType, receiveID, text); err != nil {
		return "", err
	}
	return "", nil
}

func (e *Engine) buildTaskDispatch(ctx context.Context, task Task, route Route) (taskDispatch, error) {
	task = NormalizeTask(task)
	runAt := e.nowTime()

	runner := e.llmRunnerValue()
	if runner == nil {
		return taskDispatch{}, errors.New("automation llm runner is nil")
	}

	prompt, err := renderActionTemplate(task.Prompt, runAt)
	if err != nil {
		return taskDispatch{}, err
	}
	if prompt == "" {
		return taskDispatch{}, errors.New("prompt is empty")
	}

	threadID := ""
	if !task.Fresh {
		threadID = task.ResumeThreadID
	}

	progress := &taskProgressDispatcher{ctx: ctx, engine: e, task: task, route: route}
	logging.Infof("automation task llm call id=%s fresh=%v thread=%s", task.ID, task.Fresh, threadID)

	result, err := runner.Run(ctx, agentbridge.RunRequest{
		ThreadID:   threadID,
		AgentName:  "scheduler",
		UserText:   prompt,
		Scene:      taskScene(task),
		Env:        e.buildTaskRunEnv(task),
		OnProgress: progress.Send,
	})
	if err != nil {
		return taskDispatch{}, err
	}

	logging.Infof("automation task llm done id=%s reply_len=%d next_thread=%s", task.ID, len(result.Reply), strings.TrimSpace(result.NextThreadID))

	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		return taskDispatch{}, errors.New("llm reply is empty")
	}

	dispatch := taskDispatch{
		text:           reply,
		nextThreadID:   strings.TrimSpace(result.NextThreadID),
		firstMessageID: progress.FirstMessageID(),
	}

	if progress.LastMessage() == reply {
		dispatch.finalSent = true
	}

	return dispatch, nil
}

type taskProgressDispatcher struct {
	ctx    context.Context
	engine *Engine
	task   Task
	route  Route

	mu             sync.Mutex
	lastMessage    string
	lastDelivered  string
	firstMessageID string
}

func (d *taskProgressDispatcher) Send(message string) {
	if d == nil || d.engine == nil {
		return
	}
	normalized := strings.TrimSpace(message)
	if normalized == "" || strings.HasPrefix(normalized, "[file_change] ") {
		return
	}
	d.mu.Lock()
	if normalized == d.lastMessage {
		d.mu.Unlock()
		return
	}
	d.lastMessage = normalized
	d.mu.Unlock()
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	messageID, err := d.engine.sendTextWithFallback(ctx, d.task, d.route, normalized)
	if err != nil {
		logging.Warnf("send automation agent message failed id=%s: %v", d.task.ID, err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastDelivered = normalized
	if strings.TrimSpace(d.firstMessageID) == "" {
		d.firstMessageID = strings.TrimSpace(messageID)
	}
}

func (d *taskProgressDispatcher) FirstMessageID() string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.TrimSpace(d.firstMessageID)
}

func (d *taskProgressDispatcher) LastMessage() string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.TrimSpace(d.lastDelivered)
}

func (e *Engine) logSessionBusy(task Task, sk string) {
	const skipLogInterval = time.Minute
	now := e.nowTime()
	if last, ok := e.lastSkipLog.Load(task.ID); !ok || now.Sub(last.(time.Time)) >= skipLogInterval {
		logging.Infof("automation task skipped (session busy) id=%s session=%s", task.ID, sk)
		e.lastSkipLog.Store(task.ID, now)
	}
}

func unclaimOrFallback(e *Engine, task Task) {
	if err := e.store.UnclaimTask(task.ID); err != nil {
		logging.Errorf("unclaim automation task failed id=%s err=%v; recording no-op result", task.ID, err)
		if recErr := e.store.RecordTaskResult(task.ID, e.nowTime(), nil); recErr != nil {
			logging.Errorf("fallback record result failed id=%s err=%v", task.ID, recErr)
		}
	}
}

func (e *Engine) sendTaskStartNotification(ctx context.Context, task Task) {
	if e.sender == nil {
		return
	}
	task = NormalizeTask(task)
	title := task.Title
	if title == "" {
		title = "未命名任务"
	}
	text := "定时任务「" + title + "」开始运行..."
	route := effectiveRoute(task)
	e.sendTextDispatch(ctx, route.ReceiveIDType, route.ReceiveID, text)
}
