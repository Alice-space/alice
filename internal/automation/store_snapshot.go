package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/storeutil"
	bolt "go.etcd.io/bbolt"
)

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
	version, err := storeutil.ReadVersion(tx.Bucket(automationMetaBucket), snapshotVersionKey, defaultSnapshotVersion)
	if err != nil {
		return 0, fmt.Errorf("parse automation snapshot version failed: %w", err)
	}
	return version, nil
}

func writeSnapshotVersion(tx *bolt.Tx, version int) error {
	metaBucket := tx.Bucket(automationMetaBucket)
	if metaBucket == nil {
		return errors.New("automation meta bucket is missing")
	}
	if err := storeutil.WriteVersion(metaBucket, snapshotVersionKey, version, defaultSnapshotVersion); err != nil {
		return fmt.Errorf("write automation snapshot version failed: %w", err)
	}
	return nil
}

func readTaskTx(tx *bolt.Tx, taskID string) (Task, error) {
	if tx == nil {
		return Task{}, errors.New("automation transaction is nil")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Task{}, errors.New("task id is empty")
	}
	tasksBucket := tx.Bucket(automationTasksBucket)
	if tasksBucket == nil {
		return Task{}, ErrTaskNotFound
	}
	raw := tasksBucket.Get([]byte(taskID))
	if len(raw) == 0 {
		return Task{}, ErrTaskNotFound
	}
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, fmt.Errorf("parse automation task failed: %w", err)
	}
	task = NormalizeTask(task)
	if task.ID == "" {
		return Task{}, ErrTaskNotFound
	}
	return task, nil
}

func writeTaskTx(tx *bolt.Tx, task Task) error {
	if tx == nil {
		return errors.New("automation transaction is nil")
	}
	task = NormalizeTask(task)
	if task.ID == "" {
		return errors.New("task id is empty")
	}
	tasksBucket := tx.Bucket(automationTasksBucket)
	if tasksBucket == nil {
		return errors.New("automation tasks bucket is missing")
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal automation task failed: %w", err)
	}
	if err := tasksBucket.Put([]byte(task.ID), raw); err != nil {
		return fmt.Errorf("write automation task failed: %w", err)
	}
	return nil
}
