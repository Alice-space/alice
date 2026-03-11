package store

import (
	"context"
	"encoding/json"
	"time"

	"alice/internal/domain"
	bolt "go.etcd.io/bbolt"
)

// GetRouteTarget looks up a route target by key
func (s *BoltIndexStore) GetRouteTarget(ctx context.Context, key string) (domain.RouteTarget, error) {
	var target domain.RouteTarget
	err := s.db.View(func(tx *bolt.Tx) error {
		// Check request routes
		b := tx.Bucket([]byte(bucketRequestsByRoute))
		if data := b.Get([]byte(key)); data != nil {
			target.Kind = domain.RouteTargetRequest
			target.ID = string(data)
			return nil
		}
		// Check task routes
		b = tx.Bucket([]byte(bucketTasksByRoute))
		if data := b.Get([]byte(key)); data != nil {
			target.Kind = domain.RouteTargetTask
			target.ID = string(data)
			return nil
		}
		return nil
	})
	return target, err
}

// IsTaskActive checks if a task is active
func (s *BoltIndexStore) IsTaskActive(ctx context.Context, taskID string) (bool, error) {
	var active bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketActiveTasks))
		active = b.Get([]byte(taskID)) != nil
		return nil
	})
	return active, err
}

// GetTaskIDByExecutionID looks up task ID by step execution ID
func (s *BoltIndexStore) GetTaskIDByExecutionID(ctx context.Context, executionID string) (string, error) {
	// Simplified - would need execution index
	return "", nil
}

// GetApprovalRequest looks up an approval request
func (s *BoltIndexStore) GetApprovalRequest(ctx context.Context, approvalID string) (domain.ApprovalRequestView, bool, error) {
	var approval domain.ApprovalRequestView
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketApprovalQueue))
		data := b.Get([]byte(approvalID))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &approval)
	})
	return approval, found, err
}

// GetHumanWait looks up a human wait
func (s *BoltIndexStore) GetHumanWait(ctx context.Context, waitID string) (domain.HumanWaitView, bool, error) {
	var wait domain.HumanWaitView
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketHumanQueue))
		data := b.Get([]byte(waitID))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &wait)
	})
	return wait, found, err
}

// GetScheduleSource looks up a schedule source
func (s *BoltIndexStore) GetScheduleSource(ctx context.Context, scheduledTaskID string) (domain.ScheduleSourceView, bool, error) {
	var source domain.ScheduleSourceView
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketScheduleSources))
		data := b.Get([]byte(scheduledTaskID))
		if data == nil {
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

// ListScheduleSources lists all schedule sources
func (s *BoltIndexStore) ListScheduleSources(ctx context.Context) ([]domain.ScheduleSourceView, error) {
	var sources []domain.ScheduleSourceView
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketScheduleSources))
		return b.ForEach(func(k, v []byte) error {
			var record ScheduleSourceIndexRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			sources = append(sources, domain.ScheduleSourceView{
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
	return sources, err
}

// ListPendingOutbox lists pending outbox items
func (s *BoltIndexStore) ListPendingOutbox(ctx context.Context, domain string, before time.Time, limit int) ([]PendingOutboxIndexRecord, error) {
	var records []PendingOutboxIndexRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketPendingOutbox))
		c := b.Cursor()
		count := 0
		for k, v := c.First(); k != nil && count < limit; k, v = c.Next() {
			var record PendingOutboxIndexRecord
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}
			if domain != "" && record.Domain != domain {
				continue
			}
			if record.NextAttemptAt.After(before) {
				continue
			}
			records = append(records, record)
			count++
		}
		return nil
	})
	return records, err
}

// FindPendingOutboxByRemote looks up outbox by remote request ID
func (s *BoltIndexStore) FindPendingOutboxByRemote(ctx context.Context, actionID, remoteReqID, domain string) (PendingOutboxIndexRecord, bool, error) {
	// Simplified implementation
	return PendingOutboxIndexRecord{}, false, nil
}

// GetDedupeRecord gets a deduplication record
func (s *BoltIndexStore) GetDedupeRecord(ctx context.Context, key string) (DedupeRecord, bool, error) {
	var record DedupeRecord
	var found bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDedupeWindow))
		data := b.Get([]byte(key))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &record)
	})
	return record, found, err
}

// PutDedupeRecord stores a deduplication record
func (s *BoltIndexStore) PutDedupeRecord(ctx context.Context, key string, record DedupeRecord) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDedupeWindow))
		val, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), val)
	})
}

// DedupeSeen checks if a dedupe key exists
func (s *BoltIndexStore) DedupeSeen(ctx context.Context, key string) (bool, error) {
	seen := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketDedupeWindow))
		seen = b.Get([]byte(key)) != nil
		return nil
	})
	return seen, err
}

// Overview returns operations overview
func (s *BoltIndexStore) Overview(ctx context.Context) (OpsOverview, error) {
	var overview OpsOverview
	err := s.db.View(func(tx *bolt.Tx) error {
		// Count open requests
		b := tx.Bucket([]byte(bucketOpenRequests))
		overview.OpenRequests = b.Stats().KeyN

		// Count active tasks
		b = tx.Bucket([]byte(bucketActiveTasks))
		overview.ActiveTasks = b.Stats().KeyN

		// Count pending outbox
		b = tx.Bucket([]byte(bucketPendingOutbox))
		overview.PendingOutbox = b.Stats().KeyN

		// Count approvals
		b = tx.Bucket([]byte(bucketApprovalQueue))
		overview.ApprovalQueue = b.Stats().KeyN

		return nil
	})
	return overview, err
}
