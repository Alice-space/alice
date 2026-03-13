package notifier

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

type stubChannel struct {
	name    string
	enabled bool
	cursor  string
	events  []string
}

func (c *stubChannel) Name() string              { return c.name }
func (c *stubChannel) Enabled() bool             { return c.enabled }
func (c *stubChannel) Cursor() (string, error)   { return c.cursor, nil }
func (c *stubChannel) SaveCursor(v string) error { c.cursor = v; return nil }
func (c *stubChannel) HandleEvent(_ context.Context, evt domain.EventEnvelope) error {
	c.events = append(c.events, evt.EventID)
	return nil
}

func TestWorkerTracksCursorPerChannel(t *testing.T) {
	rootDir := t.TempDir()
	st, err := storepkg.Open(storepkg.Config{RootDir: filepath.Join(rootDir, "core"), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	appendEventBatch(t, st,
		newEnvelope(t, "env_1", domain.AggregateKindRequest, "req_1", domain.EventTypeExternalEventIngested, "2026-03-13T09:00:00.000000000Z#0001", domain.ExternalEventIngestedPayload{Event: domain.ExternalEvent{EventID: "evt_1", EventType: domain.EventTypeExternalEventIngested}}),
		newEnvelope(t, "env_2", domain.AggregateKindRequest, "req_1", domain.EventTypeReplyRecorded, "2026-03-13T09:00:01.000000000Z#0002", domain.ReplyRecordedPayload{ReplyID: "reply_1"}),
	)

	first := &stubChannel{name: "first", enabled: true}
	second := &stubChannel{name: "second", enabled: true, cursor: "2026-03-13T09:00:00.000000000Z#0001"}
	worker := NewWorker(st, time.Millisecond, platform.NewNoopLogger(), first, second)
	if err := worker.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(first.events); got != 2 {
		t.Fatalf("first channel expected 2 events, got %d", got)
	}
	if got := len(second.events); got != 1 {
		t.Fatalf("second channel expected 1 event after cursor, got %d", got)
	}
	if first.cursor != "2026-03-13T09:00:01.000000000Z#0002" || second.cursor != "2026-03-13T09:00:01.000000000Z#0002" {
		t.Fatalf("unexpected cursors: first=%s second=%s", first.cursor, second.cursor)
	}
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
