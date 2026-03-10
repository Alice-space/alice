package ingress

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

func TestHumanActionTokenVerifyAndDecisionHash(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := workflow.NewRegistry(nil)
	if err := reg.LoadRoots(ctx, []string{filepath.Join("..", "..", "configs", "workflows")}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret")

	claims := domain.HumanActionTokenClaims{
		ActionKind:        "approve",
		TaskID:            "task_1",
		ApprovalRequestID: "apr_1",
		StepExecutionID:   "exec_1",
		DecisionHash:      "hash-1",
		Nonce:             "nonce",
		ExpiresAt:         time.Now().UTC().Add(5 * time.Minute),
	}
	token := signToken(t, "secret", claims)

	body := NormalizedEvent{
		EventType:    domain.EventTypeExternalEventIngested,
		DecisionHash: "wrong-hash",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/human-actions/"+token, bytes.NewReader(raw))
	w := httptest.NewRecorder()
	ing.handleHumanAction(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for decision hash mismatch, got %d", w.Code)
	}

	body.DecisionHash = "hash-1"
	raw, _ = json.Marshal(body)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/human-actions/"+token, bytes.NewReader(raw))
	w2 := httptest.NewRecorder()
	ing.handleHumanAction(w2, req2)
	if w2.Code == http.StatusUnauthorized {
		t.Fatalf("expected token accepted, got %d", w2.Code)
	}
}

func TestHumanActionTokenExpiredRejected(t *testing.T) {
	ing := &HTTPIngress{humanActionSecret: []byte("secret")}
	token := signToken(t, "secret", domain.HumanActionTokenClaims{
		ActionKind:        "approve",
		TaskID:            "task_1",
		ApprovalRequestID: "apr_1",
		StepExecutionID:   "exec_1",
		DecisionHash:      "h",
		Nonce:             "n",
		ExpiresAt:         time.Now().UTC().Add(-time.Minute),
	})
	if _, err := ing.verifyHumanActionToken(token); err == nil {
		t.Fatalf("expected expired token error")
	}
}

func TestSchedulerFireEndpointDerivesServerPayload(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := workflow.NewRegistry(nil)
	if err := reg.LoadRoots(ctx, []string{filepath.Join("..", "..", "configs", "workflows")}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", WebhookAuthConfig{
		SchedulerSecret: "scheduler-secret",
	})
	// Register authoritative schedule source.
	rawPayload, _ := json.Marshal(domain.ScheduledTaskRegisteredPayload{
		ScheduledTaskID:      "sch_ingress_1",
		SpecKind:             "cron",
		SpecText:             "* * * * *",
		Timezone:             "UTC",
		ScheduleRevision:     "rev-1",
		TargetWorkflowID:     "issue-delivery",
		TargetWorkflowSource: "file://configs/workflows/issue-delivery/manifest.yaml",
		TargetWorkflowRev:    "v1",
		Enabled:              true,
		NextFireAt:           time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
		RegisteredAt:         time.Now().UTC(),
	})
	registerSource := domain.EventEnvelope{
		EventID:         "evt_sched_ingress_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "sch_ingress_1",
		EventType:       domain.EventTypeScheduledTaskRegistered,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T09:00:00.000000000Z#0001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.scheduled_task_registered",
		PayloadVersion:  "v1alpha1",
		Payload:         rawPayload,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{registerSource}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	ing.RegisterRoutes(mux)
	body := []byte(`{"scheduled_task_id":"sch_ingress_1","scheduled_for_window":"2026-03-10T09:00:00Z","event_type":"forged","idempotency_key":"forged"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader(body))
	req.Header.Set("X-Scheduler-Token", "scheduler-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected accepted, got %d", w.Code)
	}

	var found domain.ScheduleTriggeredPayload
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeScheduleTriggered {
			return json.Unmarshal(evt.Payload, &found)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantFireID := domain.ComputeFireID("sch_ingress_1", time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC))
	if found.FireID != wantFireID {
		t.Fatalf("scheduler fire id should be server-derived: got=%s want=%s", found.FireID, wantFireID)
	}
}

func TestSchedulerFireEndpointRejectsUnauthorizedAndMissingOrDisabledSource(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := workflow.NewRegistry(nil)
	if err := reg.LoadRoots(ctx, []string{filepath.Join("..", "..", "configs", "workflows")}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", WebhookAuthConfig{
		SchedulerSecret: "scheduler-secret",
	})
	mux := http.NewServeMux()
	ing.RegisterRoutes(mux)

	unauthReq := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader([]byte(`{"scheduled_task_id":"sch_unauth","scheduled_for_window":"2026-03-10T09:00:00Z"}`)))
	unauthW := httptest.NewRecorder()
	mux.ServeHTTP(unauthW, unauthReq)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for missing scheduler token, got %d", unauthW.Code)
	}

	missingReq := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader([]byte(`{"scheduled_task_id":"sch_missing","scheduled_for_window":"2026-03-10T09:00:00Z"}`)))
	missingReq.Header.Set("X-Scheduler-Token", "scheduler-secret")
	missingW := httptest.NewRecorder()
	mux.ServeHTTP(missingW, missingReq)
	if missingW.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing schedule source, got %d", missingW.Code)
	}

	disabledRaw, _ := json.Marshal(domain.ScheduledTaskRegisteredPayload{
		ScheduledTaskID:      "sch_disabled",
		SpecKind:             "cron",
		SpecText:             "* * * * *",
		Timezone:             "UTC",
		ScheduleRevision:     "rev-disabled",
		TargetWorkflowID:     "issue-delivery",
		TargetWorkflowSource: "file://configs/workflows/issue-delivery/manifest.yaml",
		TargetWorkflowRev:    "v1",
		Enabled:              false,
		NextFireAt:           time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
		RegisteredAt:         time.Now().UTC(),
	})
	registerDisabled := domain.EventEnvelope{
		EventID:         "evt_sched_disabled_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "sch_disabled",
		EventType:       domain.EventTypeScheduledTaskRegistered,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T09:00:00.000000000Z#0002",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.scheduled_task_registered",
		PayloadVersion:  "v1alpha1",
		Payload:         disabledRaw,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{registerDisabled}); err != nil {
		t.Fatal(err)
	}

	disabledReq := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader([]byte(`{"scheduled_task_id":"sch_disabled","scheduled_for_window":"2026-03-10T09:00:00Z"}`)))
	disabledReq.Header.Set("X-Scheduler-Token", "scheduler-secret")
	disabledW := httptest.NewRecorder()
	mux.ServeHTTP(disabledW, disabledReq)
	if disabledW.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for disabled schedule source, got %d", disabledW.Code)
	}

	hasHumanWait := false
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeHumanWaitRecorded {
			hasHumanWait = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if hasHumanWait {
		t.Fatalf("scheduler fire endpoint must reject missing/disabled source without recovery wait side effects")
	}
}

func TestGitHubWebhookVerificationAndRouteSanitization(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := workflow.NewRegistry(nil)
	if err := reg.LoadRoots(ctx, []string{filepath.Join("..", "..", "configs", "workflows")}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", WebhookAuthConfig{
		GitHubSecret: "gh-secret",
	})

	mux := http.NewServeMux()
	ing.RegisterRoutes(mux)
	payload := []byte(`{"event_type":"ExternalEventIngested","request_id":"req_forged","task_id":"task_forged","conversation_id":"c","thread_id":"t"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", githubSignature("gh-secret", payload))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected accepted, got %d", w.Code)
	}
	var external domain.ExternalEventIngestedPayload
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeExternalEventIngested {
			return json.Unmarshal(evt.Payload, &external)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if external.Event.RequestID != "" || external.Event.TaskID != "" {
		t.Fatalf("webhook must not trust request/task ids from body")
	}
	if external.Event.SourceKind != "repo_comment" {
		t.Fatalf("webhook source_kind must stay semantic, got %s", external.Event.SourceKind)
	}
	if external.Event.TransportKind != "github" {
		t.Fatalf("webhook transport_kind should record transport, got %s", external.Event.TransportKind)
	}
	if !external.Event.Verified {
		t.Fatalf("webhook should be marked verified after signature check")
	}
	if external.Event.IdempotencyKey != "github:delivery-1" {
		t.Fatalf("unexpected webhook idempotency key: %s", external.Event.IdempotencyKey)
	}

	badReq := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github", bytes.NewReader(payload))
	badReq.Header.Set("X-GitHub-Event", "issue_comment")
	badReq.Header.Set("X-GitHub-Delivery", "delivery-2")
	badReq.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	wBad := httptest.NewRecorder()
	mux.ServeHTTP(wBad, badReq)
	if wBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid signature to be rejected, got %d", wBad.Code)
	}
}

func TestWebIngressSanitizesUntrustedObjectFields(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := workflow.NewRegistry(nil)
	if err := reg.LoadRoots(ctx, []string{filepath.Join("..", "..", "configs", "workflows")}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret")
	mux := http.NewServeMux()
	ing.RegisterRoutes(mux)

	body := []byte(`{"event_type":"ScheduleTriggered","request_id":"req_forged","task_id":"task_forged","verified":true,"conversation_id":"conv","thread_id":"thr"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingress/web/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected accepted, got %d", w.Code)
	}

	var external domain.ExternalEventIngestedPayload
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeExternalEventIngested {
			return json.Unmarshal(evt.Payload, &external)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if external.Event.RequestID != "" || external.Event.TaskID != "" {
		t.Fatalf("web ingress must scrub untrusted request/task ids")
	}
	if external.Event.SourceKind != "direct_input" {
		t.Fatalf("web ingress source_kind must be direct_input, got %s", external.Event.SourceKind)
	}
	if external.Event.TransportKind != "web" {
		t.Fatalf("web ingress transport_kind must be web, got %s", external.Event.TransportKind)
	}
	if external.Event.Verified {
		t.Fatalf("web ingress must not trust verified=true from caller")
	}
	if external.Event.EventType != domain.EventTypeExternalEventIngested {
		t.Fatalf("web ingress must force event type to ExternalEventIngested, got %s", external.Event.EventType)
	}
}

func githubSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func signToken(t *testing.T, secret string, claims domain.HumanActionTokenClaims) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "v1." + payload + "." + sig
}
