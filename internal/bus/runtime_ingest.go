package bus

import (
	"context"
	"errors"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

// IngestExternalEvent processes an external event.
func (r *Runtime) IngestExternalEvent(ctx context.Context, evt domain.ExternalEvent, reception Reception) (*ProcessResult, error) {
	start := r.clock.Now()
	now := start
	if evt.EventID == "" {
		evt.EventID = r.idgen.New(domain.IDPrefixEvent)
	}
	if evt.ReceivedAt.IsZero() {
		evt.ReceivedAt = now
	}
	if evt.SourceKind == "" {
		evt.SourceKind = "unknown"
	}
	if evt.TransportKind == "" {
		evt.TransportKind = "unknown"
	}

	r.logger.Info("event_received",
		"event_id", evt.EventID,
		"trace_id", evt.TraceID,
		"source_kind", evt.SourceKind,
		"transport_kind", evt.TransportKind,
		"source_ref", truncate(evt.SourceRef, 100),
	)

	if evt.IdempotencyKey != "" {
		record, seen, err := r.store.Indexes.GetDedupeRecord(ctx, evt.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if seen {
			return r.buildDedupeResult(evt, record), nil
		}
	}

	routeTarget, routeKey, err := r.resolveRoute(ctx, evt)
	if err != nil {
		r.logger.Error("route_failed", "event_id", evt.EventID, "error", err.Error())
		return nil, err
	}

	r.logger.Debug("route_resolved",
		"event_id", evt.EventID,
		"route_key", routeKey,
		"route_target_kind", routeTarget.Kind,
		"route_target_id", routeTarget.ID,
	)

	// Existing task hit must not fork a new ephemeral request.
	if routeTarget.Kind == domain.RouteTargetTask {
		return r.handleTaskRoute(ctx, routeTarget.ID, routeKey, evt)
	}

	return r.handleRequestRoute(ctx, evt, reception, routeTarget, routeKey, start)
}

func (r *Runtime) buildDedupeResult(evt domain.ExternalEvent, record store.DedupeRecord) *ProcessResult {
	result := &ProcessResult{
		RequestID:       strings.TrimSpace(record.RequestID),
		TaskID:          strings.TrimSpace(record.TaskID),
		RouteMatched:    "dedupe",
		EventID:         strings.TrimSpace(record.EventID),
		CommitHLC:       strings.TrimSpace(record.CommitHLC),
		RouteTargetKind: strings.TrimSpace(record.RouteTargetKind),
		RouteTargetID:   strings.TrimSpace(record.RouteTargetID),
	}
	if result.TaskID == "" {
		result.TaskID = strings.TrimSpace(evt.TaskID)
	}
	if result.RequestID == "" {
		result.RequestID = strings.TrimSpace(evt.RequestID)
	}
	if result.RouteTargetKind == "" && result.TaskID != "" {
		result.RouteTargetKind = string(domain.RouteTargetTask)
		result.RouteTargetID = result.TaskID
	} else if result.RouteTargetKind == "" && result.RequestID != "" {
		result.RouteTargetKind = string(domain.RouteTargetRequest)
		result.RouteTargetID = result.RequestID
	}
	if result.EventID == "" {
		result.EventID = evt.EventID
	}
	return result
}

func (r *Runtime) handleRequestRoute(ctx context.Context, evt domain.ExternalEvent, reception Reception, routeTarget domain.RouteTarget, routeKey string, start time.Time) (*ProcessResult, error) {
	requestID := evt.RequestID
	createdRequest := false
	if requestID == "" && routeTarget.Kind == domain.RouteTargetRequest {
		requestID = routeTarget.ID
	}
	if requestID == "" {
		requestID = r.idgen.New(domain.IDPrefixRequest)
		createdRequest = true
	}

	batch := make([]domain.EventEnvelope, 0, 4)
	var err error
	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeExternalEventIngested, domain.ExternalEventIngestedPayload{Event: evt})
	if err != nil {
		return nil, err
	}
	if isScheduleTriggerEvent(evt) {
		batch, err = r.appendScheduleTriggeredEvent(ctx, batch, evt)
		if err != nil && !errors.Is(err, ErrScheduleSourceNotFound) {
			return nil, err
		}
	}

	activatedRouteKeys := r.deriveRequestRouteKeys(evt)
	if createdRequest {
		r.routeByReq[requestID] = activatedRouteKeys
		batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeEphemeralRequestOpened, domain.EphemeralRequestOpenedPayload{
			RequestID:          requestID,
			OpenedByEventID:    evt.EventID,
			RouteSnapshotRef:   "route_snapshot:" + requestID,
			ActivatedRouteKeys: activatedRouteKeys,
			ExpiresAt:          r.clock.Now().Add(24 * time.Hour),
		})
		if err != nil {
			return nil, err
		}
		r.logger.Info("request_opened",
			"request_id", requestID,
			"event_id", evt.EventID,
			"trace_id", evt.TraceID,
			"expires_at", r.clock.Now().Add(24*time.Hour),
		)
	}

	result := &ProcessResult{RequestID: requestID, RouteMatched: routeKey}
	result.EventID = evt.EventID
	result.RouteTargetKind = string(domain.RouteTargetRequest)
	result.RouteTargetID = requestID
	if routeTarget.Found() {
		result.RouteTargetKind = string(routeTarget.Kind)
		result.RouteTargetID = routeTarget.ID
	}

	// Simplified path for direct answers or schedule triggers
	if reception == nil && !isScheduleTriggerEvent(evt) {
		return r.commitDirect(ctx, batch, evt, result)
	}

	return r.processWithReception(ctx, evt, reception, batch, result, requestID, start)
}

