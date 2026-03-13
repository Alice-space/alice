package automation

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	defaultSnapshotVersion = 1
	defaultListLimit       = 20
	maxListLimit           = 200
)

var ErrTaskNotFound = errors.New("automation task not found")

type Store struct {
	path string
	now  func() time.Time
}

func NewStore(path string) *Store {
	return &Store{
		path: strings.TrimSpace(path),
		now:  time.Now,
	}
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

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
	err = s.withLockedSnapshot(func(snapshot *Snapshot) (bool, error) {
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
		return false, nil
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
	err := s.withLockedSnapshot(func(snapshot *Snapshot) (bool, error) {
		idx := findTaskIndex(snapshot.Tasks, taskID)
		if idx < 0 {
			return false, ErrTaskNotFound
		}
		task = NormalizeTask(snapshot.Tasks[idx])
		return false, nil
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
	now := s.nowUTC()
	return s.withLockedSnapshot(func(snapshot *Snapshot) (bool, error) {
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
	now := s.nowUTC()
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

	created := Task{}
	err := s.withLockedSnapshot(func(snapshot *Snapshot) (bool, error) {
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

	updated := Task{}
	err := s.withLockedSnapshot(func(snapshot *Snapshot) (bool, error) {
		idx := findTaskIndex(snapshot.Tasks, taskID)
		if idx < 0 {
			return false, ErrTaskNotFound
		}
		task := NormalizeTask(snapshot.Tasks[idx])
		if err := mutate(&task); err != nil {
			return false, err
		}
		task = NormalizeTask(task)
		task.UpdatedAt = s.nowUTC()
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
		at = s.nowUTC()
	}
	at = at.UTC()

	claimed := make([]Task, 0, limit)
	err := s.withLockedSnapshot(func(snapshot *Snapshot) (bool, error) {
		changed := false
		for idx := range snapshot.Tasks {
			if len(claimed) >= limit {
				break
			}
			task := NormalizeTask(snapshot.Tasks[idx])
			if task.Status != TaskStatusActive {
				continue
			}
			if task.Running {
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
			next := task.NextRunAt.UTC()
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
			at = s.nowUTC()
		}
		at = at.UTC()
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

func (s *Store) withLockedSnapshot(fn func(snapshot *Snapshot) (changed bool, err error)) error {
	if fn == nil {
		return errors.New("snapshot callback is nil")
	}
	if strings.TrimSpace(s.path) == "" {
		return errors.New("store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create automation dir failed: %w", err)
	}

	lockFile, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open automation lock file failed: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock automation state failed: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	}()

	snapshot, err := s.readSnapshotFile()
	if err != nil {
		return err
	}
	if snapshot.Version <= 0 {
		snapshot.Version = defaultSnapshotVersion
	}

	changed, err := fn(&snapshot)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.writeSnapshotFile(snapshot)
}

func (s *Store) readSnapshotFile() (Snapshot, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{Version: defaultSnapshotVersion}, nil
		}
		return Snapshot{}, fmt.Errorf("read automation state failed: %w", err)
	}
	if len(data) == 0 {
		return Snapshot{Version: defaultSnapshotVersion}, nil
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("parse automation state failed: %w", err)
	}
	normalized := make([]Task, 0, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		task = NormalizeTask(task)
		if task.ID == "" {
			continue
		}
		normalized = append(normalized, task)
	}
	snapshot.Tasks = normalized
	if snapshot.Version <= 0 {
		snapshot.Version = defaultSnapshotVersion
	}
	return snapshot, nil
}

func (s *Store) writeSnapshotFile(snapshot Snapshot) error {
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal automation state failed: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(s.path), ".automation_state.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp automation state failed: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp automation state failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp automation state failed: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace automation state failed: %w", err)
	}
	return nil
}

func (s *Store) nowUTC() time.Time {
	now := time.Now().UTC()
	if s != nil && s.now != nil {
		now = s.now().UTC()
	}
	return now
}

func normalizeScope(scope Scope) Scope {
	scope.Kind = ScopeKind(strings.ToLower(strings.TrimSpace(string(scope.Kind))))
	scope.ID = strings.TrimSpace(scope.ID)
	return scope
}

func findTaskIndex(tasks []Task, taskID string) int {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return -1
	}
	for idx := range tasks {
		if strings.TrimSpace(tasks[idx].ID) == taskID {
			return idx
		}
	}
	return -1
}

func newTaskID(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id, err := ulid.New(ulid.Timestamp(now), ulid.Monotonic(rand.Reader, 0))
	if err != nil {
		return fmt.Sprintf("task_%d", now.UnixNano())
	}
	return "task_" + strings.ToLower(id.String())
}
