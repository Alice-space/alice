package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"alice/internal/domain"
	bolt "go.etcd.io/bbolt"
)

const (
	bucketRequestsByRoute = "requests_by_route"
	bucketTasksByRoute    = "tasks_by_route"
	bucketOpenRequests    = "open_requests"
	bucketActiveTasks     = "active_tasks"
	bucketPendingOutbox   = "pending_outbox"
	bucketDedupeWindow    = "dedupe_window"
	bucketScheduleSources = "schedule_sources"
	bucketOutboxByRemote  = "outbox_by_remote"
	bucketApprovalQueue   = "approval_queue"
	bucketHumanQueue      = "human_action_queue"
	bucketOpsViews        = "ops_views"
)

var criticalBuckets = []string{
	bucketRequestsByRoute,
	bucketTasksByRoute,
	bucketOpenRequests,
	bucketActiveTasks,
	bucketPendingOutbox,
	bucketDedupeWindow,
	bucketScheduleSources,
	bucketOutboxByRemote,
}

var laggingBuckets = []string{
	bucketApprovalQueue,
	bucketHumanQueue,
	bucketOpsViews,
}

type CriticalIndexStore interface {
	ApplyCritical(ctx context.Context, events []domain.EventEnvelope) error
	RebuildCritical(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}

type ProjectionStore interface {
	ApplyLagging(ctx context.Context, events []domain.EventEnvelope) error
	RebuildLagging(ctx context.Context, replay func(func(domain.EventEnvelope) error) error) error
}

type BoltIndexStore struct {
	db *bolt.DB
}

type PendingOutboxIndexRecord struct {
	ActionID        string    `json:"action_id"`
	TaskID          string    `json:"task_id"`
	Domain          string    `json:"domain"`
	ActionType      string    `json:"action_type"`
	TargetRef       string    `json:"target_ref"`
	IdempotencyKey  string    `json:"idempotency_key"`
	RemoteRequestID string    `json:"remote_request_id"`
	PayloadRef      string    `json:"payload_ref"`
	AttemptCount    uint32    `json:"attempt_count"`
	NextAttemptAt   time.Time `json:"next_attempt_at"`
}

type DedupeRecord struct {
	CommitHLC       string `json:"commit_hlc"`
	EventID         string `json:"event_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	RouteTargetKind string `json:"route_target_kind,omitempty"`
	RouteTargetID   string `json:"route_target_id,omitempty"`
}

type ScheduleSourceIndexRecord struct {
	ScheduledTaskID      string    `json:"scheduled_task_id"`
	SpecKind             string    `json:"spec_kind"`
	SpecText             string    `json:"spec_text"`
	Timezone             string    `json:"timezone"`
	ScheduleRevision     string    `json:"schedule_revision"`
	TargetWorkflowID     string    `json:"target_workflow_id"`
	TargetWorkflowSource string    `json:"target_workflow_source"`
	TargetWorkflowRev    string    `json:"target_workflow_rev"`
	Enabled              bool      `json:"enabled"`
	NextFireAt           time.Time `json:"next_fire_at"`
	LastFireAt           time.Time `json:"last_fire_at"`
}

type OpsOverview struct {
	OpenRequests  int `json:"open_requests"`
	ActiveTasks   int `json:"active_tasks"`
	PendingOutbox int `json:"pending_outbox"`
	ApprovalQueue int `json:"approval_queue"`
	HumanQueue    int `json:"human_queue"`
}

func OpenBoltIndexStore(path string) (*BoltIndexStore, error) {
	db, err := bolt.Open(path, 0o644, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	s := &BoltIndexStore{db: db}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, b := range append(append([]string{}, criticalBuckets...), laggingBuckets...) {
			if _, err := tx.CreateBucketIfNotExists([]byte(b)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}
	return s, nil
}

func (s *BoltIndexStore) Close() error {
	return s.db.Close()
}

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

func (s *BoltIndexStore) IsTaskActive(_ context.Context, taskID string) (bool, error) {
	if taskID == "" {
		return false, nil
	}
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		ok = len(tx.Bucket([]byte(bucketActiveTasks)).Get([]byte(taskID))) > 0
		return nil
	})
	return ok, err
}

func (s *BoltIndexStore) IsRequestOpen(_ context.Context, requestID string) (bool, error) {
	if requestID == "" {
		return false, nil
	}
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		ok = len(tx.Bucket([]byte(bucketOpenRequests)).Get([]byte(requestID))) > 0
		return nil
	})
	return ok, err
}

func (s *BoltIndexStore) DedupeSeen(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	_, seen, err := s.GetDedupeRecord(context.Background(), key)
	return seen, err
}

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
		if len(raw) > 0 && raw[0] == '{' {
			if err := json.Unmarshal(raw, &out); err != nil {
				return err
			}
		} else {
			out = DedupeRecord{CommitHLC: string(raw)}
		}
		return nil
	})
	return out, ok, err
}

func (s *BoltIndexStore) SetDedupeRecord(_ context.Context, key string, record DedupeRecord) error {
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

func (s *BoltIndexStore) ApplyCritical(_ context.Context, events []domain.EventEnvelope) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, evt := range events {
			if err := applyCriticalEvent(tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltIndexStore) ApplyLagging(_ context.Context, events []domain.EventEnvelope) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, evt := range events {
			if err := applyLaggingEvent(tx, evt); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltIndexStore) RebuildCritical(_ context.Context, replay func(func(domain.EventEnvelope) error) error) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, b := range criticalBuckets {
			if err := tx.DeleteBucket([]byte(b)); err != nil {
				return err
			}
			if _, err := tx.CreateBucket([]byte(b)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return replay(func(evt domain.EventEnvelope) error {
		return s.db.Update(func(tx *bolt.Tx) error {
			return applyCriticalEvent(tx, evt)
		})
	})
}

func (s *BoltIndexStore) RebuildLagging(_ context.Context, replay func(func(domain.EventEnvelope) error) error) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		for _, b := range laggingBuckets {
			if err := tx.DeleteBucket([]byte(b)); err != nil {
				return err
			}
			if _, err := tx.CreateBucket([]byte(b)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return replay(func(evt domain.EventEnvelope) error {
		return s.db.Update(func(tx *bolt.Tx) error {
			return applyLaggingEvent(tx, evt)
		})
	})
}

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

func (s *BoltIndexStore) FindPendingOutboxByLookup(_ context.Context, actionID, remoteRequestID, idempotencyKey string) (PendingOutboxIndexRecord, bool, error) {
	var found = errors.New("found")
	if actionID != "" {
		if r, ok, err := s.GetPendingOutboxByActionID(context.Background(), actionID); err != nil || ok {
			return r, ok, err
		}
	}
	if remoteRequestID != "" {
		var action string
		err := s.db.View(func(tx *bolt.Tx) error {
			v := tx.Bucket([]byte(bucketOutboxByRemote)).Get([]byte(remoteRequestID))
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
				var r PendingOutboxIndexRecord
				if err := json.Unmarshal(v, &r); err != nil {
					return err
				}
				if r.IdempotencyKey == idempotencyKey {
					out = r
					ok = true
					return found
				}
				return nil
			})
		})
		if err != nil && !errors.Is(err, found) {
			return PendingOutboxIndexRecord{}, false, err
		}
		return out, ok, nil
	}
	return PendingOutboxIndexRecord{}, false, nil
}

func (s *BoltIndexStore) GetScheduleSource(_ context.Context, scheduledTaskID string) (ScheduleSourceIndexRecord, bool, error) {
	var out ScheduleSourceIndexRecord
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketScheduleSources)).Get([]byte(scheduledTaskID))
		if len(v) == 0 {
			return nil
		}
		ok = true
		return json.Unmarshal(v, &out)
	})
	return out, ok, err
}

func (s *BoltIndexStore) ListScheduleSources(_ context.Context) ([]ScheduleSourceIndexRecord, error) {
	out := []ScheduleSourceIndexRecord{}
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketScheduleSources))
		return b.ForEach(func(_, v []byte) error {
			var record ScheduleSourceIndexRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return err
			}
			out = append(out, record)
			return nil
		})
	})
	return out, err
}

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

func (s *BoltIndexStore) GetApprovalRequest(_ context.Context, approvalRequestID string) (domain.ApprovalRequestOpenedPayload, bool, error) {
	var out domain.ApprovalRequestOpenedPayload
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketApprovalQueue)).Get([]byte(approvalRequestID))
		if len(v) == 0 {
			return nil
		}
		ok = true
		return json.Unmarshal(v, &out)
	})
	return out, ok, err
}

func (s *BoltIndexStore) GetHumanWait(_ context.Context, humanWaitID string) (domain.HumanWaitRecordedPayload, bool, error) {
	var out domain.HumanWaitRecordedPayload
	var ok bool
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketHumanQueue)).Get([]byte("wait:" + humanWaitID))
		if len(v) == 0 {
			return nil
		}
		ok = true
		return json.Unmarshal(v, &out)
	})
	return out, ok, err
}

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

func applyCriticalEvent(tx *bolt.Tx, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeExternalEventIngested:
		var payload domain.ExternalEventIngestedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		if payload.Event.IdempotencyKey != "" {
			if err := tx.Bucket([]byte(bucketDedupeWindow)).Put([]byte(payload.Event.IdempotencyKey), []byte(evt.GlobalHLC)); err != nil {
				return err
			}
		}
	case domain.EventTypeEphemeralRequestOpened:
		var payload domain.EphemeralRequestOpenedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketOpenRequests)).Put([]byte(payload.RequestID), []byte(evt.GlobalHLC)); err != nil {
			return err
		}
		for _, k := range payload.ActivatedRouteKeys {
			if k == "" {
				continue
			}
			if err := tx.Bucket([]byte(bucketRequestsByRoute)).Put([]byte(k), []byte(payload.RequestID)); err != nil {
				return err
			}
		}
	case domain.EventTypeRequestPromoted, domain.EventTypeRequestAnswered:
		var payload domain.RequestPromotedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			var answered domain.RequestAnsweredPayload
			if err2 := json.Unmarshal(evt.Payload, &answered); err2 != nil {
				return err
			}
			payload.RequestID = answered.RequestID
			payload.RevokedRouteKeys = answered.RevokedRouteKeys
		}
		if err := tx.Bucket([]byte(bucketOpenRequests)).Delete([]byte(payload.RequestID)); err != nil {
			return err
		}
		for _, k := range payload.RevokedRouteKeys {
			if err := tx.Bucket([]byte(bucketRequestsByRoute)).Delete([]byte(k)); err != nil {
				return err
			}
		}
	case domain.EventTypeTaskPromotedAndBound:
		var payload domain.TaskPromotedAndBoundPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketActiveTasks)).Put([]byte(payload.TaskID), []byte(payload.WorkflowID)); err != nil {
			return err
		}
		taskBucket := tx.Bucket([]byte(bucketTasksByRoute))
		for _, k := range payload.ActivatedRouteKeys {
			if k == "" {
				continue
			}
			if err := taskBucket.Put([]byte(k), []byte(payload.TaskID)); err != nil {
				return err
			}
		}
	case domain.EventTypeStepExecutionCancelled:
		if err := tx.Bucket([]byte(bucketActiveTasks)).Delete([]byte(evt.AggregateID)); err != nil {
			return err
		}
		return removeTaskRouteKeys(tx, evt.AggregateID)
	case domain.EventTypeOutboxQueued:
		var payload domain.OutboxQueuedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		value, err := json.Marshal(PendingOutboxIndexRecord{
			ActionID:        payload.ActionID,
			TaskID:          evt.AggregateID,
			Domain:          payload.Domain,
			ActionType:      payload.ActionType,
			TargetRef:       payload.TargetRef,
			IdempotencyKey:  payload.IdempotencyKey,
			RemoteRequestID: "",
			PayloadRef:      payload.PayloadRef,
			AttemptCount:    0,
			NextAttemptAt:   evt.ProducedAt,
		})
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketPendingOutbox)).Put([]byte(payload.ActionID), value); err != nil {
			return err
		}
		if payload.IdempotencyKey != "" {
			if err := tx.Bucket([]byte(bucketDedupeWindow)).Put([]byte(payload.IdempotencyKey), []byte(evt.GlobalHLC)); err != nil {
				return err
			}
		}
	case domain.EventTypeOutboxReceiptRecorded:
		var payload domain.OutboxReceiptRecordedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		b := tx.Bucket([]byte(bucketPendingOutbox))
		v := b.Get([]byte(payload.ActionID))
		if len(v) == 0 {
			return nil
		}
		var record PendingOutboxIndexRecord
		if err := json.Unmarshal(v, &record); err != nil {
			return err
		}
		if payload.RemoteRequestID != "" {
			record.RemoteRequestID = payload.RemoteRequestID
			if err := tx.Bucket([]byte(bucketOutboxByRemote)).Put([]byte(payload.RemoteRequestID), []byte(record.ActionID)); err != nil {
				return err
			}
		}
		if payload.ReceiptStatus == "completed" || payload.ReceiptStatus == "succeeded" || payload.ReceiptStatus == "dead" || payload.ReceiptStatus == "failed" {
			if record.RemoteRequestID != "" {
				_ = tx.Bucket([]byte(bucketOutboxByRemote)).Delete([]byte(record.RemoteRequestID))
			}
			return b.Delete([]byte(payload.ActionID))
		}
		record.AttemptCount++
		record.NextAttemptAt = evt.ProducedAt.Add(30 * time.Second)
		if payload.ReceiptStatus == "retry_wait" {
			record.NextAttemptAt = evt.ProducedAt.Add(time.Minute)
		}
		updated, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return b.Put([]byte(payload.ActionID), updated)
	case domain.EventTypeScheduledTaskRegistered:
		var payload domain.ScheduledTaskRegisteredPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		value, err := json.Marshal(ScheduleSourceIndexRecord{
			ScheduledTaskID:      payload.ScheduledTaskID,
			SpecKind:             payload.SpecKind,
			SpecText:             payload.SpecText,
			Timezone:             payload.Timezone,
			ScheduleRevision:     payload.ScheduleRevision,
			TargetWorkflowID:     payload.TargetWorkflowID,
			TargetWorkflowSource: payload.TargetWorkflowSource,
			TargetWorkflowRev:    payload.TargetWorkflowRev,
			Enabled:              payload.Enabled,
			NextFireAt:           payload.NextFireAt,
			LastFireAt:           time.Time{},
		})
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketScheduleSources)).Put([]byte(payload.ScheduledTaskID), value)
	case domain.EventTypeScheduleTriggered:
		var payload domain.ScheduleTriggeredPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		if payload.FireID != "" {
			if err := tx.Bucket([]byte(bucketDedupeWindow)).Put([]byte(payload.FireID), []byte(evt.GlobalHLC)); err != nil {
				return err
			}
		}
		b := tx.Bucket([]byte(bucketScheduleSources))
		v := b.Get([]byte(payload.ScheduledTaskID))
		if len(v) == 0 {
			return nil
		}
		var source ScheduleSourceIndexRecord
		if err := json.Unmarshal(v, &source); err != nil {
			return err
		}
		source.LastFireAt = payload.ScheduledForWindow.UTC()
		next, err := domain.NextCronFire(source.SpecText, source.Timezone, payload.ScheduledForWindow.UTC())
		if err != nil {
			return err
		}
		source.NextFireAt = next
		updated, err := json.Marshal(source)
		if err != nil {
			return err
		}
		return b.Put([]byte(payload.ScheduledTaskID), updated)
	}
	return nil
}

func removeTaskRouteKeys(tx *bolt.Tx, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	b := tx.Bucket([]byte(bucketTasksByRoute))
	if b == nil {
		return nil
	}
	keys := make([][]byte, 0, 8)
	if err := b.ForEach(func(k, v []byte) error {
		if string(v) == taskID {
			keys = append(keys, append([]byte(nil), k...))
		}
		return nil
	}); err != nil {
		return err
	}
	for _, key := range keys {
		if err := b.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func applyLaggingEvent(tx *bolt.Tx, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeApprovalRequestOpened:
		var payload domain.ApprovalRequestOpenedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		v, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketApprovalQueue)).Put([]byte(payload.ApprovalRequestID), v); err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Put([]byte("approval:"+payload.ApprovalRequestID), v)
	case domain.EventTypeApprovalRequestResolved:
		var payload domain.ApprovalRequestResolvedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketApprovalQueue)).Delete([]byte(payload.ApprovalRequestID)); err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Delete([]byte("approval:" + payload.ApprovalRequestID))
	case domain.EventTypeHumanWaitRecorded:
		var payload domain.HumanWaitRecordedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		v, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Put([]byte("wait:"+payload.HumanWaitID), v)
	case domain.EventTypeHumanWaitResolved:
		var payload domain.HumanWaitResolvedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Delete([]byte("wait:" + payload.HumanWaitID))
	}
	return nil
}
