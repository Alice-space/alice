package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/sessionctx"
)

const defaultGoalTimeout = 48 * time.Hour

func (e *Engine) runGoals(ctx context.Context) {
	if e.store == nil {
		return
	}
	goals, err := e.store.ListGoals(GoalStatusActive)
	if err != nil {
		logging.Warnf("goal tick list failed: %v", err)
		return
	}
	for _, goal := range goals {
		if goal.Running {
			continue
		}
		goal := goal
		e.taskSem <- struct{}{}
		go func() {
			defer func() { <-e.taskSem }()
			if err := e.ExecuteGoal(ctx, goal.Scope); err != nil {
				logging.Infof("goal tick exec scope=%s:%s err=%v", goal.Scope.Kind, goal.Scope.ID, err)
			}
		}()
	}
}

func (e *Engine) ExecuteGoal(ctx context.Context, scope Scope) error {
	if e == nil || e.store == nil {
		return errors.New("engine or store is nil")
	}
	runner := e.llmRunnerValue()
	if runner == nil {
		return errors.New("goal runner: llm runner is nil")
	}
	goal, err := e.store.GetGoal(scope)
	if err != nil {
		return err
	}
	if goal.Status != GoalStatusActive {
		return nil
	}
	now := e.nowTime()
	if !goal.DeadlineAt.IsZero() && now.After(goal.DeadlineAt) {
		e.markGoalTimeout(goal)
		return nil
	}
	sk := goalSessionKey(goal)
	goalCtx, goalCancel := context.WithCancelCause(ctx)
	defer goalCancel(nil)
	if checker := e.sessionCheckerValue(); checker != nil {
		if gate, ok := checker.(SessionActivityGate); ok {
			if !gate.TryAcquireSession(sk, goalCancel) {
				e.logGoalSessionBusy(goal, sk)
				return nil
			}
			defer gate.ReleaseSession(sk)
		} else if checker.IsSessionActive(sk) {
			e.logGoalSessionBusy(goal, sk)
			return nil
		}
	}
	e.setGoalRunning(scope, true)
	defer e.setGoalRunning(scope, false)
	for {
		goal, err = e.store.GetGoal(scope)
		if err != nil {
			return err
		}
		if goal.Status != GoalStatusActive {
			return nil
		}
		now = e.nowTime()
		if !goal.DeadlineAt.IsZero() && now.After(goal.DeadlineAt) {
			e.markGoalTimeout(goal)
			return nil
		}
		select {
		case <-goalCtx.Done():
			logging.Infof("goal interrupted scope=%s:%s", goal.Scope.Kind, goal.Scope.ID)
			return nil
		default:
		}
		runCtx, runCancel := e.goalRunContext(goalCtx, goal)
		threadID := goal.ThreadID
		isFirstRun := threadID == ""
		prompt := e.buildGoalPrompt(goal, isFirstRun)
		logging.Infof("goal iteration start scope=%s:%s thread=%s first=%v", goal.Scope.Kind, goal.Scope.ID, threadID, isFirstRun)
		result, err := runner.Run(runCtx, llm.RunRequest{
			ThreadID:   threadID,
			AgentName:  "goal",
			UserText:   prompt,
			Scene:      goalScene(goal),
			Env:        e.buildGoalRunEnv(goal),
			OnProgress: e.goalProgressDispatcher(runCtx, goal),
		})
		runCancel(nil)
		nextThreadID := strings.TrimSpace(result.NextThreadID)
		if nextThreadID != "" && nextThreadID != threadID {
			if _, patchErr := e.store.PatchGoal(scope, func(g *GoalTask) error {
				g.ThreadID = nextThreadID
				return nil
			}); patchErr != nil {
				logging.Warnf("goal persist thread_id failed scope=%s:%s err=%v", goal.Scope.Kind, goal.Scope.ID, patchErr)
			}
		}
		if err != nil {
			if goalCtx.Err() != nil {
				logging.Infof("goal interrupted scope=%s:%s", goal.Scope.Kind, goal.Scope.ID)
				return nil
			}
			if runCtx.Err() != nil {
				logging.Infof("goal iteration timeout scope=%s:%s, continuing", goal.Scope.Kind, goal.Scope.ID)
				continue
			}
			logging.Errorf("goal run failed scope=%s:%s err=%v", goal.Scope.Kind, goal.Scope.ID, err)
			return err
		}
		logging.Infof("goal iteration done scope=%s:%s done=%v next_thread=%s", goal.Scope.Kind, goal.Scope.ID, result.GoalDone, strings.TrimSpace(result.NextThreadID))
		if result.GoalDone {
			e.markGoalComplete(goal)
			return nil
		}
	}
}

func (e *Engine) buildGoalPrompt(goal GoalTask, isFirstRun bool) string {
	now := e.nowTime()
	data := goalPromptData{
		Objective: goal.Objective,
		Now:       now.Format("2006-01-02 15:04:05"),
		Deadline:  goal.DeadlineAt.Format("2006-01-02 15:04:05"),
		Elapsed:   formatDurationHMS(now.Sub(goal.CreatedAt)),
		Remaining: formatDurationHMS(goal.DeadlineAt.Sub(now)),
	}
	if goal.DeadlineAt.IsZero() {
		data.Deadline = "未设置"
		data.Remaining = "未设置"
	}
	if isFirstRun {
		return renderGoalTemplate(goalStartTemplate, data)
	}
	return renderGoalTemplate(goalContinueTemplate, data)
}

