package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"alice/internal/agent"
	"alice/internal/domain"
	"alice/internal/store"
	"alice/internal/workflow"
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

	batch := make([]domain.EventEnvelope, 0, 6)
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

	// Direct append path (no reception decision).
	if reception == nil && !isScheduleTriggerEvent(evt) {
		return r.commitDirect(ctx, batch, evt, result)
	}

	return r.processWithReception(ctx, evt, reception, batch, result, requestID, activatedRouteKeys, routeTarget, routeKey, start)
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

func (r *Runtime) processWithReception(
	ctx context.Context,
	evt domain.ExternalEvent,
	reception Reception,
	batch []domain.EventEnvelope,
	result *ProcessResult,
	requestID string,
	activatedRouteKeys []string,
	routeTarget domain.RouteTarget,
	routeKey string,
	start time.Time,
) (*ProcessResult, error) {
	var decision *domain.PromotionDecision
	var err error

	if isScheduleTriggerEvent(evt) {
		decision, err = r.schedulerPromotionDecision(ctx, requestID, evt)
		if err != nil {
			return r.handleScheduleRecovery(ctx, batch, evt, result, err)
		}
	} else {
		if reception == nil {
			return r.commitDirect(ctx, batch, evt, result)
		}
		decision, err = reception.Assess(ctx, domain.ReceptionInput{
			Event:         evt,
			RouteTarget:   routeTarget,
			RequestID:     requestID,
			RouteSnapshot: domain.RouteSnapshot{RouteKeys: activatedRouteKeys, MatchedBy: routeKey},
		})
		if err != nil {
			return nil, err
		}
	}

	return r.executePromotion(ctx, evt, batch, result, decision, requestID, start)
}

func (r *Runtime) executePromotion(
	ctx context.Context,
	evt domain.ExternalEvent,
	batch []domain.EventEnvelope,
	result *ProcessResult,
	decision *domain.PromotionDecision,
	requestID string,
	start time.Time,
) (*ProcessResult, error) {
	now := r.clock.Now().UTC()
	if decision == nil {
		return nil, fmt.Errorf("promotion decision is nil")
	}
	if decision.DecisionID == "" {
		decision.DecisionID = r.idgen.New(domain.IDPrefixDecision)
	}
	decision.RequestID = requestID
	if decision.ProducedAt.IsZero() {
		decision.ProducedAt = now
	}

	policyDecision, err := r.policy.DecidePromotion(decision)
	if err != nil {
		return nil, err
	}
	decision.Result = policyDecision.Result
	decision.ReasonCodes = policyDecision.ReasonCodes

	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypePromotionAssessed, domain.PromotionAssessedPayload{
		RequestID:          requestID,
		DecisionID:         decision.DecisionID,
		Result:             string(decision.Result),
		SelectedWorkflowID: decision.SelectedWorkflowID,
		ReasonCodes:        decision.ReasonCodes,
		Confidence:         decision.Confidence,
		AssessedAt:         now,
	})
	if err != nil {
		return nil, err
	}

	if decision.Result != domain.PromotionResultPromote {
		batch, err = r.appendDirectAnswerEvents(ctx, batch, evt, requestID, decision, now)
		if err != nil {
			return nil, err
		}
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
			"result", string(decision.Result),
		)
		return result, nil
	}

	candidates, err := r.workflow.Registry().ResolveCandidate(ctx, decision, &evt)
	if err != nil {
		return nil, err
	}
	ref, ok := workflow.UniqueCandidate(candidates)
	if !ok {
		return nil, fmt.Errorf("workflow candidate is not unique: %d", len(candidates))
	}
	decision.SelectedWorkflowID = ref.WorkflowID

	promoteCmd := domain.PromoteAndBindWorkflowCommand{
		RequestID:         requestID,
		TaskID:            r.idgen.New(domain.IDPrefixTask),
		BindingID:         r.idgen.New(domain.IDPrefixBinding),
		WorkflowID:        ref.WorkflowID,
		WorkflowSource:    ref.WorkflowSource,
		WorkflowRev:       ref.WorkflowRev,
		ManifestDigest:    ref.ManifestDigest,
		EntryStepID:       r.entryStepID(ref),
		RouteSnapshotRef:  "route_snapshot:" + requestID,
		ActivatedRouteKey: r.deriveTaskRouteKeys(evt),
		At:                now,
	}
	atomicBatch, err := r.buildPromoteAndBindBatch(promoteCmd)
	if err != nil {
		return nil, err
	}
	batch = append(batch, atomicBatch...)

	applyBatchCausation(batch, evt.CausationID)
	if err := r.commitBatch(ctx, batch); err != nil {
		return nil, err
	}
	result.TaskID = promoteCmd.TaskID
	result.RouteTargetKind = string(domain.RouteTargetTask)
	result.RouteTargetID = promoteCmd.TaskID
	result.CommitHLC = batchCommitHLC(batch)
	result.Promoted = true
	if err := r.persistDedupeRecord(ctx, evt, result); err != nil {
		return nil, err
	}
	r.logger.Info("request_processed",
		"request_id", requestID,
		"task_id", promoteCmd.TaskID,
		"duration_ms", time.Since(start).Milliseconds(),
		"result", string(decision.Result),
	)
	return result, nil
}

