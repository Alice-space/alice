package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

type Registry struct {
	mu      sync.RWMutex
	clients map[string]Client
}

func NewRegistry() *Registry {
	return &Registry{clients: map[string]Client{}}
}

func (r *Registry) Register(domain string, client Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[domain] = client
}

func (r *Registry) Client(domain string) (Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	client, ok := r.clients[domain]
	return client, ok
}

type OutboxReconciler struct {
	indexes *store.BoltIndexStore
	clients *Registry
	writer  OutboxReceiptWriter
}

type OutboxReceiptWriter interface {
	RecordOutboxReceipt(ctx context.Context, payload domain.OutboxReceiptRecordedPayload) error
}

func NewOutboxReconciler(indexes *store.BoltIndexStore, clients *Registry, writer OutboxReceiptWriter) *OutboxReconciler {
	return &OutboxReconciler{indexes: indexes, clients: clients, writer: writer}
}

func (r *OutboxReconciler) Name() string { return "outbox-reconciler" }

func (r *OutboxReconciler) Recover(ctx context.Context) error {
	return r.Reconcile(ctx)
}

func (r *OutboxReconciler) Start(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.Reconcile(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *OutboxReconciler) Reconcile(ctx context.Context) error {
	records, err := r.indexes.ListPendingOutbox(ctx, "", time.Now().UTC(), 100)
	if err != nil {
		return err
	}
	for _, item := range records {
		client, ok := r.clients.Client(item.Domain)
		if !ok {
			continue
		}
		status, err := client.ActionStatus(ctx, item.ActionID)
		if err != nil {
			lookup := &MCPActionLookupRequest{
				ActionID:        item.ActionID,
				RemoteRequestID: item.RemoteRequestID,
				IdempotencyKey:  item.IdempotencyKey,
			}
			status, err = client.Lookup(ctx, lookup)
			if err != nil {
				return fmt.Errorf("reconcile outbox action %s: %w", item.ActionID, err)
			}
		}
		if r.writer != nil {
			receiptStatus := status.Status
			if receiptStatus == "completed" {
				receiptStatus = "succeeded"
			}
			if err := r.writer.RecordOutboxReceipt(ctx, domain.OutboxReceiptRecordedPayload{
				TaskID:          item.TaskID,
				ActionID:        item.ActionID,
				ReceiptSource:   "reconciler",
				ReceiptKind:     "lookup",
				ReceiptStatus:   receiptStatus,
				RemoteRequestID: status.RemoteRequestID,
				ExternalRef:     status.ExternalRef,
				ErrorCode:       status.ErrorCode,
				ErrorMessage:    status.ErrorMessage,
				RecordedAt:      time.Now().UTC(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type OutboxDispatcher struct {
	indexes *store.BoltIndexStore
	clients *Registry
	writer  OutboxReceiptWriter
}

func NewOutboxDispatcher(indexes *store.BoltIndexStore, clients *Registry, writer OutboxReceiptWriter) *OutboxDispatcher {
	return &OutboxDispatcher{indexes: indexes, clients: clients, writer: writer}
}

func (d *OutboxDispatcher) Name() string { return "outbox-dispatcher" }

func (d *OutboxDispatcher) Recover(ctx context.Context) error {
	return d.Dispatch(ctx)
}

func (d *OutboxDispatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.Dispatch(ctx); err != nil {
				return err
			}
		}
	}
}

func (d *OutboxDispatcher) Dispatch(ctx context.Context) error {
	if d.writer == nil {
		return nil
	}
	records, err := d.indexes.ListPendingOutbox(ctx, "", time.Now().UTC(), 100)
	if err != nil {
		return err
	}
	for _, item := range records {
		client, ok := d.clients.Client(item.Domain)
		if !ok {
			continue
		}
		resp, err := client.Action(ctx, &MCPActionRequest{
			ActionID:       item.ActionID,
			IdempotencyKey: item.IdempotencyKey,
			Domain:         item.Domain,
			ActionType:     item.ActionType,
			TargetRef:      item.TargetRef,
			DeadlineAt:     time.Now().UTC().Add(1 * time.Minute),
		})
		if err != nil {
			if writeErr := d.writer.RecordOutboxReceipt(ctx, domain.OutboxReceiptRecordedPayload{
				TaskID:        item.TaskID,
				ActionID:      item.ActionID,
				ReceiptSource: "dispatcher",
				ReceiptKind:   "submit",
				ReceiptStatus: "retry_wait",
				ErrorCode:     "dispatch_error",
				ErrorMessage:  err.Error(),
				RecordedAt:    time.Now().UTC(),
			}); writeErr != nil {
				return writeErr
			}
			continue
		}
		receiptStatus := resp.Status
		if receiptStatus == "completed" {
			receiptStatus = "succeeded"
		}
		if receiptStatus == "accepted" {
			receiptStatus = "dispatching"
		}
		if err := d.writer.RecordOutboxReceipt(ctx, domain.OutboxReceiptRecordedPayload{
			TaskID:          item.TaskID,
			ActionID:        item.ActionID,
			ReceiptSource:   "dispatcher",
			ReceiptKind:     "submit",
			ReceiptStatus:   receiptStatus,
			RemoteRequestID: resp.RemoteRequestID,
			ExternalRef:     resp.ExternalRef,
			ErrorCode:       resp.ErrorCode,
			ErrorMessage:    resp.ErrorMessage,
			RecordedAt:      time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	return nil
}
