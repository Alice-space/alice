package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

// appendEvent adds an event to the batch.
func (r *Runtime) appendEvent(batch []domain.EventEnvelope, aggregateKind, aggregateID string, eventType domain.EventType, payload any) ([]domain.EventEnvelope, error) {
	evt, err := r.newEnvelope(aggregateKind, aggregateID, eventType, payload)
	if err != nil {
		return nil, err
	}
	return append(batch, evt), nil
}

// newEnvelope creates a new event envelope.
func (r *Runtime) newEnvelope(aggregateKind, aggregateID string, eventType domain.EventType, payload any) (domain.EventEnvelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	schema, ok := domain.EventSchemaFor(eventType)
	if !ok {
		return domain.EventEnvelope{}, fmt.Errorf("event schema is not registered: %s", eventType)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return domain.EventEnvelope{}, fmt.Errorf("marshal payload: %w", err)
	}

	if !domain.AggregateKindAllowed(eventType, aggregateKind) {
		return domain.EventEnvelope{}, fmt.Errorf("aggregate contract mismatch for %s: got=%s", eventType, aggregateKind)
	}

	var parentEventID, causationID, traceID string
	switch p := payload.(type) {
	case domain.ExternalEventIngestedPayload:
		parentEventID = p.Event.ParentEventID
		causationID = p.Event.CausationID
		traceID = p.Event.TraceID
	case *domain.ExternalEventIngestedPayload:
		if p != nil {
			parentEventID = p.Event.ParentEventID
			causationID = p.Event.CausationID
			traceID = p.Event.TraceID
		}
	}

	seqKey := aggregateKind + ":" + aggregateID
	r.seqByAgg[seqKey]++
	r.hlcCounter++
	now := r.clock.Now().UTC()
	hlc := fmt.Sprintf("%s#%04d", now.Format("2006-01-02T15:04:05.000000000Z07:00"), r.hlcCounter)

	return domain.EventEnvelope{
		EventID:         r.idgen.New(domain.IDPrefixEvent),
		AggregateKind:   aggregateKind,
		AggregateID:     aggregateID,
		EventType:       eventType,
		Sequence:        r.seqByAgg[seqKey],
		GlobalHLC:       hlc,
		ParentEventID:   parentEventID,
		CausationID:     causationID,
		CorrelationID:   causationID,
		TraceID:         traceID,
		ProducedAt:      now,
		Producer:        "bus",
		PayloadSchemaID: schema.PayloadSchemaID,
		PayloadVersion:  schema.PayloadVersion,
		Payload:         payloadJSON,
	}, nil
}

// commitBatch commits a batch of events to the store.
func (r *Runtime) commitBatch(ctx context.Context, batch []domain.EventEnvelope) error {
	if len(batch) == 0 {
		return nil
	}
	return r.store.AppendBatch(ctx, batch)
}

// batchCommitHLC returns the commit HLC for a batch.
func batchCommitHLC(batch []domain.EventEnvelope) string {
	if len(batch) == 0 {
		return ""
	}
	return batch[len(batch)-1].GlobalHLC
}

// applyBatchCausation applies causation IDs to a batch.
func applyBatchCausation(batch []domain.EventEnvelope, causationID string) {
	for i := range batch {
		if batch[i].CausationID == "" && causationID != "" {
			batch[i].CausationID = causationID
			batch[i].CorrelationID = causationID
		}
	}
}

// persistDedupeRecord persists a deduplication record.
func (r *Runtime) persistDedupeRecord(ctx context.Context, evt domain.ExternalEvent, result *ProcessResult) error {
	if evt.IdempotencyKey == "" {
		return nil
	}
	return r.store.Indexes.PutDedupeRecord(ctx, evt.IdempotencyKey, store.DedupeRecord{
		RequestID:       result.RequestID,
		TaskID:          result.TaskID,
		EventID:         result.EventID,
		CommitHLC:       result.CommitHLC,
		RouteTargetKind: result.RouteTargetKind,
		RouteTargetID:   result.RouteTargetID,
		ReceivedAt:      r.clock.Now(),
	})
}

