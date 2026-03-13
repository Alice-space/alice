package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
	"alice/internal/workflow"
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
	err := r.store.AppendBatch(ctx, batch)
	if err != nil && errors.Is(err, store.ErrCriticalIndexApply) && r.onCritical != nil {
		r.onCritical(err)
	}
	return err
}

// batchCommitHLC returns the commit HLC for a batch.
func batchCommitHLC(batch []domain.EventEnvelope) string {
	if len(batch) == 0 {
		return ""
	}
	return strings.TrimSpace(batch[len(batch)-1].GlobalHLC)
}

// applyBatchCausation applies causation IDs to a batch.
func applyBatchCausation(batch []domain.EventEnvelope, causationID string) {
	causationID = strings.TrimSpace(causationID)
	if causationID == "" {
		return
	}
	for i := range batch {
		if strings.TrimSpace(batch[i].CausationID) == "" {
			batch[i].CausationID = causationID
		}
		if strings.TrimSpace(batch[i].CorrelationID) == "" {
			batch[i].CorrelationID = causationID
		}
	}
}

// persistDedupeRecord persists a deduplication record.
func (r *Runtime) persistDedupeRecord(ctx context.Context, evt domain.ExternalEvent, result *ProcessResult) error {
	if strings.TrimSpace(evt.IdempotencyKey) == "" || result == nil {
		return nil
	}
	record := store.DedupeRecord{
		CommitHLC:       strings.TrimSpace(result.CommitHLC),
		EventID:         strings.TrimSpace(result.EventID),
		RequestID:       strings.TrimSpace(result.RequestID),
		TaskID:          strings.TrimSpace(result.TaskID),
		RouteTargetKind: strings.TrimSpace(result.RouteTargetKind),
		RouteTargetID:   strings.TrimSpace(result.RouteTargetID),
		ReceivedAt:      r.clock.Now(),
	}
	if record.RouteTargetKind == "" {
		if record.TaskID != "" {
			record.RouteTargetKind = string(domain.RouteTargetTask)
			record.RouteTargetID = record.TaskID
		} else if record.RequestID != "" {
			record.RouteTargetKind = string(domain.RouteTargetRequest)
			record.RouteTargetID = record.RequestID
		}
	}
	return r.store.Indexes.PutDedupeRecord(ctx, evt.IdempotencyKey, record)
}

