package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/logging"
)

type taskMessageSender interface {
	SendTextMessage(ctx context.Context, receiveIDType, receiveID, text string) (string, error)
	SendCardMessage(ctx context.Context, receiveIDType, receiveID, cardContent string) (string, error)
}

type taskUrgentSender interface {
	UrgentApp(ctx context.Context, messageID, userIDType string, userIDs []string) error
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

	var (
		dispatch taskDispatch
		err      error
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("automation user task panic: %v", recovered)
			logging.Errorf("automation user task panic id=%s scope=%s:%s err=%v", task.ID, task.Scope.Kind, task.Scope.ID, recovered)
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
	if err != nil {
		return
	}
	// Persist the new Claude thread ID for the next run (sticky thread).
	if strings.TrimSpace(dispatch.nextThreadID) != "" &&
		strings.TrimSpace(dispatch.nextThreadID) != strings.TrimSpace(task.Action.ResumeThreadID) {
		if patchErr := e.store.RecordTaskResumeThreadID(task.ID, dispatch.nextThreadID); patchErr != nil {
			logging.Warnf("persist resume_thread_id failed id=%s err=%v", task.ID, patchErr)
		}
	}
	// Bootstrap source_message_id: on the first normal (non-signal, non-forced-
	// card) successful send, record the returned message ID so future runs can
	// reply in-thread.  Signal/forceCard sends are exceptional messages and
	// should not become the permanent thread anchor.
	if strings.TrimSpace(dispatch.firstMessageID) != "" &&
		strings.TrimSpace(task.Action.SourceMessageID) == "" &&
		dispatch.signal == nil && !dispatch.forceCard {
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

func (e *Engine) userTaskContext(ctx context.Context, task Task) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, e.userTaskTimeoutDuration())
}

// effectiveRoute returns the delivery route for a task run.
// If action.SourceMessageID is set (bootstrapped from a prior run), it takes
// precedence over task.Route so the message is posted as a reply in the same
// Feishu thread via the Reply API (receive_id_type="source_message_id").
func effectiveRoute(task Task) Route {
	task = NormalizeTask(task)
	if id := task.Action.SourceMessageID; id != "" {
		return Route{ReceiveIDType: "source_message_id", ReceiveID: id}
	}
	return task.Route
}

func (e *Engine) executeUserTask(ctx context.Context, task Task) (taskDispatch, error) {
	if e.sender == nil {
		return taskDispatch{}, errors.New("automation sender is nil")
	}
	task = NormalizeTask(task)
	// Validate the effective route (source_message_id takes precedence when set).
	route := effectiveRoute(task)
	if strings.TrimSpace(route.ReceiveIDType) == "" || strings.TrimSpace(route.ReceiveID) == "" {
		return taskDispatch{}, errors.New("task route is incomplete")
	}
	dispatch, err := e.buildTaskDispatch(ctx, task)
	if err != nil {
		return taskDispatch{}, err
	}
	if dispatch.forceCard {
		messageID, err := e.sendCardWithFallback(ctx, task, route, dispatch.cardContent)
		if err == nil {
			dispatch.firstMessageID = messageID
			e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
			return dispatch, nil
		}
		if strings.TrimSpace(dispatch.text) == "" {
			return dispatch, errors.New("warning card send failed and no text fallback is available")
		}
		messageID, err = e.sendTextWithFallback(ctx, task, route, dispatch.text)
		if err != nil {
			return dispatch, err
		}
		dispatch.firstMessageID = messageID
		e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
		return dispatch, nil
	}
	if taskPrefersCard(task) {
		cardContent, err := buildTaskCardContent(task, dispatch.text)
		if err != nil {
			return dispatch, err
		}
		messageID, err := e.sendCardWithFallback(ctx, task, route, cardContent)
		if err != nil {
			return dispatch, err
		}
		dispatch.firstMessageID = messageID
		e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
		return dispatch, nil
	}
	messageID, err := e.sendTextWithFallback(ctx, task, route, dispatch.text)
	if err != nil {
		return dispatch, err
	}
	dispatch.firstMessageID = messageID
	e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
	return dispatch, nil
}

// sendTextWithFallback sends text via route.  If route is a source_message_id
// override (thread reply) and delivery fails, it falls back to task.Route so a
// stale source_message_id cannot permanently silence a task.
func (e *Engine) sendTextWithFallback(ctx context.Context, task Task, route Route, text string) (string, error) {
	messageID, err := e.sendTextDispatch(ctx, route.ReceiveIDType, route.ReceiveID, text)
	if err != nil && route.ReceiveIDType == "source_message_id" {
		logging.Warnf("thread reply failed, falling back to task route id=%s err=%v", task.ID, err)
		return e.sendTextDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, text)
	}
	return messageID, err
}

