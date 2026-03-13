package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"alice/internal/domain"
	bolt "go.etcd.io/bbolt"
)

// GetRouteTarget looks up a route target by key.
func (s *BoltIndexStore) GetRouteTarget(_ context.Context, key string) (domain.RouteTarget, error) {
	var target domain.RouteTarget
	err := s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket([]byte(bucketTasksByRoute)).Get([]byte(key)); len(v) > 0 {
			target = domain.RouteTarget{Kind: domain.RouteTargetTask, ID: string(v)}
			return nil
		}
		if v := tx.Bucket([]byte(bucketRequestsByRoute)).Get([]byte(key)); len(v) > 0 {
			target = domain.RouteTarget{Kind: domain.RouteTargetRequest, ID: string(v)}
		}
		return nil
	})
	return target, err
}

// IsTaskActive checks if a task is active.
func (s *BoltIndexStore) IsTaskActive(_ context.Context, taskID string) (bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false, nil
	}
	var active bool
	err := s.db.View(func(tx *bolt.Tx) error {
		active = len(tx.Bucket([]byte(bucketActiveTasks)).Get([]byte(taskID))) > 0
		return nil
	})
	return active, err
}

// IsRequestOpen checks if a request is open.
func (s *BoltIndexStore) IsRequestOpen(_ context.Context, requestID string) (bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false, nil
	}
	var open bool
	err := s.db.View(func(tx *bolt.Tx) error {
		open = len(tx.Bucket([]byte(bucketOpenRequests)).Get([]byte(requestID))) > 0
		return nil
	})
	return open, err
}

// GetTaskIDByExecutionID looks up task ID by step execution ID.
func (s *BoltIndexStore) GetTaskIDByExecutionID(_ context.Context, _ string) (string, error) {
	// Step execution index is not materialized in v1 critical store.
	return "", nil
}

// GetApprovalRequest looks up an approval request.
func (s *BoltIndexStore) GetApprovalRequest(_ context.Context, approvalID string) (domain.ApprovalRequestOpenedPayload, bool, error) {
	var approval domain.ApprovalRequestOpenedPayload
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketApprovalQueue)).Get([]byte(approvalID))
		if len(data) == 0 {
			return nil
		}
		found = true
		return json.Unmarshal(data, &approval)
	})
	return approval, found, err
}

// GetHumanWait looks up a human wait.
func (s *BoltIndexStore) GetHumanWait(_ context.Context, waitID string) (domain.HumanWaitRecordedPayload, bool, error) {
	var wait domain.HumanWaitRecordedPayload
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketHumanQueue)).Get([]byte("wait:" + waitID))
		if len(data) == 0 {
			return nil
		}
		found = true
		return json.Unmarshal(data, &wait)
	})
	return wait, found, err
}

// GetScheduleSource looks up a schedule source.
func (s *BoltIndexStore) GetScheduleSource(_ context.Context, scheduledTaskID string) (domain.ScheduleSourceView, bool, error) {
	var source domain.ScheduleSourceView
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket([]byte(bucketScheduleSources)).Get([]byte(scheduledTaskID))
		if len(data) == 0 {
			return nil
		}
		found = true
		var record ScheduleSourceIndexRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		source = domain.ScheduleSourceView{
			ScheduledTaskID:      record.ScheduledTaskID,
			SpecKind:             record.SpecKind,
			SpecText:             record.SpecText,
			Timezone:             record.Timezone,
			ScheduleRevision:     record.ScheduleRevision,
			TargetWorkflowID:     record.TargetWorkflowID,
			TargetWorkflowSource: record.TargetWorkflowSource,
			TargetWorkflowRev:    record.TargetWorkflowRev,
			Enabled:              record.Enabled,
			NextFireAt:           record.NextFireAt,
			LastFireAt:           record.LastFireAt,
		}
		return nil
	})
	return source, found, err
}

// ListScheduleSources lists all schedule sources.
func (s *BoltIndexStore) ListScheduleSources(_ context.Context) ([]domain.ScheduleSourceView, error) {
	out := []domain.ScheduleSourceView{}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketScheduleSources))
		return b.ForEach(func(_, v []byte) error {
			var record ScheduleSourceIndexRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			out = append(out, domain.ScheduleSourceView{
				ScheduledTaskID:      record.ScheduledTaskID,
				SpecKind:             record.SpecKind,
				SpecText:             record.SpecText,
				Timezone:             record.Timezone,
				ScheduleRevision:     record.ScheduleRevision,
				TargetWorkflowID:     record.TargetWorkflowID,
				TargetWorkflowSource: record.TargetWorkflowSource,
				TargetWorkflowRev:    record.TargetWorkflowRev,
				Enabled:              record.Enabled,
				NextFireAt:           record.NextFireAt,
				LastFireAt:           record.LastFireAt,
			})
			return nil
		})
	})
	return out, err
}