// buildPromoteAndBindBatch builds the atomic request->task promotion batch.
func (r *Runtime) buildPromoteAndBindBatch(cmd domain.PromoteAndBindWorkflowCommand) ([]domain.EventEnvelope, error) {
	if err := domain.ValidatePromoteAndBindCommand(cmd); err != nil {
		return nil, err
	}
	reqRoutes := append([]string(nil), r.routeByReq[cmd.RequestID]...)
	if len(reqRoutes) == 0 {
		reqRoutes = []string{cmd.RouteSnapshotRef}
	}
	events := make([]domain.EventEnvelope, 0, 3)
	var err error
	events, err = r.appendEvent(events, domain.AggregateKindRequest, cmd.RequestID, domain.EventTypeRequestPromoted, domain.RequestPromotedPayload{
		RequestID:        cmd.RequestID,
		TaskID:           cmd.TaskID,
		RouteSnapshotRef: cmd.RouteSnapshotRef,
		RevokedRouteKeys: reqRoutes,
		PromotedAt:       cmd.At,
	})
	if err != nil {
		return nil, err
	}
	events, err = r.appendEvent(events, domain.AggregateKindTask, cmd.TaskID, domain.EventTypeTaskPromotedAndBound, domain.TaskPromotedAndBoundPayload{
		RequestID:          cmd.RequestID,
		TaskID:             cmd.TaskID,
		BindingID:          cmd.BindingID,
		WorkflowID:         cmd.WorkflowID,
		WorkflowSource:     cmd.WorkflowSource,
		WorkflowRev:        cmd.WorkflowRev,
		ManifestDigest:     cmd.ManifestDigest,
		EntryStepID:        cmd.EntryStepID,
		ActivatedRouteKeys: cmd.ActivatedRouteKey,
		RouteSnapshotRef:   cmd.RouteSnapshotRef,
		PromotedAt:         cmd.At,
	})
	if err != nil {
		return nil, err
	}
	events, err = r.appendEvent(events, domain.AggregateKindTask, cmd.TaskID, domain.EventTypeStepExecutionStarted, domain.StepExecutionStartedPayload{
		ExecutionID:      r.idgen.New(domain.IDPrefixExecution),
		TaskID:           cmd.TaskID,
		BindingID:        cmd.BindingID,
		StepID:           cmd.EntryStepID,
		Attempt:          1,
		LeaseOwner:       "runtime",
		LeaseExpiresAt:   cmd.At.Add(2 * time.Minute),
		InputArtifactIDs: nil,
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Runtime) entryStepID(ref workflow.ManifestRef) string {
	m, err := r.workflow.Registry().Load(context.Background(), ref.WorkflowID, ref.WorkflowRev)
	if err != nil || len(m.Steps) == 0 {
		return "entry"
	}
	return m.Steps[0].ID
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
	batch := make([]domain.EventEnvelope, 0, 4)
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

	result := &ProcessResult{
		RequestID:       evt.RequestID,
		TaskID:          taskID,
		RouteMatched:    routeKey,
		RouteTargetKind: string(domain.RouteTargetTask),
		RouteTargetID:   taskID,
		EventID:         evt.EventID,
		CommitHLC:       batchCommitHLC(batch),
	}
	if err := r.persistDedupeRecord(ctx, evt, result); err != nil {
		return nil, err
	}
	return result, nil
}

// applyHumanAction applies human action events.
func (r *Runtime) applyHumanAction(ctx context.Context, batch []domain.EventEnvelope, taskID string, evt domain.ExternalEvent) ([]domain.EventEnvelope, error) {
	now := r.clock.Now().UTC()
	kind := domain.NormalizeHumanActionKind(evt.ActionKind)
	if kind == "" {
		return nil, fmt.Errorf("human action missing action_kind")
	}
	actor := evt.ActorRef
	if actor == "" {
		actor = "human_action"
	}

	switch kind {
	case domain.HumanActionApprove, domain.HumanActionConfirm, domain.HumanActionResumeBudget, domain.HumanActionReject:
		approval, ok, err := r.store.Indexes.GetApprovalRequest(ctx, evt.ApprovalRequestID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("approval request is not active: %s", evt.ApprovalRequestID)
		}
		if approval.TaskID != taskID {
			return nil, fmt.Errorf("approval request task mismatch: approval_task=%s route_task=%s", approval.TaskID, taskID)
		}
		if evt.StepExecutionID != "" && approval.StepExecutionID != "" && evt.StepExecutionID != approval.StepExecutionID {
			return nil, fmt.Errorf("step execution mismatch for approval action")
		}
		if !isGateDecisionAllowed(kind, approval.GateType) {
			return nil, fmt.Errorf("action %s is not allowed for gate_type=%s", kind, approval.GateType)
		}
		batch, err = r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeApprovalRequestResolved, domain.ApprovalRequestResolvedPayload{
			ApprovalRequestID: evt.ApprovalRequestID,
			Resolution:        string(kind),
			ResolvedByActor:   actor,
			ResolutionRef:     evt.PayloadRef,
			ResolvedAt:        now,
		})
		if err != nil {
			return nil, err
		}
		if kind == domain.HumanActionReject {
			executionID := approval.StepExecutionID
			if executionID == "" {
				executionID = evt.StepExecutionID
			}
			if executionID == "" {
				executionID = "cancel:" + taskID
			}
			return r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeStepExecutionCancelled, domain.StepExecutionCancelledPayload{
				ExecutionID: executionID,
				Attempt:     1,
				ReasonCode:  "human_reject",
				CancelledAt: now,
			})
		}
		waitReason := evt.WaitingReason
		if waitReason == "" {
			waitReason = waitingReasonForGate(approval.GateType)
		}
		return r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeTaskResumed, domain.TaskResumedPayload{
			TaskID:          taskID,
			WaitingReason:   waitReason,
			StepExecutionID: approval.StepExecutionID,
			ResumeDecision:  string(kind),
			ResumePointRef:  evt.PayloadRef,
			ResumedAt:       now,
		})

	case domain.HumanActionProvideInput, domain.HumanActionResumeRecovery, domain.HumanActionRewind:
		wait, ok, err := r.store.Indexes.GetHumanWait(ctx, evt.HumanWaitID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("human wait is not active: %s", evt.HumanWaitID)
		}
		if wait.TaskID != taskID {
			return nil, fmt.Errorf("human wait task mismatch: wait_task=%s route_task=%s", wait.TaskID, taskID)
		}
		if evt.StepExecutionID != "" && wait.StepExecutionID != "" && evt.StepExecutionID != wait.StepExecutionID {
			return nil, fmt.Errorf("step execution mismatch for human wait action")
		}
		if !waitAllows(wait.ResumeOptions, kind) {
			return nil, fmt.Errorf("action %s is not allowed by wait options", kind)
		}
		if evt.WaitingReason != "" && !strings.EqualFold(evt.WaitingReason, wait.WaitingReason) {
			return nil, fmt.Errorf("waiting_reason mismatch")
		}
		if kind == domain.HumanActionProvideInput && len(strings.TrimSpace(string(evt.InputPatch))) == 0 {
			return nil, fmt.Errorf("input_patch is required for provide_input")
		}
		if kind != domain.HumanActionRewind && len(strings.TrimSpace(string(evt.InputPatch))) > 0 {
			if _, err := validateHumanWaitPatchedInput(wait, evt.InputPatch); err != nil {
				return nil, err
			}
		}
		batch, err = r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeHumanWaitResolved, domain.HumanWaitResolvedPayload{
			HumanWaitID:     evt.HumanWaitID,
			WaitingReason:   wait.WaitingReason,
			Resolution:      string(kind),
			ResolvedByActor: actor,
			ResolutionRef:   evt.PayloadRef,
			ResolvedAt:      now,
		})
		if err != nil {
			return nil, err
		}
		if kind == domain.HumanActionRewind {
			if evt.TargetStepID == "" {
				return nil, fmt.Errorf("rewind requires target_step_id")
			}
			fromExecutionID := evt.StepExecutionID
			if fromExecutionID == "" {
				fromExecutionID = wait.StepExecutionID
			}
			batch, err = r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeStepExecutionRewound, domain.StepExecutionRewoundPayload{
				TaskID:          taskID,
				FromExecutionID: fromExecutionID,
				ToStepID:        evt.TargetStepID,
				DecisionRef:     evt.PayloadRef,
				RewoundAt:       now,
			})
			if err != nil {
				return nil, err
			}
		}
		return r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeTaskResumed, domain.TaskResumedPayload{
			TaskID:          taskID,
			WaitingReason:   wait.WaitingReason,
			StepExecutionID: wait.StepExecutionID,
			ResumeDecision:  string(kind),
			ResumePointRef:  evt.PayloadRef,
			ResumedAt:       now,
		})

	case domain.HumanActionCancel:
		executionID := evt.StepExecutionID
		if executionID == "" {
			executionID = "cancel:" + taskID
		}
		return r.appendEvent(batch, domain.AggregateKindTask, taskID, domain.EventTypeStepExecutionCancelled, domain.StepExecutionCancelledPayload{
			ExecutionID: executionID,
			Attempt:     1,
			ReasonCode:  "human_cancel",
			CancelledAt: now,
		})

	default:
		return nil, fmt.Errorf("unsupported action_kind=%s", kind)
	}
}

