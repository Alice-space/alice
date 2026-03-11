package domain

import (
	"encoding/json"
	"time"
)

type EventEnvelope struct {
	EventID         string          `json:"event_id"`
	AggregateKind   string          `json:"aggregate_kind"`
	AggregateID     string          `json:"aggregate_id"`
	EventType       EventType       `json:"event_type"`
	Sequence        uint64          `json:"sequence"`
	GlobalHLC       string          `json:"global_hlc"`
	ParentEventID   string          `json:"parent_event_id"`
	CausationID     string          `json:"causation_id"`
	CorrelationID   string          `json:"correlation_id"`
	TraceID         string          `json:"trace_id"`
	ProducedAt      time.Time       `json:"produced_at"`
	Producer        string          `json:"producer"`
	PayloadSchemaID string          `json:"payload_schema_id"`
	PayloadVersion  string          `json:"payload_version"`
	Payload         json.RawMessage `json:"payload"`
}

type EventSchema struct {
	AggregateKind   string
	PayloadSchemaID string
	PayloadVersion  string
}

const DefaultPayloadVersion = "v1alpha1"

const (
	EventTypeExternalEventIngested     EventType = "ExternalEventIngested"
	EventTypeEphemeralRequestOpened    EventType = "EphemeralRequestOpened"
	EventTypePromotionAssessed         EventType = "PromotionAssessed"
	EventTypeRequestPromoted           EventType = "RequestPromoted"
	EventTypeRequestAnswered           EventType = "RequestAnswered"
	EventTypeContextPackRecorded       EventType = "ContextPackRecorded"
	EventTypeAgentDispatchRecorded     EventType = "AgentDispatchRecorded"
	EventTypeAgentDispatchCheckpointed EventType = "AgentDispatchCheckpointed"
	EventTypeAgentDispatchCompleted    EventType = "AgentDispatchCompleted"
	EventTypeToolCallRecorded          EventType = "ToolCallRecorded"
	EventTypeTaskPromotedAndBound      EventType = "TaskPromotedAndBound"
	EventTypeTaskWaitingHumanMarked    EventType = "TaskWaitingHumanMarked"
	EventTypeTaskResumed               EventType = "TaskResumed"
	EventTypeWorkflowBindingSuperseded EventType = "WorkflowBindingSuperseded"
	EventTypeStepExecutionStarted      EventType = "StepExecutionStarted"
	EventTypeStepExecutionCheckpointed EventType = "StepExecutionCheckpointed"
	EventTypeStepExecutionCompleted    EventType = "StepExecutionCompleted"
	EventTypeStepExecutionFailed       EventType = "StepExecutionFailed"
	EventTypeStepExecutionCancelled    EventType = "StepExecutionCancelled"
	EventTypeStepExecutionRewound      EventType = "StepExecutionRewound"
	EventTypeApprovalRequestOpened     EventType = "ApprovalRequestOpened"
	EventTypeApprovalRequestResolved   EventType = "ApprovalRequestResolved"
	EventTypeHumanWaitRecorded         EventType = "HumanWaitRecorded"
	EventTypeHumanWaitResolved         EventType = "HumanWaitResolved"
	EventTypeOutboxQueued              EventType = "OutboxQueued"
	EventTypeOutboxReceiptRecorded     EventType = "OutboxReceiptRecorded"
	EventTypeReplyRecorded             EventType = "ReplyRecorded"
	EventTypeTerminalResultRecorded    EventType = "TerminalResultRecorded"
	EventTypeUsageLedgerRecorded       EventType = "UsageLedgerRecorded"
	EventTypeScheduledTaskRegistered   EventType = "ScheduledTaskRegistered"
	EventTypeScheduleFire              EventType = "ScheduleFire"
	EventTypeScheduleTriggered         EventType = "ScheduleTriggered"
	EventTypeAdminAuditRecorded        EventType = "AdminAuditRecorded"
)