func (r *Runtime) commitDirect(ctx context.Context, batch []domain.EventEnvelope, evt domain.ExternalEvent, result *ProcessResult) (*ProcessResult, error) {
	applyBatchCausation(batch, evt.CausationID)
	if err := r.commitBatch(ctx, batch); err != nil {
		return nil, err
	}
	result.CommitHLC = batchCommitHLC(batch)
	if err := r.persistDedupeRecord(ctx, evt, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runtime) processWithReception(ctx context.Context, evt domain.ExternalEvent, reception Reception, batch []domain.EventEnvelope, result *ProcessResult, requestID string, start time.Time) (*ProcessResult, error) {
	var decision *domain.PromotionDecision
	var err error

	if isScheduleTriggerEvent(evt) {
		decision, err = r.schedulerPromotionDecision(ctx, requestID, evt)
		if err != nil {
			return r.handleScheduleRecovery(ctx, batch, evt, result, err)
		}
	} else {
		decision, err = reception.Assess(ctx, domain.ReceptionInput{RequestID: requestID, Event: evt})
		if err != nil {
			return nil, err
		}
	}

	return r.executePromotion(ctx, evt, batch, result, decision, requestID, start)
}

func (r *Runtime) executePromotion(ctx context.Context, evt domain.ExternalEvent, batch []domain.EventEnvelope, result *ProcessResult, decision *domain.PromotionDecision, requestID string, start time.Time) (*ProcessResult, error) {
	// Simplified promotion execution
	applyBatchCausation(batch, evt.CausationID)
	if err := r.commitBatch(ctx, batch); err != nil {
		return nil, err
	}
	result.CommitHLC = batchCommitHLC(batch)
	if err := r.persistDedupeRecord(ctx, evt, result); err != nil {
		return nil, err
	}
	r.logger.Info("request_processed",
		"request_id", requestID,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return result, nil
}

func (r *Runtime) handleScheduleRecovery(ctx context.Context, batch []domain.EventEnvelope, evt domain.ExternalEvent, result *ProcessResult, err error) (*ProcessResult, error) {
	batch, appendErr := r.appendScheduleRecoveryEvents(batch, evt, err)
	if appendErr != nil {
		return nil, appendErr
	}
	applyBatchCausation(batch, evt.CausationID)
	if commitErr := r.commitBatch(ctx, batch); commitErr != nil {
		return nil, commitErr
	}
	result.RouteMatched = "schedule_recovery"
	result.CommitHLC = batchCommitHLC(batch)
	if persistErr := r.persistDedupeRecord(ctx, evt, result); persistErr != nil {
		return nil, persistErr
	}
	return result, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