func isGateDecisionAllowed(kind domain.HumanActionKind, gateType string) bool {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case "approval":
		return kind == domain.HumanActionApprove || kind == domain.HumanActionReject
	case "confirmation":
		return kind == domain.HumanActionConfirm || kind == domain.HumanActionReject
	case "budget":
		return kind == domain.HumanActionResumeBudget || kind == domain.HumanActionReject
	case "evaluation":
		return kind == domain.HumanActionConfirm || kind == domain.HumanActionReject
	default:
		return false
	}
}

func waitingReasonForGate(gateType string) string {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case "budget":
		return string(domain.WaitingReasonBudget)
	case "confirmation":
		return string(domain.WaitingReasonConfirmation)
	default:
		return string(domain.WaitingReasonInput)
	}
}

func waitAllows(options []string, kind domain.HumanActionKind) bool {
	if len(options) == 0 {
		return true
	}
	for _, option := range options {
		if domain.NormalizeHumanActionKind(option) == kind {
			return true
		}
	}
	return false
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
func (r *Runtime) appendScheduleRecoveryEvents(batch []domain.EventEnvelope, evt domain.ExternalEvent, cause error) ([]domain.EventEnvelope, error) {
	humanWaitID := r.idgen.New(domain.IDPrefixResult)
	inputDraft, _ := json.Marshal(map[string]any{"reason": cause.Error()})
	var err error
	batch, err = r.appendEvent(batch, domain.AggregateKindTask, evt.ScheduledTaskID, domain.EventTypeTaskWaitingHumanMarked, domain.TaskWaitingHumanMarkedPayload{
		TaskID:          evt.ScheduledTaskID,
		WaitingReason:   string(domain.WaitingReasonRecovery),
		StepExecutionID: "",
		WaitRef:         humanWaitID,
		EnteredAt:       r.clock.Now(),
	})
	if err != nil {
		return nil, err
	}
	batch, err = r.appendEvent(batch, domain.AggregateKindTask, evt.ScheduledTaskID, domain.EventTypeHumanWaitRecorded, domain.HumanWaitRecordedPayload{
		HumanWaitID:     humanWaitID,
		TaskID:          evt.ScheduledTaskID,
		StepExecutionID: "",
		WaitingReason:   string(domain.WaitingReasonRecovery),
		InputSchemaID:   "recovery.schedule_trigger",
		InputDraft:      inputDraft,
		ResumeOptions:   []string{"resume_recovery", "cancel"},
		PromptRef:       "schedule-recovery:" + cause.Error(),
		DeadlineAt:      r.clock.Now().Add(24 * time.Hour),
	})
	if err != nil {
		return nil, err
	}
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

func validateHumanWaitPatchedInput(wait domain.HumanWaitRecordedPayload, patch json.RawMessage) (json.RawMessage, error) {
	return domain.ApplyHumanWaitInputPatch(wait, patch)
}
