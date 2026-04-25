package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/storeutil"
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

type ScheduleType string

const (
	ScheduleTypeInterval ScheduleType = "interval"
	ScheduleTypeCron     ScheduleType = "cron"
)

type ActionType string

const (
	ActionTypeRunLLM ActionType = "run_llm"
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
	Type         ScheduleType `json:"type"`
	EverySeconds int          `json:"every_seconds"`
	CronExpr     string       `json:"cron_expr,omitempty"`
}

type Action struct {
	Type            ActionType `json:"type"`
	Text            string     `json:"text"`
	Prompt          string     `json:"prompt,omitempty"`
	Provider        string     `json:"provider,omitempty"`
	Model           string     `json:"model,omitempty"`
	Profile         string     `json:"profile,omitempty"`
	StateKey        string     `json:"state_key,omitempty"`
	SessionKey      string     `json:"session_key,omitempty"`
	ResumeThreadID  string     `json:"resume_thread_id,omitempty"`
	SourceMessageID string     `json:"source_message_id,omitempty"`
	ReasoningEffort string     `json:"reasoning_effort,omitempty"`
	Personality     string     `json:"personality,omitempty"`
	PromptPrefix    string     `json:"prompt_prefix,omitempty"`
	MentionUserIDs  []string   `json:"mention_user_ids,omitempty"`
}

type Task struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title,omitempty"`
	Scope               Scope      `json:"scope"`
	Route               Route      `json:"route"`
	Creator             Actor      `json:"creator"`
	ManageMode          ManageMode `json:"manage_mode"`
	Schedule            Schedule   `json:"schedule"`
	Action              Action     `json:"action"`
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
	LastSignalKind      string     `json:"last_signal_kind,omitempty"`
	LastSignalMessage   string     `json:"last_signal_message,omitempty"`
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
	task.Scope.Kind = ScopeKind(strings.ToLower(strings.TrimSpace(string(task.Scope.Kind))))
	task.Scope.ID = strings.TrimSpace(task.Scope.ID)
	task.Route.ReceiveIDType = strings.TrimSpace(task.Route.ReceiveIDType)
	task.Route.ReceiveID = strings.TrimSpace(task.Route.ReceiveID)
	task.Creator.UserID = strings.TrimSpace(task.Creator.UserID)
	task.Creator.OpenID = strings.TrimSpace(task.Creator.OpenID)
	task.Creator.Name = strings.TrimSpace(task.Creator.Name)
	task.ManageMode = ManageMode(strings.ToLower(strings.TrimSpace(string(task.ManageMode))))
	task.Schedule.Type = ScheduleType(strings.ToLower(strings.TrimSpace(string(task.Schedule.Type))))
	task.Schedule.CronExpr = strings.TrimSpace(task.Schedule.CronExpr)
	task.Action.Type = ActionType(strings.ToLower(strings.TrimSpace(string(task.Action.Type))))
	task.Action.Text = strings.TrimSpace(task.Action.Text)
	task.Action.Prompt = strings.TrimSpace(task.Action.Prompt)
	task.Action.Provider = strings.ToLower(strings.TrimSpace(task.Action.Provider))
	task.Action.Model = strings.TrimSpace(task.Action.Model)
	task.Action.Profile = strings.TrimSpace(task.Action.Profile)
	task.Action.StateKey = strings.TrimSpace(task.Action.StateKey)
	task.Action.SessionKey = strings.TrimSpace(task.Action.SessionKey)
	task.Action.ResumeThreadID = strings.TrimSpace(task.Action.ResumeThreadID)
	task.Action.SourceMessageID = strings.TrimSpace(task.Action.SourceMessageID)
	task.Action.ReasoningEffort = strings.ToLower(strings.TrimSpace(task.Action.ReasoningEffort))
	task.Action.Personality = strings.ToLower(strings.TrimSpace(task.Action.Personality))
	task.Action.MentionUserIDs = storeutil.UniqueNonEmptyStrings(task.Action.MentionUserIDs)
	task.Status = TaskStatus(strings.ToLower(strings.TrimSpace(string(task.Status))))
	task.LastResult = strings.TrimSpace(task.LastResult)
	task.LastSignalKind = strings.ToLower(strings.TrimSpace(task.LastSignalKind))
	task.LastSignalMessage = strings.TrimSpace(task.LastSignalMessage)

	if task.ManageMode == "" {
		task.ManageMode = ManageModeCreatorOnly
	}
	if task.Schedule.Type == "" {
		task.Schedule.Type = ScheduleTypeInterval
	}
	if task.Status == "" {
		task.Status = TaskStatusActive
	}
	if task.Status != TaskStatusDeleted {
		task.DeletedAt = time.Time{}
	}
	return task
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
	switch task.Schedule.Type {
	case ScheduleTypeInterval:
		if task.Schedule.EverySeconds < 60 {
			return errors.New("every_seconds must be >= 60 for interval schedule")
		}
	case ScheduleTypeCron:
		if strings.TrimSpace(task.Schedule.CronExpr) == "" {
			return errors.New("cron_expr is empty")
		}
		if err := validateCronExpression(task.Schedule.CronExpr); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid schedule type %q", task.Schedule.Type)
	}
	switch task.Action.Type {
	case ActionTypeRunLLM:
		if strings.TrimSpace(task.Action.Prompt) == "" {
			return errors.New("action prompt is empty for run_llm")
		}
		if _, err := buildMentionParts(task.Action.MentionUserIDs); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid action type %q", task.Action.Type)
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
	normalized := NormalizeTask(Task{Schedule: schedule}).Schedule
	if from.IsZero() {
		from = time.Now()
	}
	from = from.Local()
	switch normalized.Type {
	case ScheduleTypeInterval:
		if normalized.EverySeconds <= 0 {
			return from
		}
		return from.Add(time.Duration(normalized.EverySeconds) * time.Second)
	case ScheduleTypeCron:
		next, err := nextCronRunAt(from, normalized.CronExpr)
		if err != nil {
			return from
		}
		return next
	default:
		return from
	}
}

func BuildDispatchText(action Action) (string, error) {
	action = NormalizeTask(Task{Action: action}).Action
	mentionParts, err := buildMentionParts(action.MentionUserIDs)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(action.Text)
	if len(mentionParts) == 0 && text == "" {
		return "", errors.New("action text and mention_user_ids are both empty")
	}
	if len(mentionParts) == 0 {
		return text, nil
	}
	prefix := strings.Join(mentionParts, " ")
	if text == "" {
		return prefix, nil
	}
	return prefix + " " + text, nil
}

func buildMentionParts(mentionUserIDs []string) ([]string, error) {
	if len(mentionUserIDs) == 0 {
		return nil, nil
	}
	mentionParts := make([]string, 0, len(mentionUserIDs))
	for _, userID := range mentionUserIDs {
		normalized := strings.TrimSpace(userID)
		if normalized == "" {
			continue
		}
		if strings.ContainsRune(normalized, '"') {
			return nil, fmt.Errorf("invalid mention user id %q", normalized)
		}
		mentionParts = append(mentionParts, `<at user_id="`+normalized+`">`+normalized+`</at>`)
	}
	return mentionParts, nil
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
