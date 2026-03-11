package ops

import (
	"encoding/json"
	"slices"
	"strings"

	"alice/internal/domain"
)

// consume processes a single event and updates the read model.
func (m *readModel) consume(evt domain.EventEnvelope) {
	if compareHLC(evt.GlobalHLC, m.VisibleHLC) > 0 {
		m.VisibleHLC = evt.GlobalHLC
	}

	view := EventView{
		EventID:         evt.EventID,
		EventType:       string(evt.EventType),
		AggregateKind:   evt.AggregateKind,
		AggregateID:     evt.AggregateID,
		CausationID:     evt.CausationID,
		TraceID:         evt.TraceID,
		PayloadSchemaID: evt.PayloadSchemaID,
		PayloadVersion:  evt.PayloadVersion,
		GlobalHLC:       evt.GlobalHLC,
	}

	if evt.EventType == domain.EventTypeExternalEventIngested {
		var payload domain.ExternalEventIngestedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err == nil {
			view.External = &EventExternalView{
				SourceKind:     payload.Event.SourceKind,
				TransportKind:  payload.Event.TransportKind,
				SourceRef:      payload.Event.SourceRef,
				ActorRef:       payload.Event.ActorRef,
				ReplyToEventID: payload.Event.ReplyToEventID,
				PayloadRef:     payload.Event.PayloadRef,
			}
			if evt.AggregateKind == domain.AggregateKindRequest && strings.HasPrefix(evt.AggregateID, domain.IDPrefixRequest) {
				req := m.ensureRequest(evt.AggregateID)
				req.ConversationID = payload.Event.ConversationID
				req.ActorRef = payload.Event.ActorRef
				req.UpdatedHLC = evt.GlobalHLC
			}
			if strings.TrimSpace(payload.Event.EventID) != "" {
				m.eventsByExternalID[strings.TrimSpace(payload.Event.EventID)] = view
			}
		}
	}

	m.eventsByID[view.EventID] = view
	m.Events = append(m.Events, view)

	m.handleEvent(evt)
}

// handleEvent routes event-specific handling.
func (m *readModel) handleEvent(evt domain.EventEnvelope) {
	switch evt.EventType {
	case domain.EventTypeEphemeralRequestOpened:
		m.handleEphemeralRequestOpened(evt)
	case domain.EventTypePromotionAssessed:
		m.handlePromotionAssessed(evt)
	case domain.EventTypeContextPackRecorded:
		m.handleContextPackRecorded(evt)
	case domain.EventTypeAgentDispatchRecorded:
		m.handleAgentDispatchRecorded(evt)
	case domain.EventTypeToolCallRecorded:
		m.handleToolCallRecorded(evt)
	case domain.EventTypeReplyRecorded:
		m.handleReplyRecorded(evt)
	case domain.EventTypeTerminalResultRecorded:
		m.handleTerminalResultRecorded(evt)
	case domain.EventTypeRequestAnswered:
		m.handleRequestAnswered(evt)
	case domain.EventTypeRequestPromoted:
		m.handleRequestPromoted(evt)
	case domain.EventTypeTaskPromotedAndBound:
		m.handleTaskPromotedAndBound(evt)
	case domain.EventTypeStepExecutionStarted:
		m.handleStepExecutionStarted(evt)
	case domain.EventTypeStepExecutionCompleted:
		m.handleStepExecutionCompleted(evt)
	case domain.EventTypeStepExecutionFailed:
		m.handleStepExecutionFailed(evt)
	case domain.EventTypeStepExecutionCancelled:
		m.handleStepExecutionCancelled(evt)
	case domain.EventTypeTaskWaitingHumanMarked:
		m.handleTaskWaitingHumanMarked(evt)
	case domain.EventTypeTaskResumed:
		m.handleTaskResumed(evt)
	case domain.EventTypeOutboxQueued:
		m.handleOutboxQueued(evt)
	case domain.EventTypeOutboxReceiptRecorded:
		m.handleOutboxReceiptRecorded(evt)
	case domain.EventTypeUsageLedgerRecorded:
		m.handleUsageLedgerRecorded(evt)
	case domain.EventTypeApprovalRequestOpened:
		m.handleApprovalRequestOpened(evt)
	case domain.EventTypeApprovalRequestResolved:
		m.handleApprovalRequestResolved(evt)
	case domain.EventTypeHumanWaitRecorded:
		m.handleHumanWaitRecorded(evt)
	case domain.EventTypeHumanWaitResolved:
		m.handleHumanWaitResolved(evt)
	case domain.EventTypeScheduledTaskRegistered:
		m.handleScheduledTaskRegistered(evt)
	case domain.EventTypeScheduleTriggered:
		m.handleScheduleTriggered(evt)
	}
}

