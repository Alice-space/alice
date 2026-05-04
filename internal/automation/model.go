package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ScopeKind string

const (
	ScopeKindUser ScopeKind = "user"
	ScopeKindChat ScopeKind = "chat"
)

type ManageMode string

const (
	ManageModeCreatorOnly ManageMode = "creator_only"
	ManageModeScopeAll    ManageMode = "scope_all"
)

type TaskStatus string

const (
	TaskStatusActive  TaskStatus = "active"
	TaskStatusPaused  TaskStatus = "paused"
	TaskStatusDeleted TaskStatus = "deleted"
)

type Scope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id"`
}

type Route struct {
	ReceiveIDType string `json:"receive_id_type"`
	ReceiveID     string `json:"receive_id"`
}

type Actor struct {
	UserID string `json:"user_id,omitempty"`
	OpenID string `json:"open_id,omitempty"`
	Name   string `json:"name,omitempty"`
}

func (a Actor) PreferredID() string {
	if id := strings.TrimSpace(a.UserID); id != "" {
		return id
	}
	return strings.TrimSpace(a.OpenID)
}

type Schedule struct {
	EverySeconds int    `json:"every_seconds,omitempty"`
	CronExpr     string `json:"cron_expr,omitempty"`
}

func (s Schedule) isCron() bool {
	return strings.TrimSpace(s.CronExpr) != ""
}

func (s Schedule) isInterval() bool {
	return s.EverySeconds > 0
}

type Task struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Prompt   string   `json:"prompt"`
	Fresh    bool     `json:"fresh,omitempty"`
	Schedule Schedule `json:"schedule"`

	Scope           Scope      `json:"scope"`
	Route           Route      `json:"route"`
	Creator         Actor      `json:"creator"`
	ManageMode      ManageMode `json:"manage_mode"`
	SessionKey      string     `json:"session_key,omitempty"`
	ResumeThreadID  string     `json:"resume_thread_id,omitempty"`
	SourceMessageID string     `json:"source_message_id,omitempty"`

	Status              TaskStatus `json:"status"`
	MaxRuns             int        `json:"max_runs,omitempty"`
	RunCount            int        `json:"run_count,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	NextRunAt           time.Time  `json:"next_run_at"`
	LastRunAt           time.Time  `json:"last_run_at,omitempty"`
	DeletedAt           time.Time  `json:"deleted_at,omitempty"`
	Running             bool       `json:"running,omitempty"`
	LastResult          string     `json:"last_result,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures,omitempty"`
	Revision            int64      `json:"revision"`
}

type Snapshot struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

func NormalizeTask(task Task) Task {
	task.ID = strings.TrimSpace(task.ID)
	task.Title = strings.TrimSpace(task.Title)
	task.Prompt = strings.TrimSpace(task.Prompt)
	task.Schedule.EverySeconds = scheduleEverySeconds(task.Schedule.EverySeconds)
	task.Schedule.CronExpr = strings.TrimSpace(task.Schedule.CronExpr)
	task.Scope.Kind = ScopeKind(strings.ToLower(strings.TrimSpace(string(task.Scope.Kind))))
	task.Scope.ID = strings.TrimSpace(task.Scope.ID)
	task.Route.ReceiveIDType = strings.TrimSpace(task.Route.ReceiveIDType)
	task.Route.ReceiveID = strings.TrimSpace(task.Route.ReceiveID)
	task.Creator.UserID = strings.TrimSpace(task.Creator.UserID)
	task.Creator.OpenID = strings.TrimSpace(task.Creator.OpenID)
	task.Creator.Name = strings.TrimSpace(task.Creator.Name)
	task.ManageMode = ManageMode(strings.ToLower(strings.TrimSpace(string(task.ManageMode))))
	task.SessionKey = strings.TrimSpace(task.SessionKey)
	task.ResumeThreadID = strings.TrimSpace(task.ResumeThreadID)
	task.SourceMessageID = strings.TrimSpace(task.SourceMessageID)
	task.Status = TaskStatus(strings.ToLower(strings.TrimSpace(string(task.Status))))
	task.LastResult = strings.TrimSpace(task.LastResult)

	if task.ManageMode == "" {
		task.ManageMode = ManageModeCreatorOnly
	}
	if task.Status == "" {
		task.Status = TaskStatusActive
	}
	if task.Status != TaskStatusDeleted {
		task.DeletedAt = time.Time{}
	}
	return task
}

func scheduleEverySeconds(raw int) int {
	if raw < 60 {
		return 0
	}
	return raw
}

func ValidateTask(task Task) error {
	task = NormalizeTask(task)
	if task.ID == "" {
		return errors.New("task id is empty")
	}
	if task.Scope.Kind != ScopeKindUser && task.Scope.Kind != ScopeKindChat {
		return fmt.Errorf("invalid scope kind %q", task.Scope.Kind)
	}
	if task.Scope.ID == "" {
		return errors.New("scope id is empty")
	}
	if task.Route.ReceiveIDType == "" || task.Route.ReceiveID == "" {
		return errors.New("route is incomplete")
	}
	if task.Creator.PreferredID() == "" {
		return errors.New("creator id is empty")
	}
	if task.ManageMode != ManageModeCreatorOnly && task.ManageMode != ManageModeScopeAll {
		return fmt.Errorf("invalid manage mode %q", task.ManageMode)
	}
	task.Prompt = strings.TrimSpace(task.Prompt)
	if task.Prompt == "" {
		return errors.New("prompt is empty")
	}
	if task.Schedule.isInterval() {
		if task.Schedule.EverySeconds < 60 {
			return errors.New("every_seconds must be >= 60")
		}
	} else if task.Schedule.isCron() {
		if err := validateCronExpression(task.Schedule.CronExpr); err != nil {
			return err
		}
	} else {
		return errors.New("schedule requires every_seconds or cron_expr")
	}
	if task.Status != TaskStatusActive && task.Status != TaskStatusPaused && task.Status != TaskStatusDeleted {
		return fmt.Errorf("invalid status %q", task.Status)
	}
	if task.MaxRuns < 0 {
		return errors.New("max_runs must be >= 0")
	}
	if task.RunCount < 0 {
		return errors.New("run_count must be >= 0")
	}
	if task.MaxRuns > 0 && task.RunCount > task.MaxRuns {
		return errors.New("run_count exceeds max_runs")
	}
	if task.Status == TaskStatusActive && task.MaxRuns > 0 && task.RunCount >= task.MaxRuns && !task.Running {
		return errors.New("active task already reached max_runs")
	}
	return nil
}

func NextRunAt(from time.Time, schedule Schedule) time.Time {
	if from.IsZero() {
		from = time.Now()
	}
	from = from.Local()
	if schedule.isCron() {
		next, err := nextCronRunAt(from, schedule.CronExpr)
		if err != nil {
			return from
		}
		return next
	}
	if schedule.EverySeconds > 0 {
		return from.Add(time.Duration(schedule.EverySeconds) * time.Second)
	}
	return from
}

func ParseStatusFilter(raw string) (TaskStatus, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", false, nil
	}
	if normalized == "all" {
		return "", true, nil
	}
	status := TaskStatus(normalized)
	switch status {
	case TaskStatusActive, TaskStatusPaused, TaskStatusDeleted:
		return status, false, nil
	default:
		return "", false, fmt.Errorf("invalid status filter %q", raw)
	}
}
