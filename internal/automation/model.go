package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrGoalIDEmpty     = errors.New("goal id is empty")
	ErrObjectiveEmpty  = errors.New("objective is empty")
	ErrScopeIDEmpty    = errors.New("scope id is empty")
	ErrRouteIncomplete = errors.New("route is incomplete")
	ErrCreatorIDEmpty  = errors.New("creator id is empty")

	ErrScopeEmpty = errors.New("scope is empty")

	ErrTaskIDEmpty        = errors.New("task id is empty")
	ErrPromptEmpty        = errors.New("prompt is empty")
	ErrScheduleRequired   = errors.New("schedule requires every_seconds or cron_expr")
	ErrMaxRunsNegative    = errors.New("max_runs must be >= 0")
	ErrRunCountNegative   = errors.New("run_count must be >= 0")
	ErrRunCountExceedsMax = errors.New("run_count exceeds max_runs")
	ErrMaxRunsReached     = errors.New("active task already reached max_runs")
	ErrEverySecondsMin    = errors.New("every_seconds must be >= 60")
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

type GoalStatus string

const (
	GoalStatusActive            GoalStatus = "active"
	GoalStatusPaused            GoalStatus = "paused"
	GoalStatusComplete          GoalStatus = "complete"
	GoalStatusTimeout           GoalStatus = "timeout"
	GoalStatusWaitingForSession GoalStatus = "waiting_for_session"
)

func (s GoalStatus) IsTerminal() bool {
	return s == GoalStatusComplete || s == GoalStatusTimeout
}

type GoalTask struct {
	ID         string     `json:"id"`
	Objective  string     `json:"objective"`
	Status     GoalStatus `json:"status"`
	DeadlineAt time.Time  `json:"deadline_at"`
	ThreadID   string     `json:"thread_id,omitempty"`

	Scope      Scope  `json:"scope"`
	Route      Route  `json:"route"`
	Creator    Actor  `json:"creator"`
	SessionKey string `json:"session_key,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Revision  int64     `json:"revision"`
	Running   bool      `json:"running,omitempty"`
}

func NormalizeGoal(goal GoalTask) GoalTask {
	goal.ID = strings.TrimSpace(goal.ID)
	goal.Objective = strings.TrimSpace(goal.Objective)
	goal.Status = GoalStatus(strings.ToLower(strings.TrimSpace(string(goal.Status))))
	goal.ThreadID = strings.TrimSpace(goal.ThreadID)
	goal.Scope.Kind = ScopeKind(strings.ToLower(strings.TrimSpace(string(goal.Scope.Kind))))
	goal.Scope.ID = strings.TrimSpace(goal.Scope.ID)
	goal.Route.ReceiveIDType = strings.TrimSpace(goal.Route.ReceiveIDType)
	goal.Route.ReceiveID = strings.TrimSpace(goal.Route.ReceiveID)
	goal.Creator.UserID = strings.TrimSpace(goal.Creator.UserID)
	goal.Creator.OpenID = strings.TrimSpace(goal.Creator.OpenID)
	goal.Creator.Name = strings.TrimSpace(goal.Creator.Name)
	goal.SessionKey = strings.TrimSpace(goal.SessionKey)
	if goal.Status == "" {
		goal.Status = GoalStatusActive
	}
	return goal
}

func ValidateGoal(goal GoalTask) error {
	goal = NormalizeGoal(goal)
	if goal.ID == "" {
		return ErrGoalIDEmpty
	}
	if goal.Objective == "" {
		return ErrObjectiveEmpty
	}
	if goal.Scope.Kind != ScopeKindUser && goal.Scope.Kind != ScopeKindChat {
		return fmt.Errorf("invalid scope kind %q", goal.Scope.Kind)
	}
	if goal.Scope.ID == "" {
		return ErrScopeIDEmpty
	}
	if goal.Route.ReceiveIDType == "" || goal.Route.ReceiveID == "" {
		return ErrRouteIncomplete
	}
	if goal.Creator.PreferredID() == "" {
		return ErrCreatorIDEmpty
	}
	switch goal.Status {
	case GoalStatusActive, GoalStatusPaused, GoalStatusComplete, GoalStatusTimeout, GoalStatusWaitingForSession:
	default:
		return fmt.Errorf("invalid goal status %q", goal.Status)
	}
	return nil
}

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
		return ErrTaskIDEmpty
	}
	if task.Scope.Kind != ScopeKindUser && task.Scope.Kind != ScopeKindChat {
		return fmt.Errorf("invalid scope kind %q", task.Scope.Kind)
	}
	if task.Scope.ID == "" {
		return ErrScopeIDEmpty
	}
	if task.Route.ReceiveIDType == "" || task.Route.ReceiveID == "" {
		return ErrRouteIncomplete
	}
	if task.Creator.PreferredID() == "" {
		return ErrCreatorIDEmpty
	}
	if task.ManageMode != ManageModeCreatorOnly && task.ManageMode != ManageModeScopeAll {
		return fmt.Errorf("invalid manage mode %q", task.ManageMode)
	}
	task.Prompt = strings.TrimSpace(task.Prompt)
	if task.Prompt == "" {
		return ErrPromptEmpty
	}
	if task.Schedule.isInterval() {
		if task.Schedule.EverySeconds < 60 {
			return ErrEverySecondsMin
		}
	} else if task.Schedule.isCron() {
		if err := validateCronExpression(task.Schedule.CronExpr); err != nil {
			return err
		}
	} else {
		return ErrScheduleRequired
	}
	if task.Status != TaskStatusActive && task.Status != TaskStatusPaused && task.Status != TaskStatusDeleted {
		return fmt.Errorf("invalid status %q", task.Status)
	}
	if task.MaxRuns < 0 {
		return ErrMaxRunsNegative
	}
	if task.RunCount < 0 {
		return ErrRunCountNegative
	}
	if task.MaxRuns > 0 && task.RunCount > task.MaxRuns {
		return ErrRunCountExceedsMax
	}
	if task.Status == TaskStatusActive && task.MaxRuns > 0 && task.RunCount >= task.MaxRuns && !task.Running {
		return ErrMaxRunsReached
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
