package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"alice/internal/domain"
)

type Config struct {
	RootDir          string
	SnapshotInterval int
}

type Store struct {
	EventLog  *JSONLEventLog
	Snapshots *GzipSnapshotStore
	Indexes   *BoltIndexStore
	RootDir   string
	appended  int
	snapshotN int
}

var ErrCriticalIndexApply = errors.New("critical index apply failed")

func Open(cfg Config) (*Store, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	if cfg.SnapshotInterval <= 0 {
		cfg.SnapshotInterval = 100
	}
	dirs := []string{
		filepath.Join(cfg.RootDir, "eventlog"),
		filepath.Join(cfg.RootDir, "snapshots"),
		filepath.Join(cfg.RootDir, "indexes"),
		filepath.Join(cfg.RootDir, "deadletters"),
		filepath.Join(cfg.RootDir, "blobs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	log, err := OpenJSONLEventLog(filepath.Join(cfg.RootDir, "eventlog"), 0, 0)
	if err != nil {
		return nil, err
	}
	snapshots, err := OpenGzipSnapshotStore(filepath.Join(cfg.RootDir, "snapshots"))
	if err != nil {
		_ = log.Close()
		return nil, err
	}
	indexes, err := OpenBoltIndexStore(filepath.Join(cfg.RootDir, "indexes", "indexes.db"))
	if err != nil {
		_ = log.Close()
		return nil, err
	}

	return &Store{
		EventLog:  log,
		Snapshots: snapshots,
		Indexes:   indexes,
		RootDir:   cfg.RootDir,
		snapshotN: cfg.SnapshotInterval,
	}, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	if s.EventLog != nil {
		if err := s.EventLog.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.Indexes != nil {
		if err := s.Indexes.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Store) AppendBatch(ctx context.Context, batch []domain.EventEnvelope) error {
	if len(batch) == 0 {
		return nil
	}
	if err := s.EventLog.Append(ctx, batch); err != nil {
		return err
	}
	if err := s.Indexes.ApplyCritical(ctx, batch); err != nil {
		return fmt.Errorf("%w: %v", ErrCriticalIndexApply, err)
	}
	if err := s.Indexes.ApplyLagging(ctx, batch); err != nil {
		// lagging projections can lag; keep append success semantics.
	}

	s.appended++
	if s.appended%s.snapshotN == 0 {
		_ = s.Snapshots.Save(ctx, &Snapshot{
			SnapshotID: time.Now().UTC().Format("20060102T150405.000000000"),
			SavedAt:    time.Now().UTC(),
		})
	}
	return nil
}

func (s *Store) Replay(ctx context.Context, fromHLC string, fn func(domain.EventEnvelope) error) error {
	return s.EventLog.Replay(ctx, fromHLC, fn)
}

func (s *Store) RebuildIndexes(ctx context.Context) error {
	replayFn := func(apply func(domain.EventEnvelope) error) error {
		return s.EventLog.Replay(ctx, "", apply)
	}
	if err := s.Indexes.RebuildCritical(ctx, replayFn); err != nil {
		return err
	}
	return s.Indexes.RebuildLagging(ctx, replayFn)
}
