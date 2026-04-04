package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/llm"
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
		if shouldIgnoreInternalWorkflowDeliveryError(task, dispatch, err) {
			logging.Warnf(
				"ignore automation delivery error after successful internal workflow id=%s state_key=%s err=%v",
				task.ID,
				task.Action.StateKey,
				err,
			)
			err = nil
			return
		}
		logging.Errorf("run automation task failed id=%s scope=%s:%s err=%v", task.ID, task.Scope.Kind, task.Scope.ID, err)
	}
}

func shouldIgnoreInternalWorkflowDeliveryError(task Task, dispatch taskDispatch, err error) bool {
	if err == nil {
		return false
	}
	task = NormalizeTask(task)
	if task.Action.Type != ActionTypeRunWorkflow || !dispatch.completed {
		return false
	}
	stateKey := strings.TrimSpace(task.Action.StateKey)
	return strings.HasPrefix(stateKey, "campaign_dispatch:") || strings.HasPrefix(stateKey, "campaign_wake:")
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
	task = NormalizeTask(task)
	if task.Action.Type == ActionTypeRunWorkflow {
		timeout := e.userTaskTimeoutDuration()
		if timeout <= 0 {
			timeout = defaultWorkflowTaskTimeout
		}
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithTimeout(ctx, e.userTaskTimeoutDuration())
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
		messageID, err := e.sendCardDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.cardContent)
		if err == nil {
			e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
			return dispatch, nil
		}
		if strings.TrimSpace(dispatch.text) == "" {
			return dispatch, errors.New("warning card send failed and no text fallback is available")
		}
		messageID, err = e.sendTextDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.text)
		if err != nil {
			return dispatch, err
		}
		e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
		return dispatch, nil
	}
	if taskPrefersCard(task) {
		cardContent, err := buildTaskCardContent(task, dispatch.text)
		if err != nil {
			return dispatch, err
		}
		messageID, err := e.sendCardDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, cardContent)
		if err != nil {
			return dispatch, err
		}
		e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
		return dispatch, nil
	}
	messageID, err := e.sendTextDispatch(ctx, task.Route.ReceiveIDType, task.Route.ReceiveID, dispatch.text)
	if err != nil {
		return dispatch, err
	}
	e.maybeSendTaskUrgent(ctx, task, dispatch, messageID)
	return dispatch, nil
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
	return dispatch.signal != nil && dispatch.signal.kind == taskSignalNeedsHuman
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
			PromptPrefix:    task.Action.PromptPrefix,
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
		if guard := e.workflowPreflightHookValue(); guard != nil {
			decision, err := guard(ctx, task)
			if err != nil {
				return taskDispatch{}, err
			}
			if decision.Block {
				reason := strings.TrimSpace(decision.SignalMessage)
				if reason == "" {
					reason = strings.TrimSpace(decision.Message)
				}
				if reason == "" {
					reason = "workflow requested human intervention"
				}
				signalKind := strings.TrimSpace(decision.SignalKind)
				if signalKind == "" {
					signalKind = taskSignalNeedsHuman
				}
				signal := &taskSignal{
					kind:    signalKind,
					message: reason,
					pause:   true,
				}
				reply := strings.TrimSpace(decision.Message)
				if reply == "" {
					reply = fallbackWorkflowReply(signal)
				}
				text, err := BuildDispatchText(Action{
					Type:           ActionTypeSendText,
					Text:           reply,
					MentionUserIDs: task.Action.MentionUserIDs,
				})
				if err != nil {
					return taskDispatch{}, err
				}
				dispatch := taskDispatch{
					text:   text,
					signal: signal,
				}
				if decision.ForceCard || signal.kind == taskSignalNeedsHuman {
					cardContent, err := buildSignalCardContent(task, reply, signal)
					if err != nil {
						return taskDispatch{}, err
					}
					if cardContent != "" {
						dispatch.cardContent = cardContent
						dispatch.forceCard = true
					}
				}
				return dispatch, nil
			}
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
			PromptPrefix:    task.Action.PromptPrefix,
			Env:             e.buildTaskRunEnv(task),
		})
		if err != nil {
			return taskDispatch{}, err
		}
		signals := detectWorkflowSignals(result.Commands)
		signal := primaryWorkflowSignal(signals)
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
			text:      text,
			signal:    signal,
			signals:   signals,
			completed: true,
		}
		if signal != nil {
			cardContent, cardErr := buildSignalCardContent(task, text, signal)
			if cardErr != nil {
				return taskDispatch{}, cardErr
			}
			if cardContent != "" {
				dispatch.forceCard = true
				dispatch.cardContent = cardContent
			}
		}
		return dispatch, nil
	default:
		return taskDispatch{}, fmt.Errorf("unsupported action type %q", task.Action.Type)
	}
}
