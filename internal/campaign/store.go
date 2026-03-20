package campaign

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

func (s *Store) ListCampaigns(scopeKey, statusFilter string, limit int) ([]Campaign, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		return nil, errors.New("scope key is empty")
	}
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	var campaigns []Campaign
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		filtered := make([]Campaign, 0, len(snapshot.Campaigns))
		for _, raw := range snapshot.Campaigns {
			item := NormalizeCampaign(raw)
			if item.Session.ScopeKey != scopeKey {
				continue
			}
			if statusFilter != "" && statusFilter != "all" && string(item.Status) != statusFilter {
				continue
			}
			filtered = append(filtered, item)
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
		campaigns = filtered
		return nil
	})
	if err != nil {
		return nil, err
	}
	return campaigns, nil
}

func (s *Store) GetCampaign(campaignID string) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return Campaign{}, errors.New("campaign id is empty")
	}

	var item Campaign
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		idx := findCampaignIndex(snapshot.Campaigns, campaignID)
		if idx < 0 {
			return ErrCampaignNotFound
		}
		item = NormalizeCampaign(snapshot.Campaigns[idx])
		return nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return item, nil
}

func (s *Store) CreateCampaign(c Campaign) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	c = NormalizeCampaign(c)
	now := s.nowLocal()
	if c.ID == "" {
		c.ID = newID("camp", now)
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if err := ValidateCampaign(c); err != nil {
		return Campaign{}, err
	}

	var created Campaign
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		if findCampaignIndex(snapshot.Campaigns, c.ID) >= 0 {
			return false, fmt.Errorf("campaign id already exists: %s", c.ID)
		}
		snapshot.Campaigns = append(snapshot.Campaigns, c)
		created = c
		return true, nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return created, nil
}

func (s *Store) PatchCampaign(campaignID string, mutate func(*Campaign) error) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return Campaign{}, errors.New("campaign id is empty")
	}
	if mutate == nil {
		return Campaign{}, errors.New("mutate callback is nil")
	}

	var updated Campaign
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		idx := findCampaignIndex(snapshot.Campaigns, campaignID)
		if idx < 0 {
			return false, ErrCampaignNotFound
		}
		item := NormalizeCampaign(snapshot.Campaigns[idx])
		if err := mutate(&item); err != nil {
			return false, err
		}
		item = NormalizeCampaign(item)
		item.UpdatedAt = s.nowLocal()
		item.Revision++
		if err := ValidateCampaign(item); err != nil {
			return false, err
		}
		snapshot.Campaigns[idx] = item
		updated = item
		return true, nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return updated, nil
}

func (s *Store) UpsertTrial(campaignID string, trial Trial) (Campaign, Trial, error) {
	normalized := normalizeTrials([]Trial{trial})
	if len(normalized) == 0 {
		return Campaign{}, Trial{}, errors.New("trial is empty")
	}
	item := normalized[0]
	now := s.nowLocal()
	if item.ID == "" {
		item.ID = newID("trial", now)
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Status == "" {
		item.Status = TrialStatusPlanned
	}

	var persisted Trial
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		idx := findTrialIndex(campaign.Trials, item.ID)
		if idx >= 0 {
			item.CreatedAt = campaign.Trials[idx].CreatedAt
			campaign.Trials[idx] = item
		} else {
			campaign.Trials = append(campaign.Trials, item)
		}
		persisted = item
		return nil
	})
	if err != nil {
		return Campaign{}, Trial{}, err
	}
	return updated, persisted, nil
}

func (s *Store) AppendGuidance(campaignID string, guidance Guidance) (Campaign, Guidance, error) {
	now := s.nowLocal()
	guidance.ID = strings.TrimSpace(guidance.ID)
	guidance.Source = strings.TrimSpace(guidance.Source)
	guidance.Command = strings.TrimSpace(guidance.Command)
	guidance.Summary = strings.TrimSpace(guidance.Summary)
	if guidance.ID == "" {
		guidance.ID = newID("guidance", now)
	}
	if guidance.CreatedAt.IsZero() {
		guidance.CreatedAt = now
	}
	if guidance.Command == "" && guidance.Summary == "" {
		return Campaign{}, Guidance{}, errors.New("guidance command and summary are both empty")
	}

	var persisted Guidance
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		campaign.Guidance = append(campaign.Guidance, guidance)
		persisted = guidance
		return nil
	})
	if err != nil {
		return Campaign{}, Guidance{}, err
	}
	return updated, persisted, nil
}

