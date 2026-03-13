package feishu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	bucketTargets   = "targets"
	bucketDelivered = "delivered"
	bucketCursor    = "cursor"
	cursorKey       = "eventlog_hlc"
	scopeEvent      = "event"
	scopeRequest    = "request"
	scopeTask       = "task"
)

// StateStore persists Feishu-specific reply routing state without leaking transport concerns into the core store.
type StateStore struct {
	db *bolt.DB
}

func OpenStateStore(storageRoot string) (*StateStore, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, fmt.Errorf("feishu state storage root is required")
	}
	dir := filepath.Join(storageRoot, "feishu")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create feishu state dir: %w", err)
	}
	db, err := bolt.Open(filepath.Join(dir, "state.db"), 0o644, nil)
	if err != nil {
		return nil, fmt.Errorf("open feishu state db: %w", err)
	}
	store := &StateStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *StateStore) init() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range []string{bucketTargets, bucketDelivered, bucketCursor} {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *StateStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *StateStore) SaveTarget(eventID string, target ReplyTarget) error {
	return s.saveScopedTarget(scopeEvent, eventID, target)
}

func (s *StateStore) SaveRequestTarget(requestID string, target ReplyTarget) error {
	return s.saveScopedTarget(scopeRequest, requestID, target)
}

func (s *StateStore) SaveTaskTarget(taskID string, target ReplyTarget) error {
	return s.saveScopedTarget(scopeTask, taskID, target)
}

func (s *StateStore) saveScopedTarget(scope, id string, target ReplyTarget) error {
	id = strings.TrimSpace(id)
	if id == "" || !target.Valid() {
		return nil
	}
	payload, err := json.Marshal(target)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketTargets)).Put([]byte(targetKey(scope, id)), payload)
	})
}

func (s *StateStore) Target(eventID string) (ReplyTarget, bool, error) {
	target, ok, err := s.loadScopedTarget(scopeEvent, eventID)
	if ok || err != nil {
		return target, ok, err
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ReplyTarget{}, false, nil
	}
	var legacy ReplyTarget
	var found bool
	err = s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketTargets)).Get([]byte(eventID))
		if len(data) == 0 {
			return nil
		}
		found = true
		return json.Unmarshal(data, &legacy)
	})
	return legacy, found, err
}

func (s *StateStore) RequestTarget(requestID string) (ReplyTarget, bool, error) {
	return s.loadScopedTarget(scopeRequest, requestID)
}

func (s *StateStore) TaskTarget(taskID string) (ReplyTarget, bool, error) {
	return s.loadScopedTarget(scopeTask, taskID)
}

func (s *StateStore) loadScopedTarget(scope, id string) (ReplyTarget, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ReplyTarget{}, false, nil
	}
	var target ReplyTarget
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketTargets)).Get([]byte(targetKey(scope, id)))
		if len(data) == 0 {
			return nil
		}
		ok = true
		return json.Unmarshal(data, &target)
	})
	return target, ok, err
}

func (s *StateStore) Delivered(replyID string) (bool, error) {
	replyID = strings.TrimSpace(replyID)
	if replyID == "" {
		return false, nil
	}
	var delivered bool
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketDelivered)).Get([]byte(replyID))
		delivered = len(data) > 0
		return nil
	})
	return delivered, err
}

func (s *StateStore) MarkDelivered(replyID, remoteMessageID string, at time.Time) error {
	replyID = strings.TrimSpace(replyID)
	if replyID == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]string{
		"remote_message_id": strings.TrimSpace(remoteMessageID),
		"delivered_at":      at.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketDelivered)).Put([]byte(replyID), payload)
	})
}

func (s *StateStore) Cursor() (string, error) {
	var cursor string
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor = string(tx.Bucket([]byte(bucketCursor)).Get([]byte(cursorKey)))
		return nil
	})
	return cursor, err
}

func (s *StateStore) SaveCursor(cursor string) error {
	cursor = strings.TrimSpace(cursor)
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketCursor)).Put([]byte(cursorKey), []byte(cursor))
	})
}

func targetKey(scope, id string) string {
	scope = strings.TrimSpace(scope)
	id = strings.TrimSpace(id)
	if scope == "" {
		return id
	}
	return scope + ":" + id
}