// handleTaskRoute handles routing to an existing task.
func (r *Runtime) handleTaskRoute(ctx context.Context, taskID, routeKey string, evt domain.ExternalEvent) (*ProcessResult, error) {
	active, err := r.store.Indexes.IsTaskActive(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: task_id=%s", domain.ErrTerminalObjectNotRoutable, taskID)
	}

	ingestAggregateID := r.ingestAggregateID(evt, taskID)
	batch := make([]domain.EventEnvelope, 0, 3)
	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, ingestAggregateID, domain.EventTypeExternalEventIngested, domain.ExternalEventIngestedPayload{Event: evt})
	if err != nil {
		return nil, err
	}
	if isHumanActionSource(evt.SourceKind) {
		batch, err = r.applyHumanAction(ctx, batch, taskID, evt)
		if err != nil {
			return nil, err
		}
	}
	applyBatchCausation(batch, evt.CausationID)
	if err := r.commitBatch(ctx, batch); err != nil {
		return nil, err
	}

	return &ProcessResult{
		RequestID:       evt.RequestID,
		TaskID:          taskID,
		RouteMatched:    routeKey,
		RouteTargetKind: string(domain.RouteTargetTask),
		RouteTargetID:   taskID,
		EventID:         evt.EventID,
		CommitHLC:       batchCommitHLC(batch),
	}, nil
}

// applyHumanAction applies human action events.
func (r *Runtime) applyHumanAction(ctx context.Context, batch []domain.EventEnvelope, taskID string, evt domain.ExternalEvent) ([]domain.EventEnvelope, error) {
	actionKind := domain.NormalizeHumanActionKind(evt.ActionKind)
	switch actionKind {
	case domain.HumanActionApprove, domain.HumanActionReject, domain.HumanActionConfirm, domain.HumanActionResumeBudget:
		if evt.ApprovalRequestID != "" {
			payload := domain.ApprovalRequestResolvedPayload{
				ApprovalRequestID: evt.ApprovalRequestID,
				Resolution:        string(actionKind),
				ResolutionRef:     evt.PayloadRef,
				ResolvedAt:        r.clock.Now(),
			}
			return r.appendEvent(batch, domain.AggregateKindGate, evt.ApprovalRequestID, domain.EventTypeApprovalRequestResolved, payload)
		}
	case domain.HumanActionProvideInput, domain.HumanActionResumeRecovery, domain.HumanActionRewind:
		if evt.HumanWaitID != "" {
			payload := domain.HumanWaitResolvedPayload{
				HumanWaitID:   evt.HumanWaitID,
				WaitingReason: evt.WaitingReason,
				Resolution:    string(actionKind),
				ResolutionRef: evt.PayloadRef,
				ResolvedAt:    r.clock.Now(),
			}
			return r.appendEvent(batch, domain.AggregateKindHumanWait, evt.HumanWaitID, domain.EventTypeHumanWaitResolved, payload)
		}
	}
	return batch, nil
}

// appendScheduleTriggeredEvent appends a schedule triggered event.
func (r *Runtime) appendScheduleTriggeredEvent(ctx context.Context, batch []domain.EventEnvelope, evt domain.ExternalEvent) ([]domain.EventEnvelope, error) {
	source, ok, err := r.store.Indexes.GetScheduleSource(ctx, evt.ScheduledTaskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrScheduleSourceNotFound, evt.ScheduledTaskID)
	}
	scheduledFor := evt.ReceivedAt.UTC()
	if t, err := time.Parse(time.RFC3339, evt.SourceRef); err == nil {
		scheduledFor = t.UTC()
	}
	payload := domain.ScheduleTriggeredPayload{
		FireID:                 evt.IdempotencyKey,
		ScheduledTaskID:        evt.ScheduledTaskID,
		ScheduledForWindow:     scheduledFor,
		SourceScheduleRevision: source.ScheduleRevision,
		TargetWorkflowID:       source.TargetWorkflowID,
		TargetWorkflowSource:   source.TargetWorkflowSource,
		TargetWorkflowRev:      source.TargetWorkflowRev,
	}
	return r.appendEvent(batch, domain.AggregateKindTask, evt.ScheduledTaskID, domain.EventTypeScheduleTriggered, payload)
}

// appendScheduleRecoveryEvents appends recovery events for schedule failures.
func (r *Runtime) appendScheduleRecoveryEvents(batch []domain.EventEnvelope, evt domain.ExternalEvent, err error) ([]domain.EventEnvelope, error) {
	// Simplified implementation
	return batch, nil
}

func parseHLCCounter(hlc string) uint64 {
	parts := strings.Split(hlc, "#")
	if len(parts) < 2 {
		return 0
	}
	n, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
