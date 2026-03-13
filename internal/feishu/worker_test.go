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

func TestReplyWorkerStoresTargetAndDeliversReplyIdempotently(t *testing.T) {
	rootDir := t.TempDir()
	st, err := storepkg.Open(storepkg.Config{RootDir: filepath.Join(rootDir, "core"), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	svc, err := NewService(Config{
		Enabled:           true,
		AppID:             "cli_test_app",
		AppSecret:         "cli_test_secret",
		VerificationToken: "verify-token",
		ReplyInThread:     true,
	}, filepath.Join(rootDir, "transport"), platform.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	calls := []string{}
	svc.replyFunc = func(_ context.Context, target ReplyTarget, text, uuid string) (string, error) {
		calls = append(calls, target.MessageID+"|"+text+"|"+boolString(target.ReplyInThread)+"|"+uuid)
		return "om_reply_1", nil
	}

	meta := MessageMetadata{
		EventID:     "evt_feishu_external_1",
		MessageID:   "om_message_1",
		ChatID:      "oc_chat_1",
		ThreadID:    "omt_thread_1",
		MessageType: "text",
	}
	externalPayload, _ := json.Marshal(domain.ExternalEventIngestedPayload{
		Event: domain.ExternalEvent{
			EventID:       meta.EventID,
			EventType:     domain.EventTypeExternalEventIngested,
			SourceKind:    "direct_input",
			TransportKind: TransportKind,
			SourceRef:     "hello alice",
			InputSchemaID: MessageInputSchemaID,
			InputPatch:    EncodeMetadataPatch(meta),
		},
	})
	replyPayload, _ := json.Marshal(domain.ReplyRecordedPayload{
		ReplyID:        "reply_1",
		OwnerKind:      domain.AggregateKindRequest,
		OwnerID:        "req_1",
		ReplyChannel:   "direct_input",
		ReplyToEventID: meta.EventID,
		PayloadRef:     "reply://done",
		Final:          true,
		DeliveredAt:    time.Now().UTC(),
	})
	batch := []domain.EventEnvelope{
		{
			EventID:         "env_1",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     "req_1",
			EventType:       domain.EventTypeExternalEventIngested,
			Sequence:        1,
			GlobalHLC:       "2026-03-13T10:00:00.000000000Z#0001",
			ProducedAt:      time.Now().UTC(),
			Producer:        "test",
			PayloadSchemaID: "event.external_event_ingested",
			PayloadVersion:  domain.DefaultPayloadVersion,
			Payload:         externalPayload,
		},
		{
			EventID:         "env_2",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     "req_1",
			EventType:       domain.EventTypeReplyRecorded,
			Sequence:        2,
			GlobalHLC:       "2026-03-13T10:00:01.000000000Z#0002",
			ProducedAt:      time.Now().UTC(),
			Producer:        "test",
			PayloadSchemaID: "event.reply_recorded",
			PayloadVersion:  domain.DefaultPayloadVersion,
			Payload:         replyPayload,
		},
	}
	if err := st.AppendBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}

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
	if calls[0] != "om_message_1|done|true|reply_1" {
		t.Fatalf("unexpected feishu delivery payload: %s", calls[0])
	}
	cursor, err := svc.state.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "2026-03-13T10:00:01.000000000Z#0002" {
		t.Fatalf("cursor not advanced, got %s", cursor)
	}
	delivered, err := svc.state.Delivered("reply_1")
	if err != nil {
		t.Fatal(err)
	}
	if !delivered {
		t.Fatalf("reply should be marked as delivered")
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
