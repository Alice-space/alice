package campaign

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
	ErrCampaignNotFound = errors.New("campaign not found")

	metaBucket      = []byte("meta")
	campaignsBucket = []byte("campaigns")
	versionKey      = []byte("version")
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
		return fmt.Errorf("create campaign dir failed: %w", err)
	}

	db, err := storeutil.OpenDB(s.path, time.Second)
	if err != nil {
		return fmt.Errorf("open campaign db failed: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(metaBucket); err != nil {
			return fmt.Errorf("create campaign meta bucket failed: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(campaignsBucket); err != nil {
			return fmt.Errorf("create campaigns bucket failed: %w", err)
		}
		return writeSnapshotVersion(tx, defaultSnapshotVersion)
	}); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *Store) nowLocal() time.Time {
	if s == nil || s.now == nil {
		return time.Now().Local()
	}
	now := s.now()
	if now.IsZero() {
		return time.Now().Local()
	}
	return now.Local()
}

func findCampaignIndex(values []Campaign, campaignID string) int {
	campaignID = strings.TrimSpace(campaignID)
	for idx, item := range values {
		if strings.TrimSpace(item.ID) == campaignID {
			return idx
		}
	}
	return -1
}

func findTrialIndex(values []Trial, trialID string) int {
	trialID = strings.TrimSpace(trialID)
	for idx, item := range values {
		if strings.TrimSpace(item.ID) == trialID {
			return idx
		}
	}
	return -1
}

func newID(prefix string, at time.Time) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "camp"
	}
	entropy := ulid.Monotonic(rand.Reader, 0)
	return prefix + "_" + strings.ToLower(ulid.MustNew(ulid.Timestamp(at), entropy).String())
}
