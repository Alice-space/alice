package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alice/internal/domain"
)

// RequireEnabledScheduleSource checks if a schedule source exists and is enabled.
func (r *Runtime) RequireEnabledScheduleSource(ctx context.Context, scheduledTaskID string) (domain.ScheduleSourceView, error) {
	source, ok, err := r.store.Indexes.GetScheduleSource(ctx, scheduledTaskID)
	if err != nil {
		return domain.ScheduleSourceView{}, err
	}
	if !ok {
		return domain.ScheduleSourceView{}, fmt.Errorf("%w: %s", ErrScheduleSourceNotFound, scheduledTaskID)
	}
	if !source.Enabled {
		return domain.ScheduleSourceView{}, fmt.Errorf("%w: %s", ErrScheduleSourceDisabled, scheduledTaskID)
	}
	return source, nil
}

// RecordScheduleFire records a schedule fire by ingesting a scheduler-trigger event.
func (r *Runtime) RecordScheduleFire(ctx context.Context, cmd domain.RecordScheduleFireCommand) (domain.ScheduleFireView, error) {
	window := cmd.ScheduledForWindowUTC.UTC()
	if window.IsZero() {
		window = r.clock.Now().UTC().Truncate(time.Minute)
	}
	source, err := r.RequireEnabledScheduleSource(ctx, cmd.ScheduledTaskID)
	if err != nil {
		return domain.ScheduleFireView{}, err
	}
	fireID := domain.ComputeFireID(cmd.ScheduledTaskID, window)
	seen, err := r.store.Indexes.DedupeSeen(ctx, fireID)
	if err != nil {
		return domain.ScheduleFireView{}, err
	}
	if seen {
		return domain.ScheduleFireView{FireID: fireID, ScheduledTaskID: cmd.ScheduledTaskID, ScheduledForWindow: window}, nil
	}

	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeScheduleTriggered,
		SourceKind:      "scheduler",
		TransportKind:   "scheduler",
		SourceRef:       window.Format(time.RFC3339),
		ScheduledTaskID: cmd.ScheduledTaskID,
		IdempotencyKey:  fireID,
		Verified:        true,
		ReceivedAt:      r.clock.Now(),
	}
	if _, err := r.IngestExternalEvent(ctx, evt, nil); err != nil {
		return domain.ScheduleFireView{}, err
	}

	_ = source // retained for validation side-effect and future provenance extension.
	return domain.ScheduleFireView{
		FireID:             fireID,
		ScheduledTaskID:    cmd.ScheduledTaskID,
		ScheduledForWindow: window,
	}, nil
}

// RecordAdminAudit records an admin audit event.
func (r *Runtime) RecordAdminAudit(ctx context.Context, payload domain.AdminAuditRecordedPayload) error {
	payload.AdminActionID = strings.TrimSpace(payload.AdminActionID)
	if payload.AdminActionID == "" {
		return fmt.Errorf("admin_action_id is required")
	}
	payload.Operation = strings.TrimSpace(payload.Operation)
	if payload.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	if payload.RecordedAt.IsZero() {
		payload.RecordedAt = r.clock.Now().UTC()
	}
	evt, err := r.newEnvelope(domain.AggregateKindOther, "admin:"+payload.AdminActionID, domain.EventTypeAdminAuditRecorded, payload)
	if err != nil {
		return err
	}
	batch := []domain.EventEnvelope{evt}
	applyBatchCausation(batch, payload.AdminActionID)
	return r.commitBatch(ctx, batch)
}

// RecordOutboxReceipt records an outbox receipt.
func (r *Runtime) RecordOutboxReceipt(ctx context.Context, payload domain.OutboxReceiptRecordedPayload) error {
	taskID := payload.TaskID
	if taskID == "" {
		record, ok, err := r.store.Indexes.FindPendingOutboxByRemote(ctx, payload.ActionID, payload.RemoteRequestID, "")
		if err != nil {
			return err
		}
		if ok {
			taskID = record.TaskID
		}
	}
	if taskID == "" {
		return fmt.Errorf("task id is required for outbox receipt action=%s", payload.ActionID)
	}
	payload.TaskID = taskID
	evt, err := r.newEnvelope(domain.AggregateKindTask, taskID, domain.EventTypeOutboxReceiptRecorded, payload)
	if err != nil {
		return err
	}
	return r.commitBatch(ctx, []domain.EventEnvelope{evt})
}

// RestoreStateFromLog replays the event log to restore runtime state.
func (r *Runtime) RestoreStateFromLog(ctx context.Context) error {
	r.mu.Lock()
	r.seqByAgg = map[string]uint64{}
	r.hlcCounter = 0
	r.routeByReq = map[string][]string{}
	r.mu.Unlock()

	return r.store.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		r.mu.Lock()
		defer r.mu.Unlock()

		key := evt.AggregateKind + ":" + evt.AggregateID
		if evt.Sequence > r.seqByAgg[key] {
			r.seqByAgg[key] = evt.Sequence
		}
		if n := parseHLCCounter(evt.GlobalHLC); n > r.hlcCounter {
			r.hlcCounter = n
		}

		switch evt.EventType {
		case domain.EventTypeEphemeralRequestOpened:
			var payload domain.EphemeralRequestOpenedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err == nil {
				r.routeByReq[payload.RequestID] = append([]string(nil), payload.ActivatedRouteKeys...)
			}
		case domain.EventTypeRequestPromoted:
			var payload domain.RequestPromotedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err == nil {
				delete(r.routeByReq, payload.RequestID)
			}
		case domain.EventTypeRequestAnswered:
			var payload domain.RequestAnsweredPayload
			if err := json.Unmarshal(evt.Payload, &payload); err == nil {
				delete(r.routeByReq, payload.RequestID)
			}
		}
		return nil
	})
}

// PromoteAndBindWorkflow promotes a request and binds it to a workflow atomically.
func (r *Runtime) PromoteAndBindWorkflow(ctx context.Context, cmd domain.PromoteAndBindWorkflowCommand) error {
	batch, err := r.buildPromoteAndBindBatch(cmd)
	if err != nil {
		return err
	}
	return r.commitBatch(ctx, batch)
}

// schedulerPromotionDecision creates a promotion decision for schedule triggers.
func (r *Runtime) schedulerPromotionDecision(ctx context.Context, requestID string, evt domain.ExternalEvent) (*domain.PromotionDecision, error) {
	source, ok, err := r.store.Indexes.GetScheduleSource(ctx, evt.ScheduledTaskID)
	if err != nil {
		return nil, err
	}
	if !ok || !source.Enabled || source.TargetWorkflowID == "" || source.TargetWorkflowRev == "" {
		return nil, fmt.Errorf("invalid schedule source for trigger: %s", evt.ScheduledTaskID)
	}
	return &domain.PromotionDecision{
		DecisionID:          r.idgen.New(domain.IDPrefixDecision),
		RequestID:           requestID,
		IntentKind:          "schedule_trigger",
		RiskLevel:           "medium",
		ExternalWrite:       false,
		Async:               true,
		MultiStep:           true,
		ProposedWorkflowIDs: []string{source.TargetWorkflowID},
		Result:              domain.PromotionResultPromote,
		ReasonCodes:         []string{"schedule_trigger"},
		Confidence:          1.0,
		ProducedBy:          "scheduler",
		ProducedAt:          r.clock.Now(),
	}, nil
}
