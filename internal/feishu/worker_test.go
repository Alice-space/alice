package feishu

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"alice/internal/domain"
	"alice/internal/platform"
	storepkg "alice/internal/store"
)

type sentMessage struct {
	target  ReplyTarget
	msgType string
	content string
	uuid    string
}

func TestReplyWorkerStoresTargetAndDeliversReplyIdempotently(t *testing.T) {
	rootDir := t.TempDir()
	st, err := storepkg.Open(storepkg.Config{RootDir: filepath.Join(rootDir, "core"), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := newTestFeishuService(t, filepath.Join(rootDir, "transport"))
	defer svc.Close()

	var calls []sentMessage
	svc.sendFunc = func(_ context.Context, target ReplyTarget, msgType, content, uuid string) (string, error) {
		calls = append(calls, sentMessage{target: target, msgType: msgType, content: content, uuid: uuid})
		return "om_reply_1", nil
	}

	meta := MessageMetadata{
		EventID:     "evt_feishu_external_1",
		MessageID:   "om_message_1",
		ChatID:      "oc_chat_1",
		ThreadID:    "omt_thread_1",
		MessageType: "text",
	}
	appendEventBatch(t, st,
		newEnvelope(t, "env_1", domain.AggregateKindRequest, "req_1", domain.EventTypeExternalEventIngested, "2026-03-13T10:00:00.000000000Z#0001", domain.ExternalEventIngestedPayload{
			Event: domain.ExternalEvent{
				EventID:       meta.EventID,
				EventType:     domain.EventTypeExternalEventIngested,
				SourceKind:    "direct_input",
				TransportKind: TransportKind,
				SourceRef:     "hello alice",
				InputSchemaID: MessageInputSchemaID,
				InputPatch:    EncodeMetadataPatch(meta),
			},
		}),
		newEnvelope(t, "env_2", domain.AggregateKindRequest, "req_1", domain.EventTypeReplyRecorded, "2026-03-13T10:00:01.000000000Z#0002", domain.ReplyRecordedPayload{
			ReplyID:        "reply_1",
			OwnerKind:      domain.AggregateKindRequest,
			OwnerID:        "req_1",
			ReplyChannel:   "direct_input",
			ReplyToEventID: meta.EventID,
			PayloadRef:     "reply://done",
			Final:          true,
			DeliveredAt:    time.Now().UTC(),
		}),
	)

	worker := NewReplyWorker(st, svc, platform.NewNoopLogger())
	if err := worker.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := worker.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected single feishu delivery, got %d", len(calls))
	}
	if calls[0].target.MessageID != "om_message_1" || !calls[0].target.ReplyInThread {
		t.Fatalf("unexpected target: %+v", calls[0].target)
	}
	if calls[0].msgType != "text" {
		t.Fatalf("unexpected message type: %s", calls[0].msgType)
	}
	if got := decodeTextBody(t, calls[0].content); got != "done" {
		t.Fatalf("unexpected text reply content: %s", got)
	}
	if calls[0].uuid != "reply_1" {
		t.Fatalf("unexpected reply uuid: %s", calls[0].uuid)
	}
	cursor, err := svc.state.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "2026-03-13T10:00:01.000000000Z#0002" {
		t.Fatalf("cursor not advanced, got %s", cursor)
	}
	assertDelivered(t, svc, "reply_1")
}

func TestOutboundWorkerDeliversApprovalCardWithHumanActionToken(t *testing.T) {
	rootDir := t.TempDir()
	st, err := storepkg.Open(storepkg.Config{RootDir: filepath.Join(rootDir, "core"), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := newTestFeishuService(t, filepath.Join(rootDir, "transport"))
	defer svc.Close()

	var calls []sentMessage
	svc.sendFunc = func(_ context.Context, target ReplyTarget, msgType, content, uuid string) (string, error) {
		calls = append(calls, sentMessage{target: target, msgType: msgType, content: content, uuid: uuid})
		return "om_card_approval_1", nil
	}

	secret := []byte("approval-secret")
	meta := MessageMetadata{
		EventID:     "evt_feishu_external_approval_1",
		MessageID:   "om_message_approval_1",
		ChatID:      "oc_chat_approval_1",
		ThreadID:    "omt_thread_approval_1",
		MessageType: "text",
	}
	appendEventBatch(t, st,
		newEnvelope(t, "env_approval_1", domain.AggregateKindRequest, "req_approval_1", domain.EventTypeExternalEventIngested, "2026-03-13T11:00:00.000000000Z#0101", domain.ExternalEventIngestedPayload{
			Event: domain.ExternalEvent{
				EventID:       meta.EventID,
				EventType:     domain.EventTypeExternalEventIngested,
				SourceKind:    "direct_input",
				TransportKind: TransportKind,
				SourceRef:     "approve budget",
				InputSchemaID: MessageInputSchemaID,
				InputPatch:    EncodeMetadataPatch(meta),
			},
		}),
		newEnvelope(t, "env_approval_2", domain.AggregateKindRequest, "req_approval_1", domain.EventTypeRequestPromoted, "2026-03-13T11:00:01.000000000Z#0102", domain.RequestPromotedPayload{
			RequestID:  "req_approval_1",
			TaskID:     "task_approval_1",
			PromotedAt: time.Now().UTC(),
		}),
		newEnvelope(t, "env_approval_3", domain.AggregateKindTask, "task_approval_1", domain.EventTypeApprovalRequestOpened, "2026-03-13T11:00:02.000000000Z#0103", domain.ApprovalRequestOpenedPayload{
			ApprovalRequestID: "apr_budget_1",
			TaskID:            "task_approval_1",
			StepExecutionID:   "exec_budget_1",
			GateType:          string(domain.GateTypeBudget),
			DeadlineAt:        time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC),
		}),
	)

	worker := NewOutboundWorker(st, svc, secret, platform.NewNoopLogger())
	if err := worker.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one outbound message, got %d", len(calls))
	}
	if calls[0].msgType != "interactive" {
		t.Fatalf("unexpected message type: %s", calls[0].msgType)
	}
	if calls[0].uuid != deliveryKey(deliveryScopeApproval, "apr_budget_1") {
		t.Fatalf("unexpected delivery uuid: %s", calls[0].uuid)
	}

	values := extractActionValues(t, calls[0].content)
	resumeBudget := findActionValue(t, values, string(domain.HumanActionResumeBudget))
	token := asString(t, resumeBudget[ActionTokenValueKey])
	claims, err := domain.VerifyHumanActionTokenV1(secret, token, time.Date(2026, 3, 13, 11, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.ActionKind != string(domain.HumanActionResumeBudget) {
		t.Fatalf("unexpected action kind in token: %s", claims.ActionKind)
	}
	if claims.WaitingReason != string(domain.WaitingReasonBudget) {
		t.Fatalf("unexpected waiting reason in token: %s", claims.WaitingReason)
	}
	if claims.ApprovalRequestID != "apr_budget_1" || claims.TaskID != "task_approval_1" || claims.StepExecutionID != "exec_budget_1" {
		t.Fatalf("unexpected approval claims: %+v", claims)
	}

	target, ok, err := svc.state.TaskTarget("task_approval_1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || target.MessageID != meta.MessageID {
		t.Fatalf("unexpected task target: ok=%v target=%+v", ok, target)
	}
	assertDelivered(t, svc, deliveryKey(deliveryScopeApproval, "apr_budget_1"))
}

func TestOutboundWorkerDeliversHumanWaitCard(t *testing.T) {
	rootDir := t.TempDir()
	st, err := storepkg.Open(storepkg.Config{RootDir: filepath.Join(rootDir, "core"), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc := newTestFeishuService(t, filepath.Join(rootDir, "transport"))
	defer svc.Close()

	var calls []sentMessage
	svc.sendFunc = func(_ context.Context, target ReplyTarget, msgType, content, uuid string) (string, error) {
		calls = append(calls, sentMessage{target: target, msgType: msgType, content: content, uuid: uuid})
		return "om_card_wait_1", nil
	}

	secret := []byte("wait-secret")
	meta := MessageMetadata{
		EventID:     "evt_feishu_external_wait_1",
		MessageID:   "om_message_wait_1",
		ChatID:      "oc_chat_wait_1",
		ThreadID:    "omt_thread_wait_1",
		MessageType: "text",
	}
	appendEventBatch(t, st,
		newEnvelope(t, "env_wait_1", domain.AggregateKindRequest, "req_wait_1", domain.EventTypeExternalEventIngested, "2026-03-13T12:00:00.000000000Z#0201", domain.ExternalEventIngestedPayload{
			Event: domain.ExternalEvent{
				EventID:       meta.EventID,
				EventType:     domain.EventTypeExternalEventIngested,
				SourceKind:    "direct_input",
				TransportKind: TransportKind,
				SourceRef:     "recover task",
				InputSchemaID: MessageInputSchemaID,
				InputPatch:    EncodeMetadataPatch(meta),
			},
		}),
		newEnvelope(t, "env_wait_2", domain.AggregateKindRequest, "req_wait_1", domain.EventTypeRequestPromoted, "2026-03-13T12:00:01.000000000Z#0202", domain.RequestPromotedPayload{
			RequestID:  "req_wait_1",
			TaskID:     "task_wait_1",
			PromotedAt: time.Now().UTC(),
		}),
		newEnvelope(t, "env_wait_3", domain.AggregateKindTask, "task_wait_1", domain.EventTypeHumanWaitRecorded, "2026-03-13T12:00:02.000000000Z#0203", domain.HumanWaitRecordedPayload{
			HumanWaitID:     "wait_recovery_1",
			TaskID:          "task_wait_1",
			StepExecutionID: "exec_wait_1",
			WaitingReason:   string(domain.WaitingReasonRecovery),
			ResumeOptions:   []string{string(domain.HumanActionResumeRecovery), string(domain.HumanActionCancel)},
			PromptRef:       "prompt:recover",
			DeadlineAt:      time.Date(2026, 3, 13, 13, 0, 0, 0, time.UTC),
		}),
	)

	worker := NewOutboundWorker(st, svc, secret, platform.NewNoopLogger())
	if err := worker.syncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one outbound message, got %d", len(calls))
	}
	if calls[0].msgType != "interactive" {
		t.Fatalf("unexpected message type: %s", calls[0].msgType)
	}
	if calls[0].uuid != deliveryKey(deliveryScopeWait, "wait_recovery_1") {
		t.Fatalf("unexpected delivery uuid: %s", calls[0].uuid)
	}

	values := extractActionValues(t, calls[0].content)
	resume := findActionValue(t, values, string(domain.HumanActionResumeRecovery))
	token := asString(t, resume[ActionTokenValueKey])
	claims, err := domain.VerifyHumanActionTokenV1(secret, token, time.Date(2026, 3, 13, 12, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.ActionKind != string(domain.HumanActionResumeRecovery) || claims.HumanWaitID != "wait_recovery_1" {
		t.Fatalf("unexpected wait claims: %+v", claims)
	}
	if claims.WaitingReason != string(domain.WaitingReasonRecovery) {
		t.Fatalf("unexpected waiting reason: %s", claims.WaitingReason)
	}
	assertDelivered(t, svc, deliveryKey(deliveryScopeWait, "wait_recovery_1"))
}

func newTestFeishuService(t *testing.T, storageRoot string) *Service {
	t.Helper()
	svc, err := NewService(Config{
		Enabled:           true,
		AppID:             "cli_test_app",
		AppSecret:         "cli_test_secret",
		VerificationToken: "verify-token",
		ReplyInThread:     true,
	}, storageRoot, platform.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func appendEventBatch(t *testing.T, st *storepkg.Store, events ...domain.EventEnvelope) {
	t.Helper()
	if err := st.AppendBatch(context.Background(), events); err != nil {
		t.Fatal(err)
	}
}

func newEnvelope(t *testing.T, eventID, aggregateKind, aggregateID string, eventType domain.EventType, globalHLC string, payload any) domain.EventEnvelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	schema, ok := domain.EventSchemaFor(eventType)
	if !ok {
		t.Fatalf("schema missing for event type %s", eventType)
	}
	return domain.EventEnvelope{
		EventID:         eventID,
		AggregateKind:   aggregateKind,
		AggregateID:     aggregateID,
		EventType:       eventType,
		Sequence:        1,
		GlobalHLC:       globalHLC,
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: schema.PayloadSchemaID,
		PayloadVersion:  schema.PayloadVersion,
		Payload:         raw,
	}
}

func decodeTextBody(t *testing.T, raw string) string {
	t.Helper()
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode text body: %v", err)
	}
	return payload.Text
}

func extractActionValues(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("decode card body: %v", err)
	}
	elements, ok := card["elements"].([]any)
	if !ok {
		t.Fatalf("card elements missing: %v", card)
	}
	values := make([]map[string]any, 0)
	for _, element := range elements {
		elemMap, ok := element.(map[string]any)
		if !ok || asStringOrEmpty(elemMap["tag"]) != "action" {
			continue
		}
		actions, _ := elemMap["actions"].([]any)
		for _, action := range actions {
			actionMap, ok := action.(map[string]any)
			if !ok {
				continue
			}
			value, ok := actionMap["value"].(map[string]any)
			if ok {
				values = append(values, value)
			}
		}
	}
	if len(values) == 0 {
		t.Fatalf("card action values missing: %s", raw)
	}
	return values
}

func findActionValue(t *testing.T, values []map[string]any, actionKind string) map[string]any {
	t.Helper()
	for _, value := range values {
		if asStringOrEmpty(value["action_kind"]) == actionKind {
			return value
		}
	}
	t.Fatalf("action kind %s not found", actionKind)
	return nil
}

func assertDelivered(t *testing.T, svc *Service, id string) {
	t.Helper()
	delivered, err := svc.state.Delivered(id)
	if err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatalf("delivery %s should be marked delivered", id)
	}
}

func asString(t *testing.T, v any) string {
	t.Helper()
	s := asStringOrEmpty(v)
	if s == "" {
		t.Fatalf("expected string value, got %#v", v)
	}
	return s
}

func asStringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}
