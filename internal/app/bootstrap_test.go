package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alice/internal/domain"
	"alice/internal/platform"

	"github.com/gin-gonic/gin"
)

func TestAdminCancelTaskProducesAuditAndCancelEvent(t *testing.T) {
	cfg := &platform.Config{}
	cfg.Storage.RootDir = t.TempDir()
	cfg.Storage.SnapshotInterval = 100
	cfg.Workflow.ManifestRoots = []string{filepath.Join("..", "..", "configs", "workflows")}
	cfg.Auth.AdminToken = "admin-token"
	cfg.Auth.HumanActionSecret = "human-secret"
	cfg.HTTP.ListenAddr = "127.0.0.1:0"
	cfg.Scheduler.PollInterval = "1h"

	app, err := Bootstrap(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Store.Close()

	ref, ok := app.WorkflowRuntime.Registry().Reference("issue-delivery", "v1")
	if !ok {
		t.Fatalf("missing workflow ref")
	}
	if err := app.Bus.PromoteAndBindWorkflow(context.Background(), domain.PromoteAndBindWorkflowCommand{
		RequestID:        "req_admin_1",
		TaskID:           "task_admin_1",
		BindingID:        "bind_admin_1",
		WorkflowID:       ref.WorkflowID,
		WorkflowSource:   ref.WorkflowSource,
		WorkflowRev:      ref.WorkflowRev,
		ManifestDigest:   ref.ManifestDigest,
		EntryStepID:      "triage",
		RouteSnapshotRef: "route_snapshot:req_admin_1",
		At:               time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/tasks/task_admin_1/cancel", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	w := httptest.NewRecorder()
	app.HTTPServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected accepted cancel task status, got %d", w.Code)
	}
	var response domain.WriteAcceptedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	adminActionID := response.AdminActionID
	if strings.TrimSpace(adminActionID) == "" {
		t.Fatalf("admin cancel response must include admin_action_id")
	}

	var hasIngested, hasCancelled, hasEnvelopeCausation, hasAdminAudit, auditSeenBeforeIngest bool
	if err := app.Store.Replay(context.Background(), "", func(evt domain.EventEnvelope) error {
		switch evt.EventType {
		case domain.EventTypeAdminAuditRecorded:
			var payload domain.AdminAuditRecordedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if payload.AdminActionID == adminActionID {
				hasAdminAudit = true
			}
		case domain.EventTypeExternalEventIngested:
			var payload domain.ExternalEventIngestedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if strings.HasPrefix(payload.Event.SourceRef, "admin:task_cancel:") && payload.Event.CausationID == adminActionID {
				hasIngested = true
				if hasAdminAudit {
					auditSeenBeforeIngest = true
				}
			}
			if evt.CausationID == adminActionID {
				hasEnvelopeCausation = true
			}
		case domain.EventTypeStepExecutionCancelled:
			var payload domain.StepExecutionCancelledPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if payload.ReasonCode == "human_cancel" {
				hasCancelled = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasIngested || !hasCancelled || !hasEnvelopeCausation || !hasAdminAudit || !auditSeenBeforeIngest {
		t.Fatalf("cancel flow incomplete: audit=%v audit_before_ingest=%v ingested=%v cancelled=%v envelope_causation=%v", hasAdminAudit, auditSeenBeforeIngest, hasIngested, hasCancelled, hasEnvelopeCausation)
	}
}

func TestAdminRoutesRejectedWhenTokenNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(adminTokenMiddleware(""))
	r.POST("/v1/admin/reconcile/outbox", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := withAdminAuth(httptest.NewRequest(http.MethodPost, "/v1/admin/reconcile/outbox", nil), "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected admin route rejected when token missing, got %d", w.Code)
	}

	nonAdminReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	nonAdminW := httptest.NewRecorder()
	r.ServeHTTP(nonAdminW, nonAdminReq)
	if nonAdminW.Code != http.StatusOK {
		t.Fatalf("non-admin routes should pass through, got %d", nonAdminW.Code)
	}
}

func withAdminAuth(req *http.Request, token string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
