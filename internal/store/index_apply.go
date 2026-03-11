package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"alice/internal/domain"
	bolt "go.etcd.io/bbolt"
)

// applyEventCritical applies critical index updates for an event
func (s *BoltIndexStore) applyEventCritical(tx *bolt.Tx, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeEphemeralRequestOpened:
		return s.applyRequestOpened(tx, evt)
	case domain.EventTypeRequestPromoted:
		return s.applyRequestPromoted(tx, evt)
	case domain.EventTypeRequestAnswered:
		return s.applyRequestAnswered(tx, evt)
	case domain.EventTypeTaskPromotedAndBound:
		return s.applyTaskBound(tx, evt)
	case domain.EventTypeOutboxQueued:
		return s.applyOutboxQueued(tx, evt)
	case domain.EventTypeOutboxReceiptRecorded:
		return s.applyOutboxReceipt(tx, evt)
	case domain.EventTypeScheduledTaskRegistered:
		return s.applyScheduleRegistered(tx, evt)
	case domain.EventTypeScheduleFire:
		return s.applyScheduleFire(tx, evt)
	case domain.EventTypeExternalEventIngested:
		return s.applyDedupeCheck(tx, evt)
	}
	return nil
}

func (s *BoltIndexStore) applyRequestOpened(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.EphemeralRequestOpenedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketOpenRequests))
	val, _ := json.Marshal(map[string]any{
		"request_id":     payload.RequestID,
		"opened_at":      evt.ProducedAt,
		"expires_at":     payload.ExpiresAt,
		"route_snapshot": payload.RouteSnapshotRef,
	})
	return b.Put([]byte(payload.RequestID), val)
}

func (s *BoltIndexStore) applyRequestPromoted(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.RequestPromotedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketOpenRequests))
	if err := b.Delete([]byte(payload.RequestID)); err != nil {
		return err
	}
	return nil
}

func (s *BoltIndexStore) applyRequestAnswered(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.RequestAnsweredPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketOpenRequests))
	return b.Delete([]byte(payload.RequestID))
}

func (s *BoltIndexStore) applyTaskBound(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.TaskPromotedAndBoundPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketActiveTasks))
	val, _ := json.Marshal(map[string]any{
		"task_id":           payload.TaskID,
		"request_id":        payload.RequestID,
		"workflow_id":       payload.WorkflowID,
		"bound_at":          evt.ProducedAt,
		"scheduled_task_id": payload.ScheduledTaskID,
	})
	return b.Put([]byte(payload.TaskID), val)
}

func (s *BoltIndexStore) applyOutboxQueued(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.OutboxQueuedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketPendingOutbox))
	taskID := evt.AggregateID
	key := fmt.Sprintf("%s:%s", taskID, payload.ActionID)
	val, _ := json.Marshal(PendingOutboxIndexRecord{
		ActionID:       payload.ActionID,
		TaskID:         taskID,
		Domain:         payload.Domain,
		ActionType:     payload.ActionType,
		TargetRef:      payload.TargetRef,
		IdempotencyKey: payload.IdempotencyKey,
		NextAttemptAt:  payload.DeadlineAt,
	})
	return b.Put([]byte(key), val)
}

func (s *BoltIndexStore) applyOutboxReceipt(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.OutboxReceiptRecordedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketPendingOutbox))
	// Remove from pending
	cursor := b.Cursor()
	prefix := []byte(payload.TaskID + ":")
	for k, _ := cursor.Seek(prefix); k != nil && strings.HasPrefix(string(k), string(prefix)); k, _ = cursor.Next() {
		if strings.Contains(string(k), payload.ActionID) {
			return b.Delete(k)
		}
	}
	return nil
}

func (s *BoltIndexStore) applyScheduleRegistered(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.ScheduledTaskRegisteredPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketScheduleSources))
	val, _ := json.Marshal(ScheduleSourceIndexRecord{
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
	})
	return b.Put([]byte(payload.ScheduledTaskID), val)
}

func (s *BoltIndexStore) applyScheduleFire(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.ScheduleTriggeredPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketScheduleSources))
	data := b.Get([]byte(payload.ScheduledTaskID))
	if data == nil {
		return nil
	}
	var record ScheduleSourceIndexRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	record.LastFireAt = payload.ScheduledForWindow
	val, _ := json.Marshal(record)
	return b.Put([]byte(payload.ScheduledTaskID), val)
}

func (s *BoltIndexStore) applyDedupeCheck(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.ExternalEventIngestedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	if payload.Event.IdempotencyKey == "" {
		return nil
	}
	b := tx.Bucket([]byte(bucketDedupeWindow))
	record := DedupeRecord{
		EventID:    payload.Event.EventID,
		ReceivedAt: payload.Event.ReceivedAt,
	}
	val, _ := json.Marshal(record)
	return b.Put([]byte(payload.Event.IdempotencyKey), val)
}

// applyEventLagging applies lagging projection updates
func (s *BoltIndexStore) applyEventLagging(tx *bolt.Tx, evt domain.EventEnvelope) error {
	// Simplified - full implementation would update approval/human queues
	return nil
}