var eventSchemaRegistry = map[EventType]EventSchema{
	EventTypeExternalEventIngested:     {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.external_event_ingested", PayloadVersion: DefaultPayloadVersion},
	EventTypeEphemeralRequestOpened:    {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.request_opened", PayloadVersion: DefaultPayloadVersion},
	EventTypePromotionAssessed:         {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.promotion_assessed", PayloadVersion: DefaultPayloadVersion},
	EventTypeRequestPromoted:           {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.request_promoted", PayloadVersion: DefaultPayloadVersion},
	EventTypeRequestAnswered:           {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.request_answered", PayloadVersion: DefaultPayloadVersion},
	EventTypeContextPackRecorded:       {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.context_pack_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeAgentDispatchRecorded:     {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.agent_dispatch_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeAgentDispatchCheckpointed: {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.agent_dispatch_checkpointed", PayloadVersion: DefaultPayloadVersion},
	EventTypeAgentDispatchCompleted:    {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.agent_dispatch_completed", PayloadVersion: DefaultPayloadVersion},
	EventTypeToolCallRecorded:          {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.tool_call_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeTaskPromotedAndBound:      {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.task_promoted_and_bound", PayloadVersion: DefaultPayloadVersion},
	EventTypeTaskWaitingHumanMarked:    {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.task_waiting_human_marked", PayloadVersion: DefaultPayloadVersion},
	EventTypeTaskResumed:               {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.task_resumed", PayloadVersion: DefaultPayloadVersion},
	EventTypeWorkflowBindingSuperseded: {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.binding_superseded", PayloadVersion: DefaultPayloadVersion},
	EventTypeStepExecutionStarted:      {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.step_execution_started", PayloadVersion: DefaultPayloadVersion},
	EventTypeStepExecutionCheckpointed: {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.step_execution_checkpointed", PayloadVersion: DefaultPayloadVersion},
	EventTypeStepExecutionCompleted:    {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.step_execution_completed", PayloadVersion: DefaultPayloadVersion},
	EventTypeStepExecutionFailed:       {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.step_execution_failed", PayloadVersion: DefaultPayloadVersion},
	EventTypeStepExecutionCancelled:    {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.step_execution_cancelled", PayloadVersion: DefaultPayloadVersion},
	EventTypeStepExecutionRewound:      {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.step_execution_rewound", PayloadVersion: DefaultPayloadVersion},
	EventTypeApprovalRequestOpened:     {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.approval_request_opened", PayloadVersion: DefaultPayloadVersion},
	EventTypeApprovalRequestResolved:   {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.approval_request_resolved", PayloadVersion: DefaultPayloadVersion},
	EventTypeHumanWaitRecorded:         {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.human_wait_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeHumanWaitResolved:         {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.human_wait_resolved", PayloadVersion: DefaultPayloadVersion},
	EventTypeOutboxQueued:              {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.outbox_queued", PayloadVersion: DefaultPayloadVersion},
	EventTypeOutboxReceiptRecorded:     {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.outbox_receipt_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeReplyRecorded:             {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.reply_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeTerminalResultRecorded:    {AggregateKind: AggregateKindRequest, PayloadSchemaID: "event.terminal_result_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeUsageLedgerRecorded:       {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.usage_ledger_recorded", PayloadVersion: DefaultPayloadVersion},
	EventTypeScheduledTaskRegistered:   {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.scheduled_task_registered", PayloadVersion: DefaultPayloadVersion},
	EventTypeScheduleFire:              {AggregateKind: AggregateKindSchedule, PayloadSchemaID: "event.schedule_fire", PayloadVersion: DefaultPayloadVersion},
	EventTypeScheduleTriggered:         {AggregateKind: AggregateKindTask, PayloadSchemaID: "event.schedule_triggered", PayloadVersion: DefaultPayloadVersion},
	EventTypeAdminAuditRecorded:        {AggregateKind: AggregateKindOther, PayloadSchemaID: "event.admin_audit_recorded", PayloadVersion: DefaultPayloadVersion},
}

func EventSchemaFor(t EventType) (EventSchema, bool) {
	v, ok := eventSchemaRegistry[t]
	return v, ok
}

func AggregateKindAllowed(eventType EventType, aggregateKind string) bool {
	schema, ok := eventSchemaRegistry[eventType]
	if !ok {
		return false
	}
	return schema.AggregateKind == aggregateKind
}
