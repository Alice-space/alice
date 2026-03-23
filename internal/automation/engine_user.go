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