// sendCardWithFallback is the card equivalent of sendTextWithFallback.
func (e *Engine) sendCardWithFallback(ctx context.Context, task Task, route Route, cardContent string) (string, error) {
	messageID, err := e.sendCardDispatch(ctx, route.ReceiveIDType, route.ReceiveID, cardContent)
	if err != nil && route.ReceiveIDType == "source_message_id" {
		logging.Warnf("thread card reply failed, falling back to task route id=%s err=%v", task.ID, err)
		return e.sendCardDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, cardContent)
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

func (e *Engine) sendCardDispatch(ctx context.Context, receiveIDType, receiveID, cardContent string) (string, error) {
	if sender, ok := any(e.sender).(taskMessageSender); ok {
		return sender.SendCardMessage(ctx, receiveIDType, receiveID, cardContent)
	}
	if err := e.sender.SendCard(ctx, receiveIDType, receiveID, cardContent); err != nil {
		return "", err
	}
	return "", nil
}

func (e *Engine) maybeSendTaskUrgent(ctx context.Context, task Task, dispatch taskDispatch, messageID string) {
	if !shouldUrgentTaskSignal(task, dispatch, messageID) {
		return
	}
	sender, ok := any(e.sender).(taskUrgentSender)
	if !ok {
		return
	}
	userIDType, userID, ok := taskUrgentRecipient(task.Creator)
	if !ok {
		return
	}
	if err := sender.UrgentApp(ctx, messageID, userIDType, []string{userID}); err != nil {
		logging.Warnf(
			"send automation urgent notification failed id=%s state_key=%s message=%s: %v",
			task.ID,
			task.Action.StateKey,
			messageID,
			err,
		)
	}
}

func shouldUrgentTaskSignal(task Task, dispatch taskDispatch, messageID string) bool {
	task = NormalizeTask(task)
	if strings.TrimSpace(messageID) == "" {
		return false
	}
	if task.Route.ReceiveIDType != "chat_id" {
		return false
	}
	return dispatch.signal != nil && dispatch.signal.kind == "needs_human"
}

func taskUrgentRecipient(actor Actor) (string, string, bool) {
	if openID := strings.TrimSpace(actor.OpenID); openID != "" {
		return "open_id", openID, true
	}
	if userID := strings.TrimSpace(actor.UserID); userID != "" {
		return "user_id", userID, true
	}
	return "", "", false
}

func (e *Engine) buildTaskDispatch(ctx context.Context, task Task) (taskDispatch, error) {
	task = NormalizeTask(task)
	runAt := e.nowTime()
	switch task.Action.Type {
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
		threadID := task.Action.ResumeThreadID
		provider := strings.ToLower(strings.TrimSpace(task.Action.Provider))
		if (provider == "codex" || provider == "kimi") && task.Action.StateKey != "" {
			threadID = task.Action.StateKey
		}
		result, err := runner.Run(ctx, agentbridge.RunRequest{
			ThreadID:        threadID,
			AgentName:       "scheduler",
			UserText:        composePromptWithPrefix(task.Action.PromptPrefix, task.Action.Personality, "", prompt, threadID != ""),
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
			Text:           message,
			MentionUserIDs: task.Action.MentionUserIDs,
		})
		if err != nil {
			return taskDispatch{}, err
		}
		return taskDispatch{text: text, nextThreadID: strings.TrimSpace(result.NextThreadID)}, nil
	default:
		return taskDispatch{}, fmt.Errorf("unsupported action type %q", task.Action.Type)
	}
}

// composePromptWithPrefix prepends a system prefix to userText for new threads.
// On resume (isResume=true), userText is returned as-is.
func composePromptWithPrefix(promptPrefix, personality, noReplyToken, userText string, isResume bool) string {
	if isResume {
		return userText
	}
	parts := make([]string, 0, 3)
	if p := strings.TrimSpace(promptPrefix); p != "" {
		parts = append(parts, p)
	}
	if p := strings.TrimSpace(personality); p != "" {
		parts = append(parts, "Preferred response style/personality: "+p+".")
	}
	if t := strings.TrimSpace(noReplyToken); t != "" {
		parts = append(parts, "If no reply is appropriate, return exactly this token and nothing else: "+t)
	}
	prefix := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if prefix == "" {
		return userText
	}
	return prefix + "\n\n" + strings.TrimSpace(userText)
}