// Handler methods for each event type.

func (m *readModel) handleEphemeralRequestOpened(evt domain.EventEnvelope) {
	var p domain.EphemeralRequestOpenedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	req := m.ensureRequest(p.RequestID)
	req.Status = string(domain.RequestStatusOpen)
	req.UpdatedHLC = evt.GlobalHLC
	req.RouteSnapshotRef = p.RouteSnapshotRef
	req.OpenedByEventID = p.OpenedByEventID
}

func (m *readModel) handlePromotionAssessed(evt domain.EventEnvelope) {
	var p domain.PromotionAssessedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	req := m.ensureRequest(p.RequestID)
	req.PromotionDecision = p.DecisionID
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleContextPackRecorded(evt domain.EventEnvelope) {
	var p domain.ContextPackRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil || p.OwnerKind != domain.AggregateKindRequest {
		return
	}
	req := m.ensureRequest(p.OwnerID)
	if !slices.Contains(req.ContextPacks, p.ContextPackID) {
		req.ContextPacks = append(req.ContextPacks, p.ContextPackID)
	}
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleAgentDispatchRecorded(evt domain.EventEnvelope) {
	var p domain.AgentDispatchRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil || p.OwnerKind != domain.AggregateKindRequest {
		return
	}
	req := m.ensureRequest(p.OwnerID)
	if !slices.Contains(req.AgentDispatches, p.DispatchID) {
		req.AgentDispatches = append(req.AgentDispatches, p.DispatchID)
	}
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleToolCallRecorded(evt domain.EventEnvelope) {
	var p domain.ToolCallRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil || p.OwnerKind != domain.AggregateKindRequest {
		return
	}
	req := m.ensureRequest(p.OwnerID)
	if !slices.Contains(req.ToolCalls, p.CallID) {
		req.ToolCalls = append(req.ToolCalls, p.CallID)
	}
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleReplyRecorded(evt domain.EventEnvelope) {
	var p domain.ReplyRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil || p.OwnerKind != domain.AggregateKindRequest {
		return
	}
	req := m.ensureRequest(p.OwnerID)
	req.Reply = p.ReplyID
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleTerminalResultRecorded(evt domain.EventEnvelope) {
	var p domain.TerminalResultRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil || p.OwnerKind != domain.AggregateKindRequest {
		return
	}
	req := m.ensureRequest(p.OwnerID)
	req.TerminalResult = p.ResultID
	req.LastTerminalStatus = p.FinalStatus
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleRequestAnswered(evt domain.EventEnvelope) {
	var p domain.RequestAnsweredPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	req := m.ensureRequest(p.RequestID)
	req.Status = string(domain.RequestStatusAnswered)
	req.Reply = p.FinalReplyID
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleRequestPromoted(evt domain.EventEnvelope) {
	var p domain.RequestPromotedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	req := m.ensureRequest(p.RequestID)
	req.Status = string(domain.RequestStatusPromoted)
	req.RouteTargetTaskID = p.TaskID
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleTaskPromotedAndBound(evt domain.EventEnvelope) {
	var p domain.TaskPromotedAndBoundPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	task := m.ensureTask(p.TaskID)
	task.Status = string(domain.TaskStatusActive)
	task.SourceRequestID = p.RequestID
	task.WorkflowID = p.WorkflowID
	task.RepoRef = p.RepoRef
	task.Binding = map[string]string{
		"binding_id":        p.BindingID,
		"workflow_id":       p.WorkflowID,
		"workflow_source":   p.WorkflowSource,
		"workflow_rev":      p.WorkflowRev,
		"manifest_digest":   p.ManifestDigest,
		"entry_step_id":     p.EntryStepID,
		"route_snapshot":    p.RouteSnapshotRef,
		"scheduled_task_id": p.ScheduledTaskID,
	}
	task.UpdatedHLC = evt.GlobalHLC

	req := m.ensureRequest(p.RequestID)
	req.RouteTargetTaskID = p.TaskID
	req.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleStepExecutionStarted(evt domain.EventEnvelope) {
	var p domain.StepExecutionStartedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	task := m.ensureTask(p.TaskID)
	if !slices.Contains(task.Steps, p.ExecutionID) {
		task.Steps = append(task.Steps, p.ExecutionID)
	}
	task.CurrentExecution = p.ExecutionID
	task.Status = string(domain.TaskStatusActive)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleStepExecutionCompleted(evt domain.EventEnvelope) {
	var p domain.StepExecutionCompletedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	task := m.ensureTask(evt.AggregateID)
	task.Artifacts = append(task.Artifacts, p.OutputArtifactRefs...)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleStepExecutionFailed(evt domain.EventEnvelope) {
	task := m.ensureTask(evt.AggregateID)
	task.Status = string(domain.TaskStatusFailed)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleStepExecutionCancelled(evt domain.EventEnvelope) {
	task := m.ensureTask(evt.AggregateID)
	task.Status = string(domain.TaskStatusCancelled)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleTaskWaitingHumanMarked(evt domain.EventEnvelope) {
	var p domain.TaskWaitingHumanMarkedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	task := m.ensureTask(p.TaskID)
	task.Status = string(domain.TaskStatusWaitingHuman)
	task.WaitingReason = p.WaitingReason
	task.CurrentExecution = p.StepExecutionID
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleTaskResumed(evt domain.EventEnvelope) {
	var p domain.TaskResumedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	task := m.ensureTask(p.TaskID)
	task.Status = string(domain.TaskStatusActive)
	task.WaitingReason = p.WaitingReason
	task.CurrentExecution = p.StepExecutionID
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleOutboxQueued(evt domain.EventEnvelope) {
	task := m.ensureTask(evt.AggregateID)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleOutboxReceiptRecorded(evt domain.EventEnvelope) {
	var p domain.OutboxReceiptRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	if strings.EqualFold(p.ReceiptStatus, "dead") || strings.EqualFold(p.ReceiptStatus, "failed") {
		dl := &DeadletterView{
			DeadletterID:  "dl_" + strings.TrimSpace(p.ActionID),
			UpdatedHLC:    evt.GlobalHLC,
			SourceEventID: strings.TrimSpace(p.ActionID),
			FailureStage:  strings.TrimSpace(p.ReceiptKind),
			LastError:     strings.TrimSpace(p.ErrorMessage),
			Retryable:     false,
			LastFailedAt:  evt.ProducedAt,
		}
		if existing, ok := m.deadletters[dl.DeadletterID]; ok && existing != nil {
			dl.FirstFailedAt = existing.FirstFailedAt
			if dl.FirstFailedAt.IsZero() {
				dl.FirstFailedAt = evt.ProducedAt
			}
		} else {
			dl.FirstFailedAt = evt.ProducedAt
		}
		m.deadletters[dl.DeadletterID] = dl
	}
}

func (m *readModel) handleUsageLedgerRecorded(evt domain.EventEnvelope) {
	var p domain.UsageLedgerRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	task := m.ensureTask(p.TaskID)
	if !slices.Contains(task.Usage, p.EntryID) {
		task.Usage = append(task.Usage, p.EntryID)
	}
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleApprovalRequestOpened(evt domain.EventEnvelope) {
	var p domain.ApprovalRequestOpenedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	approval := m.ensureApproval(p.ApprovalRequestID)
	approval.TaskID = p.TaskID
	approval.StepExecutionID = p.StepExecutionID
	approval.GateType = p.GateType
	approval.Status = string(domain.GateStatusOpen)
	approval.ExpiresAt = p.DeadlineAt
	approval.AllowedDecisions = allowedApprovalDecisions(p.GateType)
	approval.UpdatedHLC = evt.GlobalHLC

	task := m.ensureTask(p.TaskID)
	if !slices.Contains(task.OpenApprovalIDs, p.ApprovalRequestID) {
		task.OpenApprovalIDs = append(task.OpenApprovalIDs, p.ApprovalRequestID)
	}
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleApprovalRequestResolved(evt domain.EventEnvelope) {
	var p domain.ApprovalRequestResolvedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	approval := m.ensureApproval(p.ApprovalRequestID)
	approval.Status = p.Resolution
	approval.Note = p.ResolutionRef
	approval.UpdatedHLC = evt.GlobalHLC

	task := m.ensureTask(approval.TaskID)
	task.OpenApprovalIDs = removeValue(task.OpenApprovalIDs, p.ApprovalRequestID)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleHumanWaitRecorded(evt domain.EventEnvelope) {
	var p domain.HumanWaitRecordedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	wait := m.ensureHumanWait(p.HumanWaitID)
	wait.TaskID = p.TaskID
	wait.StepExecutionID = p.StepExecutionID
	wait.WaitingReason = p.WaitingReason
	wait.Status = "open"
	wait.AllowedDecisions = normalizeResumeOptions(p.ResumeOptions)
	if slices.Contains(wait.AllowedDecisions, "rewind") {
		wait.RewindTargets = []string{"*"}
	}
	wait.ExpiresAt = p.DeadlineAt
	wait.Note = p.PromptRef
	wait.UpdatedHLC = evt.GlobalHLC

	task := m.ensureTask(p.TaskID)
	if !slices.Contains(task.OpenHumanWaitIDs, p.HumanWaitID) {
		task.OpenHumanWaitIDs = append(task.OpenHumanWaitIDs, p.HumanWaitID)
	}
	task.WaitingReason = p.WaitingReason
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleHumanWaitResolved(evt domain.EventEnvelope) {
	var p domain.HumanWaitResolvedPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	wait := m.ensureHumanWait(p.HumanWaitID)
	wait.Status = p.Resolution
	wait.Note = p.ResolutionRef
	wait.UpdatedHLC = evt.GlobalHLC

	task := m.ensureTask(wait.TaskID)
	task.OpenHumanWaitIDs = removeValue(task.OpenHumanWaitIDs, p.HumanWaitID)
	task.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleScheduledTaskRegistered(evt domain.EventEnvelope) {
	var p domain.ScheduledTaskRegisteredPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	s := m.ensureSchedule(p.ScheduledTaskID)
	s.SpecKind = p.SpecKind
	s.SpecText = p.SpecText
	s.Timezone = p.Timezone
	s.ScheduleRevision = p.ScheduleRevision
	s.TargetWorkflowID = p.TargetWorkflowID
	s.TargetWorkflowSource = p.TargetWorkflowSource
	s.TargetWorkflowRev = p.TargetWorkflowRev
	s.Enabled = p.Enabled
	s.NextFireAt = p.NextFireAt
	s.UpdatedHLC = evt.GlobalHLC
}

func (m *readModel) handleScheduleTriggered(evt domain.EventEnvelope) {
	var p domain.ScheduleTriggeredPayload
	if json.Unmarshal(evt.Payload, &p) != nil {
		return
	}
	s := m.ensureSchedule(p.ScheduledTaskID)
	s.LastFireAt = p.ScheduledForWindow
	s.UpdatedHLC = evt.GlobalHLC
}