func (e *Engine) markGoalComplete(goal GoalTask) {
	if _, err := e.store.PatchGoal(goal.Scope, func(g *GoalTask) error {
		g.Status = GoalStatusComplete
		return nil
	}); err != nil {
		logging.Errorf("goal mark complete failed scope=%s:%s err=%v", goal.Scope.Kind, goal.Scope.ID, err)
		return
	}
	logging.Infof("goal completed scope=%s:%s", goal.Scope.Kind, goal.Scope.ID)
	elapsed := e.nowTime().Sub(goal.CreatedAt)
	msg := "✅ 目标已完成\n   耗时: " + formatDurationHMS(elapsed)
	e.sendGoalNotification(goal, msg)
}

func (e *Engine) markGoalTimeout(goal GoalTask) {
	if _, err := e.store.PatchGoal(goal.Scope, func(g *GoalTask) error {
		g.Status = GoalStatusTimeout
		return nil
	}); err != nil {
		logging.Errorf("goal mark timeout failed scope=%s:%s err=%v", goal.Scope.Kind, goal.Scope.ID, err)
		return
	}
	logging.Infof("goal timed out scope=%s:%s", goal.Scope.Kind, goal.Scope.ID)
	elapsed := e.nowTime().Sub(goal.CreatedAt)
	msg := "⏰ 目标已超时\n   已用时间: " + formatDurationHMS(elapsed) +
		"\n   目标: " + goal.Objective
	e.sendGoalNotification(goal, msg)
}

func (e *Engine) sendGoalNotification(goal GoalTask, text string) {
	if e.sender == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if sender, ok := any(e.sender).(taskMessageSender); ok {
		sender.SendTextMessage(ctx, goal.Route.ReceiveIDType, goal.Route.ReceiveID, text)
		return
	}
	e.sender.SendText(ctx, goal.Route.ReceiveIDType, goal.Route.ReceiveID, text)
}

func (e *Engine) goalRunContext(ctx context.Context, goal GoalTask) (context.Context, context.CancelCauseFunc) {
	timeout := e.userTaskTimeoutDuration()
	if timeout <= 0 {
		timeout = defaultUserTaskTimeout
	}
	deadlineRemaining := time.Until(goal.DeadlineAt)
	if goal.DeadlineAt.IsZero() {
		deadlineRemaining = defaultGoalTimeout
	}
	if deadlineRemaining > 0 && deadlineRemaining < timeout {
		timeout = deadlineRemaining
	}
	timeoutCtx, timeoutCancel := context.WithTimeoutCause(ctx, timeout, errSessionInterrupted)
	runCtx, cancel := context.WithCancelCause(timeoutCtx)
	return runCtx, func(cause error) {
		timeoutCancel()
		cancel(cause)
	}
}

func (e *Engine) goalProgressDispatcher(ctx context.Context, goal GoalTask) llm.ProgressFunc {
	route := goal.Route
	return func(message string) {
		normalized := strings.TrimSpace(message)
		if normalized == "" || strings.HasPrefix(normalized, "[file_change] ") {
			return
		}
		if sender, ok := any(e.sender).(taskMessageSender); ok {
			sender.SendTextMessage(ctx, route.ReceiveIDType, route.ReceiveID, normalized)
			return
		}
		e.sender.SendText(ctx, route.ReceiveIDType, route.ReceiveID, normalized)
	}
}

func (e *Engine) buildGoalRunEnv(goal GoalTask) map[string]string {
	goal = NormalizeGoal(goal)
	receiveIDType := goal.Route.ReceiveIDType
	receiveID := goal.Route.ReceiveID
	if receiveIDType == "source_message_id" {
		switch goal.Scope.Kind {
		case ScopeKindChat:
			receiveIDType = "chat_id"
			receiveID = goal.Scope.ID
		case ScopeKindUser:
			receiveIDType = "open_id"
			receiveID = goal.Scope.ID
		}
	}
	ctx := sessionctx.SessionContext{
		ReceiveIDType: receiveIDType,
		ReceiveID:     receiveID,
		ActorUserID:   goal.Creator.UserID,
		ActorOpenID:   goal.Creator.OpenID,
		SessionKey:    goalSessionKey(goal),
	}
	switch goal.Scope.Kind {
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

func goalSessionKey(goal GoalTask) string {
	goal = NormalizeGoal(goal)
	if sk := goal.SessionKey; sk != "" {
		return sk
	}
	return fmt.Sprintf("%s:%s", goal.Route.ReceiveIDType, goal.Route.ReceiveID)
}

func goalScene(goal GoalTask) string {
	switch sessionKey := goal.SessionKey; {
	case strings.Contains(sessionKey, "|scene:work"):
		return "work"
	case strings.Contains(sessionKey, "|scene:chat"):
		return "chat"
	default:
		return "chat"
	}
}

func formatDurationHMS(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	seconds := int(d.Seconds()) % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (e *Engine) logGoalSessionBusy(goal GoalTask, sk string) {
	const skipLogInterval = time.Minute
	now := e.nowTime()
	key := "goal_" + goal.ID
	if last, ok := e.lastSkipLog.Load(key); !ok || now.Sub(last.(time.Time)) >= skipLogInterval {
		logging.Infof("goal skipped (session busy) scope=%s:%s session=%s", goal.Scope.Kind, goal.Scope.ID, sk)
		e.lastSkipLog.Store(key, now)
	}
}

func (e *Engine) setGoalRunning(scope Scope, running bool) {
	if e.store == nil {
		return
	}
	if _, err := e.store.PatchGoal(scope, func(g *GoalTask) error {
		g.Running = running
		return nil
	}); err != nil && !errors.Is(err, ErrGoalNotFound) {
		logging.Warnf("goal set running=%v failed scope=%s:%s err=%v", running, scope.Kind, scope.ID, err)
	}
}
