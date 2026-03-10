package ops

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

type EventExternalView struct {
	SourceKind     string `json:"source_kind,omitempty"`
	TransportKind  string `json:"transport_kind,omitempty"`
	SourceRef      string `json:"source_ref,omitempty"`
	ActorRef       string `json:"actor_ref,omitempty"`
	ReplyToEventID string `json:"reply_to_event_id,omitempty"`
	PayloadRef     string `json:"payload_ref,omitempty"`
}

type EventView struct {
	EventID         string             `json:"event_id"`
	EventType       string             `json:"event_type"`
	AggregateKind   string             `json:"aggregate_kind"`
	AggregateID     string             `json:"aggregate_id"`
	CausationID     string             `json:"causation_id,omitempty"`
	TraceID         string             `json:"trace_id,omitempty"`
	PayloadSchemaID string             `json:"payload_schema_id"`
	PayloadVersion  string             `json:"payload_version"`
	GlobalHLC       string             `json:"global_hlc"`
	External        *EventExternalView `json:"external,omitempty"`
}

type RequestView struct {
	RequestID          string   `json:"request_id"`
	UpdatedHLC         string   `json:"updated_hlc"`
	Status             string   `json:"status,omitempty"`
	ConversationID     string   `json:"conversation_id,omitempty"`
	ActorRef           string   `json:"actor_ref,omitempty"`
	PromotionDecision  string   `json:"promotion_decision,omitempty"`
	ContextPacks       []string `json:"context_packs,omitempty"`
	AgentDispatches    []string `json:"agent_dispatches,omitempty"`
	ToolCalls          []string `json:"toolcalls,omitempty"`
	Reply              string   `json:"reply,omitempty"`
	TerminalResult     string   `json:"terminal_result,omitempty"`
	RouteTargetTaskID  string   `json:"task_id,omitempty"`
	RouteSnapshotRef   string   `json:"route_snapshot_ref,omitempty"`
	OpenedByEventID    string   `json:"opened_by_event_id,omitempty"`
	LastTerminalStatus string   `json:"terminal_status,omitempty"`
}

type TaskView struct {
	TaskID           string                           `json:"task_id"`
	UpdatedHLC       string                           `json:"updated_hlc"`
	Status           string                           `json:"status,omitempty"`
	WaitingReason    string                           `json:"waiting_reason,omitempty"`
	Binding          map[string]string                `json:"binding,omitempty"`
	Steps            []string                         `json:"steps,omitempty"`
	Artifacts        []string                         `json:"artifacts,omitempty"`
	Outbox           []store.PendingOutboxIndexRecord `json:"outbox,omitempty"`
	Usage            []string                         `json:"usage,omitempty"`
	OpenApprovalIDs  []string                         `json:"open_approvals,omitempty"`
	OpenHumanWaitIDs []string                         `json:"open_human_waits,omitempty"`
	CurrentExecution string                           `json:"current_step_execution_id,omitempty"`
	SourceRequestID  string                           `json:"source_request_id,omitempty"`
	WorkflowID       string                           `json:"workflow_id,omitempty"`
	RepoRef          string                           `json:"repo_ref,omitempty"`
}

type ScheduleView struct {
	ScheduledTaskID      string    `json:"scheduled_task_id"`
	UpdatedHLC           string    `json:"updated_hlc"`
	Enabled              bool      `json:"enabled"`
	SpecKind             string    `json:"spec_kind,omitempty"`
	SpecText             string    `json:"spec_text,omitempty"`
	Timezone             string    `json:"timezone,omitempty"`
	TargetWorkflowID     string    `json:"target_workflow_id,omitempty"`
	TargetWorkflowSource string    `json:"target_workflow_source,omitempty"`
	TargetWorkflowRev    string    `json:"target_workflow_rev,omitempty"`
	ScheduleRevision     string    `json:"schedule_revision,omitempty"`
	NextFireAt           time.Time `json:"next_fire_at,omitempty"`
	LastFireAt           time.Time `json:"last_fire_at,omitempty"`
}

