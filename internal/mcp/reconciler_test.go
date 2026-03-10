package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

type fakeClient struct {
	actionResp *MCPActionResponse
	statusResp *MCPActionStatusResponse
	actionErr  error
	statusErr  error
	lookupResp *MCPActionStatusResponse
	lookupErr  error
}

func (f *fakeClient) Action(context.Context, *MCPActionRequest) (*MCPActionResponse, error) {
	return f.actionResp, f.actionErr
}
func (f *fakeClient) Query(context.Context, *MCPQueryRequest) (*MCPQueryResponse, error) {
	return &MCPQueryResponse{Status: "completed"}, nil
}
func (f *fakeClient) ActionStatus(context.Context, string) (*MCPActionStatusResponse, error) {
	return f.statusResp, f.statusErr
}
func (f *fakeClient) Lookup(context.Context, *MCPActionLookupRequest) (*MCPActionStatusResponse, error) {
	return f.lookupResp, f.lookupErr
}
func (f *fakeClient) Health(context.Context) error { return nil }

type fakeWriter struct {
	mu       sync.Mutex
	receipts []domain.OutboxReceiptRecordedPayload
}

func (w *fakeWriter) RecordOutboxReceipt(_ context.Context, payload domain.OutboxReceiptRecordedPayload) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.receipts = append(w.receipts, payload)
	return nil
}

func TestOutboxDispatcherAndReconcilerWriteBack(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	queuedPayload := domain.OutboxQueuedPayload{
		ActionID:       "obx_1",
		Domain:         "github",
		ActionType:     "github.create_pr",
		TargetRef:      "repo:1",
		IdempotencyKey: "k-1",
		PayloadRef:     "blob://x",
		DeadlineAt:     time.Now().UTC().Add(time.Minute),
	}
	raw, _ := json.Marshal(queuedPayload)
	envelope := domain.EventEnvelope{
		EventID:         "evt_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "task_1",
		EventType:       domain.EventTypeOutboxQueued,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T00:00:00.000000000Z#0001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.outbox_queued",
		PayloadVersion:  "v1alpha1",
		Payload:         raw,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{envelope}); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	reg.Register("github", &fakeClient{
		actionResp: &MCPActionResponse{ActionID: "obx_1", Status: "accepted", RemoteRequestID: "r-1"},
		statusResp: &MCPActionStatusResponse{ActionID: "obx_1", Status: "running"},
	})
	writer := &fakeWriter{}
	dispatcher := NewOutboxDispatcher(st.Indexes, reg, writer)
	reconciler := NewOutboxReconciler(st.Indexes, reg, writer)

	if err := dispatcher.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(writer.receipts) < 2 {
		t.Fatalf("expected dispatcher + reconciler receipts, got %d", len(writer.receipts))
	}
	for _, receipt := range writer.receipts {
		if receipt.TaskID != "task_1" {
			t.Fatalf("receipt should carry task id, got %q", receipt.TaskID)
		}
	}
}
