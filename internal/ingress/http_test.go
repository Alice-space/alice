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
	"alice/internal/feishu"
	"alice/internal/platform"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"

	"github.com/gin-gonic/gin"
)

func init() {
	// Set gin to test mode to avoid debug output
	gin.SetMode(gin.TestMode)
}

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
		nil,
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", nil)

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

	// Create gin router and register routes
	r := gin.New()
	ing.RegisterRoutes(r.Group("/v1"))

	body := NormalizedEvent{
		EventType:    domain.EventTypeExternalEventIngested,
		DecisionHash: "wrong-hash",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/human-actions/"+token, bytes.NewReader(raw))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for decision hash mismatch, got %d", w.Code)
	}

	body.DecisionHash = "hash-1"
	raw, _ = json.Marshal(body)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/human-actions/"+token, bytes.NewReader(raw))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
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
		nil,
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", nil, WebhookAuthConfig{
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

	// Create gin router and register routes
	r := gin.New()
	ing.RegisterRoutes(r.Group("/v1"))

	body := []byte(`{"scheduled_task_id":"sch_ingress_1","scheduled_for_window":"2026-03-10T09:00:00Z","event_type":"forged","idempotency_key":"forged"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader(body))
	req.Header.Set("X-Scheduler-Token", "scheduler-secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
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
		nil,
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", nil, WebhookAuthConfig{
		SchedulerSecret: "scheduler-secret",
	})

	// Create gin router and register routes
	r := gin.New()
	ing.RegisterRoutes(r.Group("/v1"))

	unauthReq := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader([]byte(`{"scheduled_task_id":"sch_unauth","scheduled_for_window":"2026-03-10T09:00:00Z"}`)))
	unauthW := httptest.NewRecorder()
	r.ServeHTTP(unauthW, unauthReq)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for missing scheduler token, got %d", unauthW.Code)
	}

	missingReq := httptest.NewRequest(http.MethodPost, "/v1/scheduler/fires", bytes.NewReader([]byte(`{"scheduled_task_id":"sch_missing","scheduled_for_window":"2026-03-10T09:00:00Z"}`)))
	missingReq.Header.Set("X-Scheduler-Token", "scheduler-secret")
	missingW := httptest.NewRecorder()
	r.ServeHTTP(missingW, missingReq)
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
	r.ServeHTTP(disabledW, disabledReq)
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
		nil,
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", nil, WebhookAuthConfig{
		GitHubSecret: "gh-secret",
	})

	// Create gin router and register routes
	r := gin.New()
	ing.RegisterRoutes(r.Group("/v1"))

	payload := []byte(`{"event_type":"ExternalEventIngested","request_id":"req_forged","task_id":"task_forged","conversation_id":"c","thread_id":"t"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", githubSignature("gh-secret", payload))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
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
	r.ServeHTTP(wBad, badReq)
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
		nil,
	)
	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", nil)

	// Create gin router and register routes
	r := gin.New()
	ing.RegisterRoutes(r.Group("/v1"))

	body := []byte(`{"event_type":"ScheduleTriggered","request_id":"req_forged","task_id":"task_forged","verified":true,"conversation_id":"conv","thread_id":"thr"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingress/web/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
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

func TestFeishuIngressUsesSDKCallbackAndNormalizesMessage(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	st, err := store.Open(store.Config{RootDir: rootDir, SnapshotInterval: 100})
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
		nil,
	)
	feishuService, err := feishu.NewService(feishu.Config{
		Enabled:           true,
		AppID:             "cli_test_app",
		AppSecret:         "cli_test_secret",
		VerificationToken: "verify-token",
	}, rootDir, platform.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer feishuService.Close()

	ing := NewHTTPIngress(runtime, policy.NewStaticReception(domain.NewULIDGenerator()), "secret", feishuService)
	r := gin.New()
	ing.RegisterRoutes(r.Group("/v1"))

	body := []byte(`{
		"schema":"2.0",
		"header":{
			"event_id":"evt_feishu_1",
			"event_type":"im.message.receive_v1",
			"app_id":"cli_test_app",
			"tenant_key":"tenant_1",
			"create_time":"1710000000000",
			"token":"verify-token"
		},
		"event":{
			"sender":{
				"sender_id":{"open_id":"ou_sender_1"},
				"sender_type":"user",
				"tenant_key":"tenant_1"
			},
			"message":{
				"message_id":"om_message_1",
				"chat_id":"oc_chat_1",
				"thread_id":"omt_thread_1",
				"chat_type":"group",
				"message_type":"text",
				"content":"{\"text\":\"hello alice\"}"
			}
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingress/im/feishu", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected feishu callback 200, got %d", w.Code)
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
	if external.Event.TransportKind != feishu.TransportKind {
		t.Fatalf("transport_kind should be %s, got %s", feishu.TransportKind, external.Event.TransportKind)
	}
	if external.Event.SourceRef != "hello alice" {
		t.Fatalf("source_ref should be text message, got %s", external.Event.SourceRef)
	}
	if external.Event.ConversationID != "feishu:oc_chat_1" {
		t.Fatalf("conversation_id should be namespaced chat id, got %s", external.Event.ConversationID)
	}
	if external.Event.ThreadID != "omt_thread_1" {
		t.Fatalf("thread_id should come from feishu thread, got %s", external.Event.ThreadID)
	}
	if external.Event.ActorRef != "feishu:open_id:ou_sender_1" {
		t.Fatalf("actor_ref should be namespaced sender id, got %s", external.Event.ActorRef)
	}
	if external.Event.InputSchemaID != feishu.MessageInputSchemaID {
		t.Fatalf("input_schema_id should be %s, got %s", feishu.MessageInputSchemaID, external.Event.InputSchemaID)
	}

	meta, err := feishu.DecodeMetadataPatch(external.Event.InputPatch)
	if err != nil {
		t.Fatal(err)
	}
	if meta.MessageID != "om_message_1" || meta.ChatID != "oc_chat_1" {
		t.Fatalf("feishu metadata should preserve reply target, got message=%s chat=%s", meta.MessageID, meta.ChatID)
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
