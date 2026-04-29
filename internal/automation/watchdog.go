package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/logging"
)

const (
	defaultWatchdogGrace         = 2 * time.Minute
	defaultWatchdogAlertCooldown = 30 * time.Minute
)

type TaskWatchdogAlertKind string

const (
	TaskWatchdogAlertOverdue TaskWatchdogAlertKind = "overdue"
	TaskWatchdogAlertStuck   TaskWatchdogAlertKind = "stuck"
)

type TaskWatchdogAlert struct {
	Kind       TaskWatchdogAlertKind
	Task       Task
	OverdueBy  time.Duration
	RunningFor time.Duration
}

func (s *Store) ScanWatchdogAlerts(at time.Time, overdueGrace, runningGrace time.Duration) ([]TaskWatchdogAlert, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	if at.IsZero() {
		at = s.nowLocal()
	}
	at = at.Local()
	if overdueGrace <= 0 {
		overdueGrace = defaultWatchdogGrace
	}
	if runningGrace <= 0 {
		runningGrace = defaultUserTaskTimeout + defaultWatchdogGrace
	}

	alerts := make([]TaskWatchdogAlert, 0)
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		for _, raw := range snapshot.Tasks {
			task := NormalizeTask(raw)
			if task.Status != TaskStatusActive {
				continue
			}
			if task.Running {
				startedAt := task.LastRunAt
				if startedAt.IsZero() {
					startedAt = task.UpdatedAt
				}
				if startedAt.IsZero() {
					continue
				}
				runningFor := at.Sub(startedAt.Local())
				if runningFor >= runningGrace {
					alerts = append(alerts, TaskWatchdogAlert{
						Kind:       TaskWatchdogAlertStuck,
						Task:       task,
						RunningFor: runningFor,
					})
				}
				continue
			}

			dueAt := task.NextRunAt
			if dueAt.IsZero() {
				dueAt = task.UpdatedAt
			}
			if dueAt.IsZero() {
				dueAt = task.CreatedAt
			}
			if dueAt.IsZero() {
				continue
			}
			overdueBy := at.Sub(dueAt.Local())
			if overdueBy >= overdueGrace {
				alerts = append(alerts, TaskWatchdogAlert{
					Kind:      TaskWatchdogAlertOverdue,
					Task:      task,
					OverdueBy: overdueBy,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return alerts, nil
}

func (e *Engine) RunWatchdogOnce(ctx context.Context) {
	if e == nil || e.store == nil || e.sender == nil {
		return
	}
	now := e.nowTime()
	alerts, err := e.store.ScanWatchdogAlerts(
		now,
		defaultWatchdogGrace,
		e.userTaskTimeoutDuration()+defaultWatchdogGrace,
	)
	if err != nil {
		logging.Errorf("scan automation watchdog alerts failed: %v", err)
		return
	}
	for _, alert := range alerts {
		if !e.canSendWatchdogAlert(alert, now) {
			continue
		}
		if err := e.sendWatchdogAlert(ctx, alert); err != nil {
			logging.Warnf("send automation watchdog alert failed id=%s kind=%s err=%v", alert.Task.ID, alert.Kind, err)
			continue
		}
		e.markWatchdogAlertSent(alert, now)
	}
}

func (e *Engine) canSendWatchdogAlert(alert TaskWatchdogAlert, now time.Time) bool {
	key := alert.Task.ID + ":" + string(alert.Kind)
	e.watchdogMu.Lock()
	defer e.watchdogMu.Unlock()
	if last, ok := e.watchdogLastAlert[key]; ok && now.Sub(last) < defaultWatchdogAlertCooldown {
		return false
	}
	return true
}

func (e *Engine) markWatchdogAlertSent(alert TaskWatchdogAlert, now time.Time) {
	key := alert.Task.ID + ":" + string(alert.Kind)
	e.watchdogMu.Lock()
	defer e.watchdogMu.Unlock()
	if e.watchdogLastAlert == nil {
		e.watchdogLastAlert = make(map[string]time.Time)
	}
	e.watchdogLastAlert[key] = now
}

func (e *Engine) sendWatchdogAlert(ctx context.Context, alert TaskWatchdogAlert) error {
	task := NormalizeTask(alert.Task)
	cardContent, err := buildWatchdogAlertCardContent(alert)
	if err != nil {
		return err
	}
	route := effectiveRoute(task)
	messageID, err := e.sendCardWithFallback(ctx, task, route, cardContent)
	if err != nil {
		text := watchdogAlertText(alert)
		messageID, err = e.sendTextWithFallback(ctx, task, route, text)
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(messageID) != "" {
		logging.Warnf("automation watchdog alert sent id=%s kind=%s message_id=%s", task.ID, alert.Kind, messageID)
	}
	return nil
}

func buildWatchdogAlertCardContent(alert TaskWatchdogAlert) (string, error) {
	task := NormalizeTask(alert.Task)
	title := "自动任务提醒"
	if task.Title != "" {
		title = task.Title
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
				"content": title,
			},
			"template": "orange",
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":     "markdown",
					"content": watchdogAlertText(alert),
				},
			},
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal watchdog alert card failed: %w", err)
	}
	return string(raw), nil
}

func watchdogAlertText(alert TaskWatchdogAlert) string {
	task := NormalizeTask(alert.Task)
	taskName := task.Title
	if taskName == "" {
		taskName = task.ID
	}
	switch alert.Kind {
	case TaskWatchdogAlertStuck:
		return fmt.Sprintf("**定时任务可能卡住**\n任务：%s\nID：%s\n已运行：%s\n上次开始：%s",
			taskName,
			task.ID,
			formatWatchdogDuration(alert.RunningFor),
			formatWatchdogTime(task.LastRunAt),
		)
	default:
		return fmt.Sprintf("**定时任务可能没有按时触发**\n任务：%s\nID：%s\n已过期：%s\n计划时间：%s",
			taskName,
			task.ID,
			formatWatchdogDuration(alert.OverdueBy),
			formatWatchdogTime(task.NextRunAt),
		)
	}
}

func formatWatchdogDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
}

func formatWatchdogTime(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	return t.Local().Format(time.RFC3339)
}
