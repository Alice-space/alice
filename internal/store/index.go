package store

import (
	"context"
	"errors"
	"fmt"

	"alice/internal/domain"
	bolt "go.etcd.io/bbolt"
)

// BoltIndexStore provides indexed access to event data
type BoltIndexStore struct {
	db *bolt.DB
}

// newIndexStore creates a new BoltIndexStore
func newIndexStore(db *bolt.DB) *BoltIndexStore {
	return &BoltIndexStore{db: db}
}

// OpenBoltIndexStore opens a BoltDB index store at the given path
func OpenBoltIndexStore(path string) (*BoltIndexStore, error) {
	db, err := bolt.Open(path, 0o644, nil)
	if err != nil {
		return nil, fmt.Errorf("open index db: %w", err)
	}
	s := &BoltIndexStore{db: db}
	if err := s.initBuckets(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the index store
func (s *BoltIndexStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// initBuckets creates all required buckets
func (s *BoltIndexStore) initBuckets() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range criticalBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		for _, name := range laggingBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ApplyCritical applies critical index updates
func (s *BoltIndexStore) ApplyCritical(ctx context.Context, events []domain.EventEnvelope) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, evt := range events {
			if err := s.applyEventCritical(tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}

// RebuildCritical rebuilds critical indexes from scratch
func (s *BoltIndexStore) RebuildCritical(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		// Clear critical buckets
		for _, name := range criticalBuckets {
			if err := tx.DeleteBucket([]byte(name)); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
				return err
			}
			if _, err := tx.CreateBucket([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return replay(func(evt domain.EventEnvelope) error {
		return s.db.Update(func(tx *bolt.Tx) error {
			return s.applyEventCritical(tx, evt)
		})
	})
}

// ApplyLagging applies lagging projection updates
func (s *BoltIndexStore) ApplyLagging(ctx context.Context, events []domain.EventEnvelope) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, evt := range events {
			if err := s.applyEventLagging(tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}

// RebuildLagging rebuilds lagging projections from scratch
func (s *BoltIndexStore) RebuildLagging(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		// Clear lagging buckets
		for _, name := range laggingBuckets {
			if err := tx.DeleteBucket([]byte(name)); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
				return err
			}
			if _, err := tx.CreateBucket([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return replay(func(evt domain.EventEnvelope) error {
		return s.db.Update(func(tx *bolt.Tx) error {
			return s.applyEventLagging(tx, evt)
		})
	})
}