func (s *Store) AppendReview(campaignID string, review Review) (Campaign, Review, error) {
	now := s.nowLocal()
	review.ID = strings.TrimSpace(review.ID)
	review.ReviewerID = strings.TrimSpace(review.ReviewerID)
	review.Provider = strings.TrimSpace(review.Provider)
	review.Model = strings.TrimSpace(review.Model)
	review.Summary = strings.TrimSpace(review.Summary)
	review.Confidence = strings.TrimSpace(review.Confidence)
	if review.ID == "" {
		review.ID = newID("review", now)
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = now
	}
	if review.Summary == "" && len(review.Findings) == 0 {
		return Campaign{}, Review{}, errors.New("review summary and findings are both empty")
	}

	var persisted Review
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		campaign.Reviews = append(campaign.Reviews, review)
		persisted = review
		return nil
	})
	if err != nil {
		return Campaign{}, Review{}, err
	}
	return updated, persisted, nil
}

func (s *Store) AppendPitfall(campaignID string, pitfall Pitfall) (Campaign, Pitfall, error) {
	now := s.nowLocal()
	pitfall.ID = strings.TrimSpace(pitfall.ID)
	pitfall.Summary = strings.TrimSpace(pitfall.Summary)
	pitfall.Reason = strings.TrimSpace(pitfall.Reason)
	pitfall.RelatedTrialID = strings.TrimSpace(pitfall.RelatedTrialID)
	pitfall.RetryIf = strings.TrimSpace(pitfall.RetryIf)
	pitfall.Tags = uniqueNonEmptyStrings(pitfall.Tags)
	if pitfall.ID == "" {
		pitfall.ID = newID("pitfall", now)
	}
	if pitfall.CreatedAt.IsZero() {
		pitfall.CreatedAt = now
	}
	if pitfall.Summary == "" {
		return Campaign{}, Pitfall{}, errors.New("pitfall summary is empty")
	}

	var persisted Pitfall
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		campaign.Pitfalls = append(campaign.Pitfalls, pitfall)
		persisted = pitfall
		return nil
	})
	if err != nil {
		return Campaign{}, Pitfall{}, err
	}
	return updated, persisted, nil
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
		return fmt.Errorf("create campaign dir failed: %w", err)
	}

	db, err := bolt.Open(s.path, 0o600, &bolt.Options{Timeout: time.Second})
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

func (s *Store) updateSnapshot(fn func(snapshot *Snapshot) (bool, error)) error {
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
		return Snapshot{}, errors.New("campaign transaction is nil")
	}
	snapshot := Snapshot{Version: defaultSnapshotVersion}
	if version, err := readSnapshotVersion(tx); err != nil {
		return Snapshot{}, err
	} else if version > 0 {
		snapshot.Version = version
	}
	bucket := tx.Bucket(campaignsBucket)
	if bucket == nil {
		return snapshot, nil
	}
	items := make([]Campaign, 0)
	err := bucket.ForEach(func(_, value []byte) error {
		var item Campaign
		if err := json.Unmarshal(value, &item); err != nil {
			return fmt.Errorf("parse campaign failed: %w", err)
		}
		item = NormalizeCampaign(item)
		if item.ID == "" {
			return nil
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Campaigns = items
	return snapshot, nil
}

func writeSnapshotTx(tx *bolt.Tx, snapshot Snapshot) error {
	if tx == nil {
		return errors.New("campaign transaction is nil")
	}
	if snapshot.Version <= 0 {
		snapshot.Version = defaultSnapshotVersion
	}
	if err := writeSnapshotVersion(tx, snapshot.Version); err != nil {
		return err
	}
	if err := tx.DeleteBucket(campaignsBucket); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
		return fmt.Errorf("reset campaigns bucket failed: %w", err)
	}
	bucket, err := tx.CreateBucketIfNotExists(campaignsBucket)
	if err != nil {
		return fmt.Errorf("create campaigns bucket failed: %w", err)
	}
	for _, item := range snapshot.Campaigns {
		item = NormalizeCampaign(item)
		if item.ID == "" {
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("marshal campaign failed: %w", err)
		}
		if err := bucket.Put([]byte(item.ID), raw); err != nil {
			return fmt.Errorf("write campaign failed: %w", err)
		}
	}
	return nil
}

func readSnapshotVersion(tx *bolt.Tx) (int, error) {
	bucket := tx.Bucket(metaBucket)
	if bucket == nil {
		return defaultSnapshotVersion, nil
	}
	raw := strings.TrimSpace(string(bucket.Get(versionKey)))
	if raw == "" {
		return defaultSnapshotVersion, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse campaign snapshot version failed: %w", err)
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
	bucket, err := tx.CreateBucketIfNotExists(metaBucket)
	if err != nil {
		return fmt.Errorf("create campaign meta bucket failed: %w", err)
	}
	return bucket.Put(versionKey, []byte(strconv.Itoa(version)))
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

func newID(prefix string, at time.Time) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "camp"
	}
	entropy := ulid.Monotonic(rand.Reader, 0)
	return prefix + "_" + strings.ToLower(ulid.MustNew(ulid.Timestamp(at), entropy).String())
}