func (r *Runtime) appendDirectAnswerEvents(
	ctx context.Context,
	batch []domain.EventEnvelope,
	evt domain.ExternalEvent,
	requestID string,
	decision *domain.PromotionDecision,
	now time.Time,
) ([]domain.EventEnvelope, error) {
	answer := "reply:auto_direct_answer"
	toolStatus := "success"
	toolRef := "tool://direct_answer"
	responseRef := "result://direct_answer"
	failureCode := ""
	failureMessage := ""

	dispatchID := r.idgen.New(domain.IDPrefixDispatch)
	batch, err := r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeAgentDispatchRecorded, domain.AgentDispatchRecordedPayload{
		DispatchID:    dispatchID,
		OwnerKind:     domain.AggregateKindRequest,
		OwnerID:       requestID,
		RequestedRole: "helper",
		Goal:          "direct_answer:" + strings.TrimSpace(decision.IntentKind),
		AllowedTools:  []string{"local_agent"},
		WriteScopeRef: "read_only",
		ReturnToRef:   "request:" + requestID,
		DeadlineAt:    now.Add(30 * time.Second),
	})
	if err != nil {
		return nil, err
	}

	if r.directAgent != nil {
		req := agent.DirectAnswerRequest{
			RequestID:  requestID,
			EventID:    evt.EventID,
			TraceID:    evt.TraceID,
			UserInput:  evt.SourceRef,
			IntentKind: decision.IntentKind,
			Skill:      directAnswerSkillForIntent(decision.IntentKind),
		}
		execResult, execErr := r.directAgent.Execute(ctx, req)
		if execErr != nil {
			toolStatus = "failed"
			failureCode = "direct_answer_failed"
			failureMessage = execErr.Error()
			answer = "reply:auto_direct_answer_fallback"
			responseRef = "error://direct_answer"
		} else if execResult != nil {
			if strings.TrimSpace(execResult.Answer) != "" {
				answer = "reply://" + strings.TrimSpace(execResult.Answer)
			}
			if len(execResult.Sources) > 0 {
				responseRef = strings.TrimSpace(execResult.Sources[0])
			}
		}
	}

	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeToolCallRecorded, domain.ToolCallRecordedPayload{
		CallID:      r.idgen.New(domain.IDPrefixEvent),
		OwnerKind:   domain.AggregateKindRequest,
		OwnerID:     requestID,
		DispatchID:  dispatchID,
		ToolOrMCP:   directAnswerToolName(decision.IntentKind),
		RequestRef:  toolRef,
		ResponseRef: responseRef,
		Status:      toolStatus,
		StartedAt:   now,
		FinishedAt:  r.clock.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}

	dispatchStatus := "succeeded"
	if toolStatus != "success" {
		dispatchStatus = "failed"
	}
	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeAgentDispatchCompleted, domain.AgentDispatchCompletedPayload{
		DispatchID:     dispatchID,
		Status:         dispatchStatus,
		FailureCode:    failureCode,
		FailureMessage: failureMessage,
		CompletedAt:    r.clock.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}

	replyID := r.idgen.New(domain.IDPrefixReply)
	revoked := append([]string(nil), r.routeByReq[requestID]...)
	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeReplyRecorded, domain.ReplyRecordedPayload{
		ReplyID:        replyID,
		OwnerKind:      domain.AggregateKindRequest,
		OwnerID:        requestID,
		ReplyChannel:   evt.SourceKind,
		ReplyToEventID: evt.EventID,
		PayloadRef:     answer,
		Final:          true,
		DeliveredAt:    r.clock.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	resultID := r.idgen.New(domain.IDPrefixResult)
	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeTerminalResultRecorded, domain.TerminalResultRecordedPayload{
		ResultID:         resultID,
		OwnerKind:        domain.AggregateKindRequest,
		OwnerID:          requestID,
		FinalStatus:      string(domain.RequestStatusAnswered),
		FinalReplyID:     replyID,
		RevokedRouteKeys: revoked,
		ClosedAt:         r.clock.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeRequestAnswered, domain.RequestAnsweredPayload{
		RequestID:        requestID,
		FinalReplyID:     replyID,
		RevokedRouteKeys: revoked,
		AnsweredAt:       r.clock.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

func directAnswerSkillForIntent(intent string) string {
	switch strings.TrimSpace(intent) {
	case "weather_query":
		return "public-info-query"
	case "cluster_readonly_query":
		return "cluster-query"
	default:
		return ""
	}
}

func directAnswerToolName(intent string) string {
	switch strings.TrimSpace(intent) {
	case "weather_query":
		return "public_info_query"
	case "cluster_readonly_query":
		return "cluster_query"
	default:
		return "direct_answer"
	}
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