type ApprovalView struct {
	ApprovalRequestID string    `json:"approval_request_id"`
	UpdatedHLC        string    `json:"updated_hlc"`
	TaskID            string    `json:"task_id"`
	StepExecutionID   string    `json:"step_execution_id"`
	GateType          string    `json:"gate_type,omitempty"`
	Status            string    `json:"status"`
	AllowedDecisions  []string  `json:"allowed_decisions,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	Note              string    `json:"note,omitempty"`
}

type HumanWaitView struct {
	HumanWaitID      string    `json:"human_wait_id"`
	UpdatedHLC       string    `json:"updated_hlc"`
	TaskID           string    `json:"task_id"`
	StepExecutionID  string    `json:"step_execution_id,omitempty"`
	WaitingReason    string    `json:"waiting_reason,omitempty"`
	Status           string    `json:"status"`
	AllowedDecisions []string  `json:"allowed_decisions,omitempty"`
	RewindTargets    []string  `json:"rewind_targets,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	Note             string    `json:"note,omitempty"`
}

type DeadletterView struct {
	DeadletterID  string    `json:"deadletter_id"`
	UpdatedHLC    string    `json:"updated_hlc"`
	SourceEventID string    `json:"source_event_id,omitempty"`
	FailureStage  string    `json:"failure_stage,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	Retryable     bool      `json:"retryable"`
	FirstFailedAt time.Time `json:"first_failed_at,omitempty"`
	LastFailedAt  time.Time `json:"last_failed_at,omitempty"`
}

type OpsOverviewView struct {
	OpenRequests  int `json:"open_requests"`
	ActiveTasks   int `json:"active_tasks"`
	PendingOutbox int `json:"pending_outbox"`
	ApprovalQueue int `json:"approval_queue"`
	HumanQueue    int `json:"human_queue"`
}

type readModel struct {
	VisibleHLC         string
	Events             []EventView
	eventsByID         map[string]EventView
	eventsByExternalID map[string]EventView
	requests           map[string]*RequestView
	tasks              map[string]*TaskView
	schedules          map[string]*ScheduleView
	approvals          map[string]*ApprovalView
	humanWaits         map[string]*HumanWaitView
	deadletters        map[string]*DeadletterView
}

func newReadModel() *readModel {
	return &readModel{
		eventsByID:         map[string]EventView{},
		eventsByExternalID: map[string]EventView{},
		requests:           map[string]*RequestView{},
		tasks:              map[string]*TaskView{},
		schedules:          map[string]*ScheduleView{},
		approvals:          map[string]*ApprovalView{},
		humanWaits:         map[string]*HumanWaitView{},
		deadletters:        map[string]*DeadletterView{},
	}
}

func buildReadModel(ctx context.Context, st *store.Store) (*readModel, error) {
	model := newReadModel()
	if st == nil {
		return model, nil
	}
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		model.consume(evt)
		return nil
	}); err != nil {
		return nil, err
	}
	for _, task := range model.tasks {
		if st.Indexes == nil {
			continue
		}
		outbox, err := st.Indexes.ListPendingOutbox(ctx, "", time.Now().UTC(), 100)
		if err != nil {
			continue
		}
		for _, item := range outbox {
			if item.TaskID == task.TaskID {
				task.Outbox = append(task.Outbox, item)
			}
		}
	}
	return model, nil
}

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

	switch evt.EventType {
	case domain.EventTypeEphemeralRequestOpened:
		var p domain.EphemeralRequestOpenedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			req := m.ensureRequest(p.RequestID)
			req.Status = string(domain.RequestStatusOpen)
			req.UpdatedHLC = evt.GlobalHLC
			req.RouteSnapshotRef = p.RouteSnapshotRef
			req.OpenedByEventID = p.OpenedByEventID
		}
	case domain.EventTypePromotionAssessed:
		var p domain.PromotionAssessedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			req := m.ensureRequest(p.RequestID)
			req.PromotionDecision = p.DecisionID
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeContextPackRecorded:
		var p domain.ContextPackRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil && p.OwnerKind == domain.AggregateKindRequest {
			req := m.ensureRequest(p.OwnerID)
			req.ContextPacks = appendUnique(req.ContextPacks, p.ContextPackID)
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeAgentDispatchRecorded:
		var p domain.AgentDispatchRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil && p.OwnerKind == domain.AggregateKindRequest {
			req := m.ensureRequest(p.OwnerID)
			req.AgentDispatches = appendUnique(req.AgentDispatches, p.DispatchID)
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeToolCallRecorded:
		var p domain.ToolCallRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil && p.OwnerKind == domain.AggregateKindRequest {
			req := m.ensureRequest(p.OwnerID)
			req.ToolCalls = appendUnique(req.ToolCalls, p.CallID)
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeReplyRecorded:
		var p domain.ReplyRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil && p.OwnerKind == domain.AggregateKindRequest {
			req := m.ensureRequest(p.OwnerID)
			req.Reply = p.ReplyID
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeTerminalResultRecorded:
		var p domain.TerminalResultRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil && p.OwnerKind == domain.AggregateKindRequest {
			req := m.ensureRequest(p.OwnerID)
			req.TerminalResult = p.ResultID
			req.LastTerminalStatus = p.FinalStatus
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeRequestAnswered:
		var p domain.RequestAnsweredPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			req := m.ensureRequest(p.RequestID)
			req.Status = string(domain.RequestStatusAnswered)
			req.Reply = p.FinalReplyID
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeRequestPromoted:
		var p domain.RequestPromotedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			req := m.ensureRequest(p.RequestID)
			req.Status = string(domain.RequestStatusPromoted)
			req.RouteTargetTaskID = p.TaskID
			req.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeTaskPromotedAndBound:
		var p domain.TaskPromotedAndBoundPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
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
	case domain.EventTypeStepExecutionStarted:
		var p domain.StepExecutionStartedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			task := m.ensureTask(p.TaskID)
			task.Steps = appendUnique(task.Steps, p.ExecutionID)
			task.CurrentExecution = p.ExecutionID
			task.Status = string(domain.TaskStatusActive)
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeStepExecutionCompleted:
		var p domain.StepExecutionCompletedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			task := m.ensureTask(evt.AggregateID)
			task.Artifacts = append(task.Artifacts, p.OutputArtifactRefs...)
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeStepExecutionFailed:
		task := m.ensureTask(evt.AggregateID)
		task.Status = string(domain.TaskStatusFailed)
		task.UpdatedHLC = evt.GlobalHLC
	case domain.EventTypeStepExecutionCancelled:
		task := m.ensureTask(evt.AggregateID)
		task.Status = string(domain.TaskStatusCancelled)
		task.UpdatedHLC = evt.GlobalHLC
	case domain.EventTypeTaskWaitingHumanMarked:
		var p domain.TaskWaitingHumanMarkedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			task := m.ensureTask(p.TaskID)
			task.Status = string(domain.TaskStatusWaitingHuman)
			task.WaitingReason = p.WaitingReason
			task.CurrentExecution = p.StepExecutionID
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeTaskResumed:
		var p domain.TaskResumedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			task := m.ensureTask(p.TaskID)
			task.Status = string(domain.TaskStatusActive)
			task.WaitingReason = p.WaitingReason
			task.CurrentExecution = p.StepExecutionID
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeOutboxQueued:
		task := m.ensureTask(evt.AggregateID)
		task.UpdatedHLC = evt.GlobalHLC
	case domain.EventTypeOutboxReceiptRecorded:
		var p domain.OutboxReceiptRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
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
	case domain.EventTypeUsageLedgerRecorded:
		var p domain.UsageLedgerRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			task := m.ensureTask(p.TaskID)
			task.Usage = appendUnique(task.Usage, p.EntryID)
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeApprovalRequestOpened:
		var p domain.ApprovalRequestOpenedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			approval := m.ensureApproval(p.ApprovalRequestID)
			approval.TaskID = p.TaskID
			approval.StepExecutionID = p.StepExecutionID
			approval.GateType = p.GateType
			approval.Status = string(domain.GateStatusOpen)
			approval.ExpiresAt = p.DeadlineAt
			approval.AllowedDecisions = allowedApprovalDecisions(p.GateType)
			approval.UpdatedHLC = evt.GlobalHLC
			task := m.ensureTask(p.TaskID)
			task.OpenApprovalIDs = appendUnique(task.OpenApprovalIDs, p.ApprovalRequestID)
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeApprovalRequestResolved:
		var p domain.ApprovalRequestResolvedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			approval := m.ensureApproval(p.ApprovalRequestID)
			approval.Status = p.Resolution
			approval.Note = p.ResolutionRef
			approval.UpdatedHLC = evt.GlobalHLC
			task := m.ensureTask(approval.TaskID)
			task.OpenApprovalIDs = removeValue(task.OpenApprovalIDs, p.ApprovalRequestID)
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeHumanWaitRecorded:
		var p domain.HumanWaitRecordedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			wait := m.ensureHumanWait(p.HumanWaitID)
			wait.TaskID = p.TaskID
			wait.StepExecutionID = p.StepExecutionID
			wait.WaitingReason = p.WaitingReason
			wait.Status = "open"
			wait.AllowedDecisions = normalizeResumeOptions(p.ResumeOptions)
			if containsValue(wait.AllowedDecisions, "rewind") {
				wait.RewindTargets = []string{"*"}
			}
			wait.ExpiresAt = p.DeadlineAt
			wait.Note = p.PromptRef
			wait.UpdatedHLC = evt.GlobalHLC
			task := m.ensureTask(p.TaskID)
			task.OpenHumanWaitIDs = appendUnique(task.OpenHumanWaitIDs, p.HumanWaitID)
			task.WaitingReason = p.WaitingReason
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeHumanWaitResolved:
		var p domain.HumanWaitResolvedPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			wait := m.ensureHumanWait(p.HumanWaitID)
			wait.Status = p.Resolution
			wait.Note = p.ResolutionRef
			wait.UpdatedHLC = evt.GlobalHLC
			task := m.ensureTask(wait.TaskID)
			task.OpenHumanWaitIDs = removeValue(task.OpenHumanWaitIDs, p.HumanWaitID)
			task.UpdatedHLC = evt.GlobalHLC
		}
	case domain.EventTypeScheduledTaskRegistered:
		var p domain.ScheduledTaskRegisteredPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
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
	case domain.EventTypeScheduleTriggered:
		var p domain.ScheduleTriggeredPayload
		if json.Unmarshal(evt.Payload, &p) == nil {
			s := m.ensureSchedule(p.ScheduledTaskID)
			s.LastFireAt = p.ScheduledForWindow
			s.UpdatedHLC = evt.GlobalHLC
		}
	}
}

func (m *readModel) ensureRequest(id string) *RequestView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &RequestView{}
	}
	v, ok := m.requests[id]
	if ok {
		return v
	}
	v = &RequestView{RequestID: id}
	m.requests[id] = v
	return v
}

func (m *readModel) ensureTask(id string) *TaskView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &TaskView{}
	}
	v, ok := m.tasks[id]
	if ok {
		return v
	}
	v = &TaskView{TaskID: id}
	m.tasks[id] = v
	return v
}

func (m *readModel) ensureSchedule(id string) *ScheduleView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &ScheduleView{}
	}
	v, ok := m.schedules[id]
	if ok {
		return v
	}
	v = &ScheduleView{ScheduledTaskID: id}
	m.schedules[id] = v
	return v
}

func (m *readModel) ensureApproval(id string) *ApprovalView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &ApprovalView{}
	}
	v, ok := m.approvals[id]
	if ok {
		return v
	}
	v = &ApprovalView{ApprovalRequestID: id}
	m.approvals[id] = v
	return v
}

func (m *readModel) ensureHumanWait(id string) *HumanWaitView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &HumanWaitView{}
	}
	v, ok := m.humanWaits[id]
	if ok {
		return v
	}
	v = &HumanWaitView{HumanWaitID: id}
	m.humanWaits[id] = v
	return v
}

func (m *readModel) requestByID(id string) (RequestView, bool) {
	v, ok := m.requests[strings.TrimSpace(id)]
	if !ok || v == nil {
		return RequestView{}, false
	}
	return *v, true
}

func (m *readModel) taskByID(id string) (TaskView, bool) {
	v, ok := m.tasks[strings.TrimSpace(id)]
	if !ok || v == nil {
		return TaskView{}, false
	}
	return *v, true
}

func (m *readModel) scheduleByID(id string) (ScheduleView, bool) {
	v, ok := m.schedules[strings.TrimSpace(id)]
	if !ok || v == nil {
		return ScheduleView{}, false
	}
	return *v, true
}

func (m *readModel) approvalByID(id string) (ApprovalView, bool) {
	v, ok := m.approvals[strings.TrimSpace(id)]
	if !ok || v == nil {
		return ApprovalView{}, false
	}
	return *v, true
}

func (m *readModel) humanWaitByID(id string) (HumanWaitView, bool) {
	v, ok := m.humanWaits[strings.TrimSpace(id)]
	if !ok || v == nil {
		return HumanWaitView{}, false
	}
	return *v, true
}

func (m *readModel) deadletterByID(id string) (DeadletterView, bool) {
	v, ok := m.deadletters[strings.TrimSpace(id)]
	if !ok || v == nil {
		return DeadletterView{}, false
	}
	return *v, true
}

func (m *readModel) eventByID(id string) (EventView, bool) {
	key := strings.TrimSpace(id)
	if v, ok := m.eventsByID[key]; ok {
		return v, true
	}
	v, ok := m.eventsByExternalID[key]
	return v, ok
}

func (m *readModel) requestList() []RequestView {
	out := make([]RequestView, 0, len(m.requests))
	for _, v := range m.requests {
		if v != nil && v.RequestID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) taskList() []TaskView {
	out := make([]TaskView, 0, len(m.tasks))
	for _, v := range m.tasks {
		if v != nil && v.TaskID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) scheduleList() []ScheduleView {
	out := make([]ScheduleView, 0, len(m.schedules))
	for _, v := range m.schedules {
		if v != nil && v.ScheduledTaskID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) approvalList() []ApprovalView {
	out := make([]ApprovalView, 0, len(m.approvals))
	for _, v := range m.approvals {
		if v != nil && v.ApprovalRequestID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) humanWaitList() []HumanWaitView {
	out := make([]HumanWaitView, 0, len(m.humanWaits))
	for _, v := range m.humanWaits {
		if v != nil && v.HumanWaitID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) deadletterList() []DeadletterView {
	out := make([]DeadletterView, 0, len(m.deadletters))
	for _, v := range m.deadletters {
		if v != nil && v.DeadletterID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) eventList() []EventView {
	out := append([]EventView(nil), m.Events...)
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].GlobalHLC, out[j].GlobalHLC) > 0
	})
	return out
}

func allowedApprovalDecisions(gateType string) []string {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case "approval":
		return []string{"approve", "reject"}
	case "confirmation", "evaluation":
		return []string{"confirm", "reject"}
	case "budget":
		return []string{"resume-budget", "reject"}
	default:
		return nil
	}
}

func normalizeResumeOptions(options []string) []string {
	out := make([]string, 0, len(options))
	for _, option := range options {
		normalized := strings.ReplaceAll(string(domain.NormalizeHumanActionKind(option)), "_", "-")
		if normalized == "" {
			continue
		}
		out = appendUnique(out, normalized)
	}
	return out
}

func appendUnique(in []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	if containsValue(in, value) {
		return in
	}
	return append(in, value)
}

func containsValue(in []string, value string) bool {
	for _, item := range in {
		if item == value {
			return true
		}
	}
	return false
}

func removeValue(in []string, value string) []string {
	out := in[:0]
	for _, item := range in {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

func compareHLC(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	at, an := parseHLC(a)
	bt, bn := parseHLC(b)
	if !at.IsZero() && !bt.IsZero() {
		if at.Before(bt) {
			return -1
		}
		if at.After(bt) {
			return 1
		}
		if an < bn {
			return -1
		}
		if an > bn {
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
}

func parseHLC(v string) (time.Time, int64) {
	parts := strings.Split(strings.TrimSpace(v), "#")
	if len(parts) != 2 {
		return time.Time{}, 0
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0
	}
	return t.UTC(), n
}

func parseLimitAndCursor(reqLimit, reqCursor string) (int, int) {
	limit := 50
	if n, err := strconv.Atoi(strings.TrimSpace(reqLimit)); err == nil && n > 0 {
		if n > 500 {
			n = 500
		}
		limit = n
	}
	offset := 0
	if n, err := strconv.Atoi(strings.TrimSpace(reqCursor)); err == nil && n >= 0 {
		offset = n
	}
	return limit, offset
}

func paginateSlice[T any](items []T, offset, limit int) ([]T, string) {
	if offset >= len(items) {
		return []T{}, ""
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return items[offset:end], next
}
