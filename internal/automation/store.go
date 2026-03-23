package automation

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/storeutil"
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
	if err := storeutil.EnsureParentDir(s.path); err != nil {
		return fmt.Errorf("create automation dir failed: %w", err)
	}

	db, err := storeutil.OpenDB(s.path, time.Second)
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
		return nil
	}); err != nil {
		_ = db.Close()
		return err
	}

	s.db = db
	return nil
}

func (s *Store) nowLocal() time.Time {
	now := time.Now().Local()
	if s != nil && s.now != nil {
		now = s.now().Local()
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
		now = time.Now().Local()
	}
	id, err := ulid.New(ulid.Timestamp(now), ulid.Monotonic(rand.Reader, 0))
	if err != nil {
		return fmt.Sprintf("task_%d", now.UnixNano())
	}
	return "task_" + strings.ToLower(id.String())
}
