package automation

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	bolt "go.etcd.io/bbolt"
)

const (
	defaultSnapshotVersion = 1
	defaultListLimit       = 20
	maxListLimit           = 200
)

var (
	ErrTaskNotFound = errors.New("automation task not found")

	automationMetaBucket  = []byte("meta")
	automationTasksBucket = []byte("tasks")
	snapshotVersionKey    = []byte("version")
)

type Store struct {
	path string
	now  func() time.Time

	openOnce sync.Once
	db       *bolt.DB
	openErr  error
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
	now := s.nowUTC()
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
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
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

func (s *Store) dbOrOpen() (*bolt.DB, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	s.openOnce.Do(func() {
		s.openErr = s.openDB()
	})
	if s.openErr != nil {
		return nil, s.openErr
	}
	return s.db, nil
}

func (s *Store) openDB() error {
	if strings.TrimSpace(s.path) == "" {
		return errors.New("store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create automation dir failed: %w", err)
	}

	db, err := bolt.Open(s.path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return fmt.Errorf("open automation db failed: %w", err)
	}

	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(automationMetaBucket); err != nil {
			return fmt.Errorf("create automation meta bucket failed: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(automationTasksBucket); err != nil {
			return fmt.Errorf("create automation tasks bucket failed: %w", err)
		}
		if err := writeSnapshotVersion(tx, defaultSnapshotVersion); err != nil {
			return err
		}
		if err := s.importLegacySnapshotIfNeeded(tx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	return nil
}

func (s *Store) importLegacySnapshotIfNeeded(tx *bolt.Tx) error {
	tasksBucket := tx.Bucket(automationTasksBucket)
	if tasksBucket == nil {
		return errors.New("automation tasks bucket is missing")
	}
	cursor := tasksBucket.Cursor()
	if key, _ := cursor.First(); key != nil {
		return nil
	}

	legacyPath := s.legacySnapshotPath()
	if legacyPath == "" {
		return nil
	}
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read legacy automation state failed: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("parse legacy automation state failed: %w", err)
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
	return writeSnapshotTx(tx, snapshot)
}

func (s *Store) legacySnapshotPath() string {
	if s == nil {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(s.path)), ".db") {
		return ""
	}
	return filepath.Join(filepath.Dir(s.path), "automation_state.json")
}

func (s *Store) viewSnapshot(fn func(snapshot Snapshot) error) error {
	if fn == nil {
		return errors.New("snapshot callback is nil")
	}
	db, err := s.dbOrOpen()
	if err != nil {
		return err
	}
	return db.View(func(tx *bolt.Tx) error {
		snapshot, err := readSnapshotTx(tx)
		if err != nil {
			return err
		}
		return fn(snapshot)
	})
}

func (s *Store) updateSnapshot(fn func(snapshot *Snapshot) (changed bool, err error)) error {
	if fn == nil {
		return errors.New("snapshot callback is nil")
	}
	db, err := s.dbOrOpen()
	if err != nil {
		return err
	}
	return db.Update(func(tx *bolt.Tx) error {
		snapshot, err := readSnapshotTx(tx)
		if err != nil {
			return err
		}
		changed, err := fn(&snapshot)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return writeSnapshotTx(tx, snapshot)
	})
}

func readSnapshotTx(tx *bolt.Tx) (Snapshot, error) {
	if tx == nil {
		return Snapshot{}, errors.New("automation transaction is nil")
	}
	snapshot := Snapshot{Version: defaultSnapshotVersion}
	if version, err := readSnapshotVersion(tx); err != nil {
		return Snapshot{}, err
	} else if version > 0 {
		snapshot.Version = version
	}

	tasksBucket := tx.Bucket(automationTasksBucket)
	if tasksBucket == nil {
		return snapshot, nil
	}

	tasks := make([]Task, 0)
	err := tasksBucket.ForEach(func(_, value []byte) error {
		var task Task
		if err := json.Unmarshal(value, &task); err != nil {
			return fmt.Errorf("parse automation task failed: %w", err)
		}
		task = NormalizeTask(task)
		if task.ID == "" {
			return nil
		}
		tasks = append(tasks, task)
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Tasks = tasks
	return snapshot, nil
}

func writeSnapshotTx(tx *bolt.Tx, snapshot Snapshot) error {
	if tx == nil {
		return errors.New("automation transaction is nil")
	}
	if snapshot.Version <= 0 {
		snapshot.Version = defaultSnapshotVersion
	}
	if err := writeSnapshotVersion(tx, snapshot.Version); err != nil {
		return err
	}

	if err := tx.DeleteBucket(automationTasksBucket); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
		return fmt.Errorf("reset automation tasks bucket failed: %w", err)
	}
	tasksBucket, err := tx.CreateBucketIfNotExists(automationTasksBucket)
	if err != nil {
		return fmt.Errorf("create automation tasks bucket failed: %w", err)
	}
	for _, task := range snapshot.Tasks {
		task = NormalizeTask(task)
		if task.ID == "" {
			continue
		}
		raw, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("marshal automation task failed: %w", err)
		}
		if err := tasksBucket.Put([]byte(task.ID), raw); err != nil {
			return fmt.Errorf("write automation task failed: %w", err)
		}
	}
	return nil
}

func readSnapshotVersion(tx *bolt.Tx) (int, error) {
	metaBucket := tx.Bucket(automationMetaBucket)
	if metaBucket == nil {
		return defaultSnapshotVersion, nil
	}
	raw := strings.TrimSpace(string(metaBucket.Get(snapshotVersionKey)))
	if raw == "" {
		return defaultSnapshotVersion, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse automation snapshot version failed: %w", err)
	}
	if version <= 0 {
		return defaultSnapshotVersion, nil
	}
	return version, nil
}

func writeSnapshotVersion(tx *bolt.Tx, version int) error {
	if version <= 0 {
		version = defaultSnapshotVersion
	}
	metaBucket := tx.Bucket(automationMetaBucket)
	if metaBucket == nil {
		return errors.New("automation meta bucket is missing")
	}
	if err := metaBucket.Put(snapshotVersionKey, []byte(strconv.Itoa(version))); err != nil {
		return fmt.Errorf("write automation snapshot version failed: %w", err)
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
