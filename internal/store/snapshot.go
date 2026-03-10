package store

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"alice/internal/domain"
)

type Snapshot struct {
	SnapshotID          string                             `json:"snapshot_id"`
	SavedAt             time.Time                          `json:"saved_at"`
	LastGlobalHLC       string                             `json:"last_global_hlc"`
	LastSequence        map[string]uint64                  `json:"last_sequence"`
	Requests            map[string]domain.EphemeralRequest `json:"requests"`
	Tasks               map[string]domain.DurableTask      `json:"tasks"`
	ActiveBindings      map[string]domain.WorkflowBinding  `json:"active_bindings"`
	ActiveSteps         map[string]domain.StepExecution    `json:"active_steps"`
	OpenApprovals       map[string]domain.ApprovalRequest  `json:"open_approvals"`
	PendingOutbox       map[string]domain.OutboxRecord     `json:"pending_outbox"`
	ActiveSchedules     map[string]domain.ScheduledTask    `json:"active_schedules"`
	IndexCheckpointRefs map[string]string                  `json:"index_checkpoint_refs"`
}

type SnapshotStore interface {
	LoadLatest(ctx context.Context) (*Snapshot, error)
	Save(ctx context.Context, snapshot *Snapshot) error
}

type GzipSnapshotStore struct {
	root string
}

func OpenGzipSnapshotStore(root string) (*GzipSnapshotStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}
	return &GzipSnapshotStore{root: root}, nil
}

func (s *GzipSnapshotStore) LoadLatest(_ context.Context) (*Snapshot, error) {
	paths, err := s.snapshotFiles()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return s.loadFile(paths[len(paths)-1])
}

func (s *GzipSnapshotStore) Save(_ context.Context, snapshot *Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}
	if snapshot.SnapshotID == "" {
		snapshot.SnapshotID = time.Now().UTC().Format("20060102T150405.000000000")
	}
	if snapshot.SavedAt.IsZero() {
		snapshot.SavedAt = time.Now().UTC()
	}
	target := filepath.Join(s.root, snapshot.SnapshotID+".json.gz")
	tmp := target + ".tmp"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open temp snapshot: %w", err)
	}
	gz := gzip.NewWriter(f)
	enc := json.NewEncoder(gz)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshot); err != nil {
		_ = gz.Close()
		_ = f.Close()
		return fmt.Errorf("encode snapshot: %w", err)
	}
	if err := gz.Close(); err != nil {
		_ = f.Close()
		return fmt.Errorf("close gzip: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close snapshot file: %w", err)
	}
	return os.Rename(tmp, target)
}

func (s *GzipSnapshotStore) snapshotFiles() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == ".gz" {
			out = append(out, filepath.Join(s.root, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *GzipSnapshotStore) loadFile(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open snapshot: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open gzip reader: %w", err)
	}
	defer gz.Close()

	var snap Snapshot
	if err := json.NewDecoder(gz).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}
