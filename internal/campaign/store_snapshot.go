package campaign

import (
	"encoding/json"
	"errors"
	"fmt"

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
	version, err := storeutil.ReadVersion(tx.Bucket(metaBucket), versionKey, defaultSnapshotVersion)
	if err != nil {
		return 0, fmt.Errorf("parse campaign snapshot version failed: %w", err)
	}
	return version, nil
}

func writeSnapshotVersion(tx *bolt.Tx, version int) error {
	bucket, err := tx.CreateBucketIfNotExists(metaBucket)
	if err != nil {
		return fmt.Errorf("create campaign meta bucket failed: %w", err)
	}
	if err := storeutil.WriteVersion(bucket, versionKey, version, defaultSnapshotVersion); err != nil {
		return fmt.Errorf("write campaign snapshot version failed: %w", err)
	}
	return nil
}
