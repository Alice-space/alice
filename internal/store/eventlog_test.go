package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"alice/internal/domain"
)

func TestEventLogAppendReplay(t *testing.T) {
	dir := t.TempDir()
	log, err := OpenJSONLEventLog(filepath.Join(dir, "eventlog"), 1024*1024, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	payload, _ := json.Marshal(map[string]string{"k": "v"})
	batch := []domain.EventEnvelope{
		{
			EventID:         "evt_1",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     "req_1",
			EventType:       domain.EventTypeEphemeralRequestOpened,
			Sequence:        1,
			GlobalHLC:       "2026-03-10T00:00:00Z#0001",
			ProducedAt:      time.Now().UTC(),
			Producer:        "test",
			PayloadSchemaID: "event.request_opened",
			PayloadVersion:  "v1alpha1",
			Payload:         payload,
		},
		{
			EventID:         "evt_2",
			AggregateKind:   domain.AggregateKindRequest,
			AggregateID:     "req_1",
			EventType:       domain.EventTypePromotionAssessed,
			Sequence:        2,
			GlobalHLC:       "2026-03-10T00:00:00Z#0002",
			ProducedAt:      time.Now().UTC(),
			Producer:        "test",
			PayloadSchemaID: "event.promotion_assessed",
			PayloadVersion:  "v1alpha1",
			Payload:         payload,
		},
	}
	if err := log.Append(context.Background(), batch); err != nil {
		t.Fatal(err)
	}

	count := 0
	if err := log.Replay(context.Background(), "", func(domain.EventEnvelope) error {
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}

	count = 0
	if err := log.Replay(context.Background(), "2026-03-10T00:00:00Z#0001", func(domain.EventEnvelope) error {
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event after from_hlc, got %d", count)
	}
}
