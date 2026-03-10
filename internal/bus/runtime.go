package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"alice/internal/domain"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type Reception interface {
	Assess(ctx context.Context, in domain.ReceptionInput) (*domain.PromotionDecision, error)
}

type Runtime struct {
	store      *store.Store
	policy     *policy.Engine
	workflow   *workflow.Runtime
	routeKeys  domain.RouteKeyEncoder
	idgen      domain.IDGenerator
	clock      Clock
	shardCount int
	mu         sync.Mutex
	seqByAgg   map[string]uint64
	hlcCounter uint64
	routeByReq map[string][]string
	onCritical func(error)
}

var ErrScheduleSourceNotFound = errors.New("schedule source not found")
var ErrScheduleSourceDisabled = errors.New("schedule source disabled")

type Config struct {
	ShardCount int
}

type ProcessResult struct {
	RequestID       string `json:"request_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	RouteMatched    string `json:"route_matched,omitempty"`
	RouteTargetKind string `json:"route_target_kind,omitempty"`
	RouteTargetID   string `json:"route_target_id,omitempty"`
	EventID         string `json:"event_id,omitempty"`
	CommitHLC       string `json:"commit_hlc,omitempty"`
	Promoted        bool   `json:"promoted,omitempty"`
}

func NewRuntime(s *store.Store, p *policy.Engine, wf *workflow.Runtime, idgen domain.IDGenerator, cfg Config) *Runtime {
	if cfg.ShardCount <= 0 {
		cfg.ShardCount = 16
	}
	return &Runtime{
		store:      s,
		policy:     p,
		workflow:   wf,
		routeKeys:  domain.NewCanonicalRouteKeyEncoder(),
		idgen:      idgen,
		clock:      realClock{},
		shardCount: cfg.ShardCount,
		seqByAgg:   map[string]uint64{},
		routeByReq: map[string][]string{},
	}
}

func (r *Runtime) SetClock(clock Clock) {
	if clock != nil {
		r.clock = clock
	}
}

func (r *Runtime) SetCriticalFailureHandler(fn func(error)) {
	r.onCritical = fn
}

func (r *Runtime) IngestExternalEvent(ctx context.Context, evt domain.ExternalEvent, reception Reception) (*ProcessResult, error) {
	now := r.clock.Now()
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

	if evt.IdempotencyKey != "" {
		record, seen, err := r.store.Indexes.GetDedupeRecord(ctx, evt.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if seen {
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
			return result, nil
		}
	}

	routeTarget, routeKey, err := r.resolveRoute(ctx, evt)
	if err != nil {
		return nil, err
	}

	// Existing task hit must not fork a new ephemeral request.
	if routeTarget.Kind == domain.RouteTargetTask {
		return r.handleTaskRoute(ctx, routeTarget.ID, routeKey, evt)
	}

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
			ExpiresAt:          now.Add(24 * time.Hour),
		})
		if err != nil {
			return nil, err
		}
	}

	result := &ProcessResult{RequestID: requestID, RouteMatched: routeKey}
	result.EventID = evt.EventID
	result.RouteTargetKind = string(domain.RouteTargetRequest)
	result.RouteTargetID = requestID
	if routeTarget.Found() {
		result.RouteTargetKind = string(routeTarget.Kind)
		result.RouteTargetID = routeTarget.ID
	}
	if reception == nil && !isScheduleTriggerEvent(evt) {
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

	var decision *domain.PromotionDecision
	if isScheduleTriggerEvent(evt) {
		decision, err = r.schedulerPromotionDecision(ctx, requestID, evt)
		if err != nil {
			batch, err = r.appendScheduleRecoveryEvents(batch, evt, err)
			if err != nil {
				return nil, err
			}
			applyBatchCausation(batch, evt.CausationID)
			if commitErr := r.commitBatch(ctx, batch); commitErr != nil {
				return nil, commitErr
			}
			result := &ProcessResult{
				RequestID:       requestID,
				RouteMatched:    "schedule_recovery",
				RouteTargetKind: string(domain.RouteTargetRequest),
				RouteTargetID:   requestID,
				EventID:         evt.EventID,
				CommitHLC:       batchCommitHLC(batch),
			}
			if err := r.persistDedupeRecord(ctx, evt, result); err != nil {
				return nil, err
			}
			return result, nil
		}
	} else {
		if reception == nil {
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
		replyID := r.idgen.New(domain.IDPrefixReply)
		revoked := append([]string(nil), r.routeByReq[requestID]...)
		batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeReplyRecorded, domain.ReplyRecordedPayload{
			ReplyID:        replyID,
			OwnerKind:      domain.AggregateKindRequest,
			OwnerID:        requestID,
			ReplyChannel:   evt.SourceKind,
			ReplyToEventID: evt.EventID,
			PayloadRef:     "reply:auto_direct_answer",
			Final:          true,
			DeliveredAt:    now,
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
			ClosedAt:         now,
		})
		if err != nil {
			return nil, err
		}
		batch, err = r.appendEvent(batch, domain.AggregateKindRequest, requestID, domain.EventTypeRequestAnswered, domain.RequestAnsweredPayload{
			RequestID:        requestID,
			FinalReplyID:     replyID,
			RevokedRouteKeys: revoked,
			AnsweredAt:       now,
		})
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
	return result, nil
}

func (r *Runtime) RecordScheduleFire(ctx context.Context, cmd domain.RecordScheduleFireCommand) (domain.ScheduleTriggeredPayload, error) {
	window := cmd.ScheduledForWindowUTC.UTC()
	if window.IsZero() {
		window = r.clock.Now().UTC().Truncate(time.Minute)
	}
	source, err := r.RequireEnabledScheduleSource(ctx, cmd.ScheduledTaskID)
	if err != nil {
		return domain.ScheduleTriggeredPayload{}, err
	}
	payload := domain.ScheduleTriggeredPayload{
		FireID:                 domain.ComputeFireID(cmd.ScheduledTaskID, window),
		ScheduledTaskID:        cmd.ScheduledTaskID,
		ScheduledForWindow:     window,
		SourceScheduleRevision: source.ScheduleRevision,
		TargetWorkflowID:       source.TargetWorkflowID,
		TargetWorkflowSource:   source.TargetWorkflowSource,
		TargetWorkflowRev:      source.TargetWorkflowRev,
	}
	evt := domain.ExternalEvent{
		EventType:       domain.EventTypeScheduleTriggered,
		SourceKind:      "scheduler",
		TransportKind:   "scheduler",
		SourceRef:       payload.ScheduledForWindow.Format(time.RFC3339),
		ScheduledTaskID: cmd.ScheduledTaskID,
		IdempotencyKey:  payload.FireID,
		Verified:        true,
		ReceivedAt:      r.clock.Now(),
	}
	if _, err := r.IngestExternalEvent(ctx, evt, nil); err != nil {
		return payload, err
	}
	return payload, nil
}

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
	evt, err := r.newEnvelope(
		domain.AggregateKindOther,
		"admin:"+payload.AdminActionID,
		domain.EventTypeAdminAuditRecorded,
		payload,
	)
	if err != nil {
		return err
	}
	batch := []domain.EventEnvelope{evt}
	applyBatchCausation(batch, payload.AdminActionID)
	return r.commitBatch(ctx, batch)
}

func (r *Runtime) RequireEnabledScheduleSource(ctx context.Context, scheduledTaskID string) (store.ScheduleSourceIndexRecord, error) {
	source, ok, err := r.store.Indexes.GetScheduleSource(ctx, scheduledTaskID)
	if err != nil {
		return store.ScheduleSourceIndexRecord{}, err
	}
	if !ok {
		return store.ScheduleSourceIndexRecord{}, fmt.Errorf("%w: %s", ErrScheduleSourceNotFound, scheduledTaskID)
	}
	if !source.Enabled {
		return store.ScheduleSourceIndexRecord{}, fmt.Errorf("%w: %s", ErrScheduleSourceDisabled, scheduledTaskID)
	}
	return source, nil
}

func (r *Runtime) PromoteAndBindWorkflow(ctx context.Context, cmd domain.PromoteAndBindWorkflowCommand) error {
	batch, err := r.buildPromoteAndBindBatch(cmd)
	if err != nil {
		return err
	}
	return r.commitBatch(ctx, batch)
}

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

func (r *Runtime) resolveRoute(ctx context.Context, evt domain.ExternalEvent) (domain.RouteTarget, string, error) {
	if isScheduleTriggerEvent(evt) && evt.ScheduledTaskID != "" {
		source, ok, err := r.store.Indexes.GetScheduleSource(ctx, evt.ScheduledTaskID)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !ok || !source.Enabled {
			return domain.RouteTarget{}, "scheduled_source_missing", nil
		}
		return domain.RouteTarget{}, "scheduled_source", nil
	}

	// 1. explicit task/request id
	if evt.TaskID != "" {
		active, err := r.store.Indexes.IsTaskActive(ctx, evt.TaskID)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !active {
			return domain.RouteTarget{}, "", fmt.Errorf("%w: task_id=%s", domain.ErrTerminalObjectNotRoutable, evt.TaskID)
		}
		return domain.RouteTarget{Kind: domain.RouteTargetTask, ID: evt.TaskID}, "task_id", nil
	}
	if evt.RequestID != "" {
		open, err := r.store.Indexes.IsRequestOpen(ctx, evt.RequestID)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if !open {
			return domain.RouteTarget{}, "", fmt.Errorf("%w: request_id=%s", domain.ErrTerminalObjectNotRoutable, evt.RequestID)
		}
		return domain.RouteTarget{Kind: domain.RouteTargetRequest, ID: evt.RequestID}, "request_id", nil
	}

	type candidate struct {
		key string
		tag string
	}
	candidates := make([]candidate, 0, 7)
	if evt.ReplyToEventID != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.ReplyTo(evt.ReplyToEventID), tag: "reply_to_event_id"})
	}
	if evt.RepoRef != "" && evt.IssueRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.RepoIssue(evt.RepoRef, evt.IssueRef), tag: "repo_ref+issue_ref"})
	}
	if evt.RepoRef != "" && evt.PRRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.RepoPR(evt.RepoRef, evt.PRRef), tag: "repo_ref+pr_ref"})
	}
	if evt.ScheduledTaskID != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.ScheduledTask(evt.ScheduledTaskID), tag: "scheduled_task_id"})
	}
	if evt.ControlObjectRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.ControlObject(evt.ControlObjectRef), tag: "control_object_ref"})
	}
	if evt.WorkflowObjectRef != "" {
		candidates = append(candidates, candidate{key: r.routeKeys.WorkflowObject(evt.WorkflowObjectRef), tag: "workflow_object_ref"})
	}
	if evt.ConversationID != "" {
		candidates = append(candidates, candidate{
			key: r.routeKeys.Conversation(evt.SourceKind, evt.ConversationID, evt.ThreadID),
			tag: "conversation_id+thread_id",
		})
	}
	if evt.CoalescingKey != "" {
		candidates = append(candidates, candidate{key: evt.CoalescingKey, tag: "coalescing_key"})
	}

	for _, c := range candidates {
		target, err := r.store.Indexes.GetRouteTarget(ctx, c.key)
		if err != nil {
			return domain.RouteTarget{}, "", err
		}
		if target.Found() {
			if target.Kind == domain.RouteTargetTask {
				active, err := r.store.Indexes.IsTaskActive(ctx, target.ID)
				if err != nil {
					return domain.RouteTarget{}, "", err
				}
				if !active {
					continue
				}
			}
			if target.Kind == domain.RouteTargetRequest {
				open, err := r.store.Indexes.IsRequestOpen(ctx, target.ID)
				if err != nil {
					return domain.RouteTarget{}, "", err
				}
				if !open {
					continue
				}
			}
			if c.tag == "reply_to_event_id" && target.Kind == domain.RouteTargetTask {
				conflict, err := r.hasGovernanceRouteConflict(ctx, evt, target)
				if err != nil {
					return domain.RouteTarget{}, "", err
				}
				if conflict {
					continue
				}
			}
			return target, c.tag, nil
		}
	}
	return domain.RouteTarget{}, "new_request", nil
}

func (r *Runtime) hasGovernanceRouteConflict(ctx context.Context, evt domain.ExternalEvent, replyTarget domain.RouteTarget) (bool, error) {
	keys := []string{}
	if evt.ScheduledTaskID != "" {
		keys = append(keys, r.routeKeys.ScheduledTask(evt.ScheduledTaskID))
	}
	if evt.ControlObjectRef != "" {
		keys = append(keys, r.routeKeys.ControlObject(evt.ControlObjectRef))
	}
	if evt.WorkflowObjectRef != "" {
		keys = append(keys, r.routeKeys.WorkflowObject(evt.WorkflowObjectRef))
	}
	if len(keys) == 0 {
		return false, nil
	}
	for _, key := range keys {
		target, err := r.store.Indexes.GetRouteTarget(ctx, key)
		if err != nil {
			return false, err
		}
		if !target.Found() {
			continue
		}
		if target.Kind != replyTarget.Kind || target.ID != replyTarget.ID {
			return true, nil
		}
	}
	return false, nil
}

func (r *Runtime) deriveRequestRouteKeys(evt domain.ExternalEvent) []string {
	keys := []string{}
	if evt.ConversationID != "" {
		keys = append(keys, r.routeKeys.Conversation(evt.SourceKind, evt.ConversationID, evt.ThreadID))
	}
	if evt.CoalescingKey != "" {
		keys = append(keys, evt.CoalescingKey)
	}
	return keys
}

func (r *Runtime) deriveTaskRouteKeys(evt domain.ExternalEvent) []string {
	keys := []string{}
	if evt.ReplyToEventID != "" {
		keys = append(keys, r.routeKeys.ReplyTo(evt.ReplyToEventID))
	}
	if evt.RepoRef != "" && evt.IssueRef != "" {
		keys = append(keys, r.routeKeys.RepoIssue(evt.RepoRef, evt.IssueRef))
	}
	if evt.RepoRef != "" && evt.PRRef != "" {
		keys = append(keys, r.routeKeys.RepoPR(evt.RepoRef, evt.PRRef))
	}
	if evt.ScheduledTaskID != "" {
		keys = append(keys, r.routeKeys.ScheduledTask(evt.ScheduledTaskID))
	}
	if evt.ControlObjectRef != "" {
		keys = append(keys, r.routeKeys.ControlObject(evt.ControlObjectRef))
	}
	if evt.WorkflowObjectRef != "" {
		keys = append(keys, r.routeKeys.WorkflowObject(evt.WorkflowObjectRef))
	}
	return keys
}

func (r *Runtime) appendEvent(batch []domain.EventEnvelope, aggregateKind, aggregateID string, eventType domain.EventType, payload any) ([]domain.EventEnvelope, error) {
	evt, err := r.newEnvelope(aggregateKind, aggregateID, eventType, payload)
	if err != nil {
		return nil, err
	}
	return append(batch, evt), nil
}

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

func (r *Runtime) RecordOutboxReceipt(ctx context.Context, payload domain.OutboxReceiptRecordedPayload) error {
	taskID := payload.TaskID
	if taskID == "" {
		record, ok, err := r.store.Indexes.FindPendingOutboxByLookup(ctx, payload.ActionID, payload.RemoteRequestID, "")
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

func isScheduleTriggerEvent(evt domain.ExternalEvent) bool {
	return evt.EventType == domain.EventTypeScheduleTriggered && evt.SourceKind == "scheduler"
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

func (r *Runtime) ingestAggregateID(evt domain.ExternalEvent, taskID string) string {
	if evt.RequestID != "" {
		return evt.RequestID
	}
	if taskID != "" {
		return "req_for_task:" + taskID
	}
	return "req_ingress:" + evt.EventID
}

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

func isHumanActionSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "human_action", "human-action":
		return true
	default:
		return false
	}
}

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

func (r *Runtime) commitBatch(ctx context.Context, batch []domain.EventEnvelope) error {
	err := r.store.AppendBatch(ctx, batch)
	if err != nil && errors.Is(err, store.ErrCriticalIndexApply) && r.onCritical != nil {
		r.onCritical(err)
	}
	return err
}

func applyBatchCausation(batch []domain.EventEnvelope, causationID string) {
	if strings.TrimSpace(causationID) == "" {
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

func batchCommitHLC(batch []domain.EventEnvelope) string {
	if len(batch) == 0 {
		return ""
	}
	return strings.TrimSpace(batch[len(batch)-1].GlobalHLC)
}

func (r *Runtime) entryStepID(ref workflow.ManifestRef) string {
	m, err := r.workflow.Registry().Load(context.Background(), ref.WorkflowID, ref.WorkflowRev)
	if err != nil || len(m.Steps) == 0 {
		return "entry"
	}
	return m.Steps[0].ID
}

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
	return r.store.Indexes.SetDedupeRecord(ctx, evt.IdempotencyKey, record)
}

func validateHumanWaitPatchedInput(wait domain.HumanWaitRecordedPayload, patch json.RawMessage) (json.RawMessage, error) {
	return domain.ApplyHumanWaitInputPatch(wait, patch)
}
