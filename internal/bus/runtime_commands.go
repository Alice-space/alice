package bus

import (
	"context"
	"encoding/json"
	"fmt"

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

// RecordScheduleFire records a schedule fire event.
func (r *Runtime) RecordScheduleFire(ctx context.Context, cmd domain.RecordScheduleFireCommand) (domain.ScheduleFireView, error) {
	source, ok, err := r.store.Indexes.GetScheduleSource(ctx, cmd.ScheduledTaskID)
	if err != nil {
		return domain.ScheduleFireView{}, err
	}
	if !ok {
		return domain.ScheduleFireView{}, fmt.Errorf("schedule source not found: %s", cmd.ScheduledTaskID)
	}
	fireID := domain.ComputeFireID(cmd.ScheduledTaskID, cmd.ScheduledForWindowUTC)
	seen, err := r.store.Indexes.DedupeSeen(ctx, fireID)
	if err != nil {
		return domain.ScheduleFireView{}, err
	}
	if seen {
		return domain.ScheduleFireView{}, fmt.Errorf("schedule fire already recorded: %s", fireID)
	}

	batch := make([]domain.EventEnvelope, 0, 1)
	payload := domain.ScheduleFirePayload{
		FireID:                 fireID,
		ScheduledTaskID:        cmd.ScheduledTaskID,
		ScheduledForWindow:     cmd.ScheduledForWindowUTC,
		SourceScheduleRevision: source.ScheduleRevision,
	}
	batch, err = r.appendEvent(batch, domain.AggregateKindSchedule, cmd.ScheduledTaskID, domain.EventTypeScheduleFire, payload)
	if err != nil {
		return domain.ScheduleFireView{}, err
	}
	if err := r.commitBatch(ctx, batch); err != nil {
		return domain.ScheduleFireView{}, err
	}

	return domain.ScheduleFireView{
		FireID:             fireID,
		ScheduledTaskID:    cmd.ScheduledTaskID,
		ScheduledForWindow: cmd.ScheduledForWindowUTC,
	}, nil
}

// RecordAdminAudit records an admin audit event.
func (r *Runtime) RecordAdminAudit(ctx context.Context, payload domain.AdminAuditRecordedPayload) error {
	if payload.AdminActionID == "" {
		return fmt.Errorf("admin action id is required")
	}
	evt, err := r.newEnvelope(domain.AggregateKindAdmin, payload.AdminActionID, domain.EventTypeAdminAuditRecorded, payload)
	if err != nil {
		return err
	}
	return r.commitBatch(ctx, []domain.EventEnvelope{evt})
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

// PromoteAndBindWorkflow promotes a request and binds it to a workflow.
func (r *Runtime) PromoteAndBindWorkflow(ctx context.Context, cmd domain.PromoteAndBindWorkflowCommand) error {
	if err := domain.ValidatePromoteAndBindCommand(cmd); err != nil {
		return err
	}

	batch := make([]domain.EventEnvelope, 0, 2)

	// Request promoted event
	reqPayload := domain.RequestPromotedPayload{
		RequestID:        cmd.RequestID,
		TaskID:           cmd.TaskID,
		RouteSnapshotRef: cmd.RouteSnapshotRef,
		PromotedAt:       cmd.At,
	}
	batch, err := r.appendEvent(batch, domain.AggregateKindRequest, cmd.RequestID, domain.EventTypeRequestPromoted, reqPayload)
	if err != nil {
		return err
	}

	// Task promoted and bound event
	taskPayload := domain.TaskPromotedAndBoundPayload{
		RequestID:      cmd.RequestID,
		TaskID:         cmd.TaskID,
		BindingID:      cmd.BindingID,
		WorkflowID:     cmd.WorkflowID,
		WorkflowSource: cmd.WorkflowSource,
		WorkflowRev:    cmd.WorkflowRev,
		ManifestDigest: cmd.ManifestDigest,
		EntryStepID:    cmd.EntryStepID,
		PromotedAt:     cmd.At,
	}
	batch, err = r.appendEvent(batch, domain.AggregateKindTask, cmd.TaskID, domain.EventTypeTaskPromotedAndBound, taskPayload)
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
