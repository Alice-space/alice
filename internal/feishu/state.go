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
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || !target.Valid() {
		return nil
	}
	payload, err := json.Marshal(target)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketTargets)).Put([]byte(eventID), payload)
	})
}

func (s *StateStore) Target(eventID string) (ReplyTarget, bool, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ReplyTarget{}, false, nil
	}
	var target ReplyTarget
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketTargets)).Get([]byte(eventID))
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
