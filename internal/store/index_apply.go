package store

import (
	"encoding/json"
	"strings"
	"time"

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
	case domain.EventTypeStepExecutionCancelled:
		return s.applyStepExecutionCancelled(tx, evt)
	case domain.EventTypeOutboxQueued:
		return s.applyOutboxQueued(tx, evt)
	case domain.EventTypeOutboxReceiptRecorded:
		return s.applyOutboxReceipt(tx, evt)
	case domain.EventTypeScheduledTaskRegistered:
		return s.applyScheduleRegistered(tx, evt)
	case domain.EventTypeScheduleTriggered:
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
	if err := b.Put([]byte(payload.RequestID), val); err != nil {
		return err
	}
	routeBucket := tx.Bucket([]byte(bucketRequestsByRoute))
	for _, key := range payload.ActivatedRouteKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := routeBucket.Put([]byte(key), []byte(payload.RequestID)); err != nil {
			return err
		}
	}
	return nil
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
	if err := s.removeRequestRouteKeys(tx, payload.RequestID, payload.RevokedRouteKeys); err != nil {
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
	if err := b.Delete([]byte(payload.RequestID)); err != nil {
		return err
	}
	return s.removeRequestRouteKeys(tx, payload.RequestID, payload.RevokedRouteKeys)
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
	if err := b.Put([]byte(payload.TaskID), val); err != nil {
		return err
	}
	routeBucket := tx.Bucket([]byte(bucketTasksByRoute))
	for _, key := range payload.ActivatedRouteKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := routeBucket.Put([]byte(key), []byte(payload.TaskID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *BoltIndexStore) applyStepExecutionCancelled(tx *bolt.Tx, evt domain.EventEnvelope) error {
	if err := tx.Bucket([]byte(bucketActiveTasks)).Delete([]byte(strings.TrimSpace(evt.AggregateID))); err != nil {
		return err
	}
	return s.removeTaskRouteKeys(tx, evt.AggregateID)
}

func (s *BoltIndexStore) applyOutboxQueued(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.OutboxQueuedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketPendingOutbox))
	taskID := strings.TrimSpace(evt.AggregateID)
	key := strings.TrimSpace(payload.ActionID)
	if key == "" {
		return nil
	}
	val, _ := json.Marshal(PendingOutboxIndexRecord{
		ActionID:       payload.ActionID,
		TaskID:         taskID,
		Domain:         payload.Domain,
		ActionType:     payload.ActionType,
		TargetRef:      payload.TargetRef,
		IdempotencyKey: payload.IdempotencyKey,
		PayloadRef:     payload.PayloadRef,
		NextAttemptAt:  evt.ProducedAt,
	})
	if err := b.Put([]byte(key), val); err != nil {
		return err
	}
	if payload.IdempotencyKey != "" {
		record := DedupeRecord{
			CommitHLC: evt.GlobalHLC,
			EventID:   evt.EventID,
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketDedupeWindow)).Put([]byte(payload.IdempotencyKey), raw); err != nil {
			return err
		}
	}
	return nil
}

func (s *BoltIndexStore) applyOutboxReceipt(tx *bolt.Tx, evt domain.EventEnvelope) error {
	var payload domain.OutboxReceiptRecordedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	b := tx.Bucket([]byte(bucketPendingOutbox))
	actionID := strings.TrimSpace(payload.ActionID)
	if actionID == "" {
		return nil
	}
	v := b.Get([]byte(actionID))
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
	status := strings.ToLower(strings.TrimSpace(payload.ReceiptStatus))
	if status == "completed" || status == "succeeded" || status == "dead" || status == "failed" {
		if record.RemoteRequestID != "" {
			_ = tx.Bucket([]byte(bucketOutboxByRemote)).Delete([]byte(record.RemoteRequestID))
		}
		return b.Delete([]byte(actionID))
	}
	record.AttemptCount++
	record.NextAttemptAt = evt.ProducedAt.Add(30 * time.Second)
	if status == "retry_wait" {
		record.NextAttemptAt = evt.ProducedAt.Add(time.Minute)
	}
	updated, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return b.Put([]byte(actionID), updated)
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
	record.LastFireAt = payload.ScheduledForWindow.UTC()
	next, err := domain.NextCronFire(record.SpecText, record.Timezone, record.LastFireAt)
	if err == nil {
		record.NextFireAt = next
	}
	val, _ := json.Marshal(record)
	if err := b.Put([]byte(payload.ScheduledTaskID), val); err != nil {
		return err
	}
	if payload.FireID != "" {
		record := DedupeRecord{
			CommitHLC: evt.GlobalHLC,
			EventID:   evt.EventID,
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketDedupeWindow)).Put([]byte(payload.FireID), raw); err != nil {
			return err
		}
	}
	return nil
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
		CommitHLC:  evt.GlobalHLC,
		EventID:    payload.Event.EventID,
		RequestID:  strings.TrimSpace(payload.Event.RequestID),
		TaskID:     strings.TrimSpace(payload.Event.TaskID),
		ReceivedAt: payload.Event.ReceivedAt,
	}
	val, _ := json.Marshal(record)
	return b.Put([]byte(payload.Event.IdempotencyKey), val)
}

// applyEventLagging applies lagging projection updates
func (s *BoltIndexStore) applyEventLagging(tx *bolt.Tx, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeApprovalRequestOpened:
		var payload domain.ApprovalRequestOpenedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketApprovalQueue)).Put([]byte(payload.ApprovalRequestID), raw); err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Put([]byte("approval:"+payload.ApprovalRequestID), raw)
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
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Put([]byte("wait:"+payload.HumanWaitID), raw)
	case domain.EventTypeHumanWaitResolved:
		var payload domain.HumanWaitResolvedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketHumanQueue)).Delete([]byte("wait:" + payload.HumanWaitID))
	}
	return nil
}

func (s *BoltIndexStore) removeTaskRouteKeys(tx *bolt.Tx, taskID string) error {
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

func (s *BoltIndexStore) removeRequestRouteKeys(tx *bolt.Tx, requestID string, revoked []string) error {
	requestID = strings.TrimSpace(requestID)
	b := tx.Bucket([]byte(bucketRequestsByRoute))
	if b == nil {
		return nil
	}
	if len(revoked) > 0 {
		for _, key := range revoked {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if err := b.Delete([]byte(key)); err != nil {
				return err
			}
		}
		return nil
	}
	keys := make([][]byte, 0, 8)
	if err := b.ForEach(func(k, v []byte) error {
		if string(v) == requestID {
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
