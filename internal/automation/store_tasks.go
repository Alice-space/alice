package automation

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Store) ListTasks(scope Scope, statusFilter string, limit int) ([]Task, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	scope = normalizeScope(scope)
	if scope.Kind == "" || scope.ID == "" {
		return nil, errors.New("scope is empty")
	}
	status, includeAll, err := ParseStatusFilter(statusFilter)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	var tasks []Task
	err = s.viewSnapshot(func(snapshot Snapshot) error {
		filtered := make([]Task, 0, len(snapshot.Tasks))
		for _, raw := range snapshot.Tasks {
			task := NormalizeTask(raw)
			if task.Scope != scope {
				continue
			}
			if includeAll {
				filtered = append(filtered, task)
				continue
			}
			if status != "" {
				if task.Status != status {
					continue
				}
				filtered = append(filtered, task)
				continue
			}
			if task.Status == TaskStatusDeleted {
				continue
			}
			filtered = append(filtered, task)
		}
		sort.Slice(filtered, func(i, j int) bool {
			left := filtered[i]
			right := filtered[j]
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.After(right.CreatedAt)
			}
			return left.ID > right.ID
		})
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		tasks = filtered
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *Store) GetTask(taskID string) (Task, error) {
	if s == nil {
		return Task{}, errors.New("store is nil")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, errors.New("task id is empty")
	}

	var task Task
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		idx := findTaskIndex(snapshot.Tasks, taskID)
		if idx < 0 {
			return ErrTaskNotFound
		}
		task = NormalizeTask(snapshot.Tasks[idx])
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) ResetRunningTasks() error {
	if s == nil {
		return errors.New("store is nil")
	}
	now := s.nowLocal()
	return s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		changed := false
		for idx := range snapshot.Tasks {
			task := NormalizeTask(snapshot.Tasks[idx])
			if !task.Running {
				continue
			}
			task.Running = false
			task.UpdatedAt = now
			task.Revision++
			snapshot.Tasks[idx] = task
			changed = true
		}
		return changed, nil
	})
}

func (s *Store) CreateTask(task Task) (Task, error) {
	if s == nil {
		return Task{}, errors.New("store is nil")
	}
	task = NormalizeTask(task)
	now := s.nowLocal()
	if task.ID == "" {
		task.ID = newTaskID(now)
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if task.NextRunAt.IsZero() {
		task.NextRunAt = NextRunAt(now, task.Schedule)
	}
	if err := ValidateTask(task); err != nil {
		return Task{}, err
	}

	var created Task
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		if findTaskIndex(snapshot.Tasks, task.ID) >= 0 {
			return false, fmt.Errorf("task id already exists: %s", task.ID)
		}
		snapshot.Tasks = append(snapshot.Tasks, task)
		created = task
		return true, nil
	})
	if err != nil {
		return Task{}, err
	}
	return created, nil
}

func (s *Store) PatchTask(taskID string, mutate func(task *Task) error) (Task, error) {
	if s == nil {
		return Task{}, errors.New("store is nil")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, errors.New("task id is empty")
	}
	if mutate == nil {
		return Task{}, errors.New("mutate callback is nil")
	}

	var updated Task
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		idx := findTaskIndex(snapshot.Tasks, taskID)
		if idx < 0 {
			return false, ErrTaskNotFound
		}
		task := NormalizeTask(snapshot.Tasks[idx])
		if err := mutate(&task); err != nil {
			return false, err
		}
		task = NormalizeTask(task)
		task.UpdatedAt = s.nowLocal()
		task.Revision++
		if task.NextRunAt.IsZero() && task.Status == TaskStatusActive {
			task.NextRunAt = NextRunAt(task.UpdatedAt, task.Schedule)
		}
		if err := ValidateTask(task); err != nil {
			return false, err
		}
		snapshot.Tasks[idx] = task
		updated = task
		return true, nil
	})
	if err != nil {
		return Task{}, err
	}
	return updated, nil
}

func (s *Store) ClaimDueTasks(at time.Time, limit int) ([]Task, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	if limit <= 0 {
		limit = 20
	}
	if at.IsZero() {
		at = s.nowLocal()
	}
	at = at.Local()

	claimed := make([]Task, 0, limit)
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		changed := false
		for idx := range snapshot.Tasks {
			if len(claimed) >= limit {
				break
			}
			task := NormalizeTask(snapshot.Tasks[idx])
			if task.Status != TaskStatusActive || task.Running {
				continue
			}
			if task.MaxRuns > 0 && task.RunCount >= task.MaxRuns {
				task.Status = TaskStatusPaused
				task.NextRunAt = time.Time{}
				task.UpdatedAt = at
				task.Revision++
				snapshot.Tasks[idx] = task
				changed = true
				continue
			}
			switch task.Schedule.Type {
			case ScheduleTypeInterval:
				if task.Schedule.EverySeconds <= 0 {
					continue
				}
			case ScheduleTypeCron:
				if strings.TrimSpace(task.Schedule.CronExpr) == "" {
					continue
				}
			default:
				continue
			}
			next := task.NextRunAt.Local()
			if !next.IsZero() && next.After(at) {
				continue
			}
			task.LastRunAt = at
			task.Running = true
			task.RunCount++
			if task.MaxRuns > 0 && task.RunCount >= task.MaxRuns {
				task.Status = TaskStatusPaused
				task.NextRunAt = time.Time{}
			} else {
				task.NextRunAt = NextRunAt(at, task.Schedule)
			}
			task.UpdatedAt = at
			task.Revision++
			snapshot.Tasks[idx] = task
			claimed = append(claimed, task)
			changed = true
		}
		return changed, nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) RecordTaskResult(taskID string, at time.Time, runErr error) error {
	if s == nil {
		return errors.New("store is nil")
	}
	_, err := s.PatchTask(taskID, func(task *Task) error {
		if at.IsZero() {
			at = s.nowLocal()
		}
		at = at.Local()
		task.Running = false
		if runErr != nil {
			task.ConsecutiveFailures++
			task.LastResult = "error: " + strings.TrimSpace(runErr.Error())
		} else {
			task.ConsecutiveFailures = 0
			task.LastResult = "ok: " + at.Format(time.RFC3339)
		}
		return nil
	})
	if errors.Is(err, ErrTaskNotFound) {
		return nil
	}
	return err
}

func (s *Store) RecordTaskSignal(taskID string, at time.Time, kind, message string, pause bool) error {
	if s == nil {
		return errors.New("store is nil")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "signal"
	}
	_, err := s.PatchTask(taskID, func(task *Task) error {
		if at.IsZero() {
			at = s.nowLocal()
		}
		at = at.Local()
		task.Running = false
		task.ConsecutiveFailures = 0
		message = strings.TrimSpace(message)
		if message == "" {
			task.LastResult = kind + ": " + at.Format(time.RFC3339)
		} else {
			task.LastResult = kind + ": " + message
		}
		if pause {
			task.Status = TaskStatusPaused
			task.NextRunAt = time.Time{}
		}
		return nil
	})
	if errors.Is(err, ErrTaskNotFound) {
		return nil
	}
	return err
}