// ListPendingOutbox lists pending outbox records.
func (s *BoltIndexStore) ListPendingOutbox(_ context.Context, mcpDomain string, now time.Time, limit int) ([]PendingOutboxIndexRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	out := make([]PendingOutboxIndexRecord, 0, limit)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketPendingOutbox))
		return b.ForEach(func(_, v []byte) error {
			var record PendingOutboxIndexRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			if mcpDomain != "" && record.Domain != mcpDomain {
				return nil
			}
			if !record.NextAttemptAt.IsZero() && record.NextAttemptAt.After(now) {
				return nil
			}
			out = append(out, record)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NextAttemptAt.Before(out[j].NextAttemptAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// GetPendingOutboxByActionID gets pending outbox by action id.
func (s *BoltIndexStore) GetPendingOutboxByActionID(_ context.Context, actionID string) (PendingOutboxIndexRecord, bool, error) {
	var out PendingOutboxIndexRecord
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketPendingOutbox)).Get([]byte(actionID))
		if len(v) == 0 {
			return nil
		}
		ok = true
		return json.Unmarshal(v, &out)
	})
	return out, ok, err
}

// FindPendingOutboxByRemote finds a pending outbox item by identifiers.
func (s *BoltIndexStore) FindPendingOutboxByRemote(_ context.Context, actionID, remoteReqID, idempotencyKey string) (PendingOutboxIndexRecord, bool, error) {
	var marker = errors.New("found")
	if actionID != "" {
		if r, ok, err := s.GetPendingOutboxByActionID(context.Background(), actionID); err != nil || ok {
			return r, ok, err
		}
	}
	if remoteReqID != "" {
		var action string
		err := s.db.View(func(tx *bolt.Tx) error {
			v := tx.Bucket([]byte(bucketOutboxByRemote)).Get([]byte(remoteReqID))
			if len(v) > 0 {
				action = string(v)
			}
			return nil
		})
		if err != nil {
			return PendingOutboxIndexRecord{}, false, err
		}
		if action != "" {
			return s.GetPendingOutboxByActionID(context.Background(), action)
		}
	}
	if idempotencyKey != "" {
		var out PendingOutboxIndexRecord
		var ok bool
		err := s.db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(bucketPendingOutbox))
			return b.ForEach(func(_, v []byte) error {
				var record PendingOutboxIndexRecord
				if err := json.Unmarshal(v, &record); err != nil {
					return err
				}
				if record.IdempotencyKey == idempotencyKey {
					out = record
					ok = true
					return marker
				}
				return nil
			})
		})
		if err != nil && !errors.Is(err, marker) {
			return PendingOutboxIndexRecord{}, false, err
		}
		return out, ok, nil
	}
	return PendingOutboxIndexRecord{}, false, nil
}

// FindPendingOutboxByLookup is a backward-compatible alias.
func (s *BoltIndexStore) FindPendingOutboxByLookup(ctx context.Context, actionID, remoteReqID, idempotencyKey string) (PendingOutboxIndexRecord, bool, error) {
	return s.FindPendingOutboxByRemote(ctx, actionID, remoteReqID, idempotencyKey)
}

// GetDedupeRecord gets a dedupe record.
func (s *BoltIndexStore) GetDedupeRecord(_ context.Context, key string) (DedupeRecord, bool, error) {
	if key == "" {
		return DedupeRecord{}, false, nil
	}
	var out DedupeRecord
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket([]byte(bucketDedupeWindow)).Get([]byte(key))
		if len(raw) == 0 {
			return nil
		}
		ok = true
		if raw[0] == '{' {
			return json.Unmarshal(raw, &out)
		}
		out = DedupeRecord{CommitHLC: string(raw)}
		return nil
	})
	return out, ok, err
}

// PutDedupeRecord stores a dedupe record.
func (s *BoltIndexStore) PutDedupeRecord(_ context.Context, key string, record DedupeRecord) error {
	if key == "" {
		return nil
	}
	value, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketDedupeWindow)).Put([]byte(key), value)
	})
}

// SetDedupeRecord is a backward-compatible alias.
func (s *BoltIndexStore) SetDedupeRecord(ctx context.Context, key string, record DedupeRecord) error {
	return s.PutDedupeRecord(ctx, key, record)
}

// DedupeSeen checks whether the dedupe key already exists.
func (s *BoltIndexStore) DedupeSeen(ctx context.Context, key string) (bool, error) {
	_, seen, err := s.GetDedupeRecord(ctx, key)
	return seen, err
}

// ListHumanActionQueue lists serialized human-action queue entries.
func (s *BoltIndexStore) ListHumanActionQueue(_ context.Context) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketHumanQueue))
		return b.ForEach(func(_, v []byte) error {
			out = append(out, append([]byte(nil), v...))
			return nil
		})
	})
	return out, err
}

// Overview returns operations overview counters.
func (s *BoltIndexStore) Overview(_ context.Context) (OpsOverview, error) {
	var out OpsOverview
	err := s.db.View(func(tx *bolt.Tx) error {
		out.OpenRequests = tx.Bucket([]byte(bucketOpenRequests)).Stats().KeyN
		out.ActiveTasks = tx.Bucket([]byte(bucketActiveTasks)).Stats().KeyN
		out.PendingOutbox = tx.Bucket([]byte(bucketPendingOutbox)).Stats().KeyN
		out.ApprovalQueue = tx.Bucket([]byte(bucketApprovalQueue)).Stats().KeyN
		out.HumanQueue = tx.Bucket([]byte(bucketHumanQueue)).Stats().KeyN
		return nil
	})
	return out, err
}
