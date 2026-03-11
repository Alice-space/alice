package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"

	"github.com/gin-gonic/gin"
)

func newHTTPManagerTestFixture(t *testing.T) (*HTTPManager, *store.Store, *bus.Runtime, *policy.StaticReception, *gin.Engine) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	reg := workflow.NewRegistry(nil)
	if err := reg.LoadRoots(ctx, []string{filepath.Join("..", "..", "configs", "workflows")}); err != nil {
		t.Fatal(err)
	}
	idgen := domain.NewULIDGenerator()
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		idgen,
		bus.Config{ShardCount: 4},
		nil,
	)
	reception := policy.NewStaticReception(idgen)
	mgr := NewHTTPManager(st, runtime, reception, AdminHooks{}, SurfaceConfig{
		AdminEventInjectionEnabled:     true,
		AdminScheduleFireReplayEnabled: true,
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	mgr.RegisterRoutesGin(router)
	return mgr, st, runtime, reception, router
}

func TestAdminSubmitEventRejectsForbiddenFieldsAndSetsCausation(t *testing.T) {
	_, st, _, _, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	forbidden := []byte(`{"event_id":"evt_forged","input_kind":"web_form_message","body_schema_id":"web-form-message.v1","body":{"text":"hello"}}`)
	reqForbidden := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/events", bytes.NewReader(forbidden))
	wForbidden := httptest.NewRecorder()
	router.ServeHTTP(wForbidden, reqForbidden)
	if wForbidden.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for forbidden field, got %d", wForbidden.Code)
	}

	duplicateRouteCritical := []byte(`{"input_kind":"control_plane_message","body_schema_id":"control-plane-message.v1","scheduled_task_id":"sch_1","body":{"text":"hello","scheduled_task_id":"sch_1"}}`)
	reqDup := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/events", bytes.NewReader(duplicateRouteCritical))
	wDup := httptest.NewRecorder()
	router.ServeHTTP(wDup, reqDup)
	if wDup.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for route-critical duplicate, got %d", wDup.Code)
	}

	valid := []byte(`{"input_kind":"web_form_message","body_schema_id":"web-form-message.v1","conversation_id":"conv_submit_event_1","body":{"text":"hello from admin submit event"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/events", bytes.NewReader(valid))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected accepted for valid submit event, got %d", w.Code)
	}

	var response domain.WriteAcceptedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(response.AdminActionID) == "" || strings.TrimSpace(response.EventID) == "" || strings.TrimSpace(response.CommitHLC) == "" {
		t.Fatalf("submit event response must contain admin_action_id/event_id/commit_hlc: %+v", response)
	}
	if strings.TrimSpace(response.RouteTargetKind) == "" || strings.TrimSpace(response.RouteTargetID) == "" {
		t.Fatalf("submit event response must contain route target: %+v", response)
	}

	var hasMatchingIngest bool
	var hasAdminAudit bool
	var ingestHLC string
	var auditHLC string
	if err := st.Replay(context.Background(), "", func(evt domain.EventEnvelope) error {
		switch evt.EventType {
		case domain.EventTypeExternalEventIngested:
			var payload domain.ExternalEventIngestedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if payload.Event.EventID == response.EventID {
				hasMatchingIngest = true
				ingestHLC = evt.GlobalHLC
				if payload.Event.CausationID != response.AdminActionID {
					t.Fatalf("causation_id must equal admin_action_id: got=%s want=%s", payload.Event.CausationID, response.AdminActionID)
				}
			}
		case domain.EventTypeAdminAuditRecorded:
			var payload domain.AdminAuditRecordedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if payload.AdminActionID == response.AdminActionID {
				hasAdminAudit = true
				auditHLC = evt.GlobalHLC
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasMatchingIngest {
		t.Fatalf("missing ingested external event for response event_id=%s", response.EventID)
	}
	if !hasAdminAudit {
		t.Fatalf("missing admin audit event for admin_action_id=%s", response.AdminActionID)
	}
	if compareHLC(auditHLC, ingestHLC) >= 0 {
		t.Fatalf("admin audit must commit before business ingest: audit=%s ingest=%s", auditHLC, ingestHLC)
	}
}

func TestReadEndpointsExposeVisibleHLCAndWaitQuery(t *testing.T) {
	_, st, runtime, reception, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	result, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		SourceRef:      "cli",
		ConversationID: "conv_read_wait_1",
		ThreadID:       "root",
		ReceivedAt:     time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.RequestID) == "" || strings.TrimSpace(result.CommitHLC) == "" {
		t.Fatalf("runtime result must include request_id and commit_hlc: %+v", result)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/requests/"+result.RequestID+"?min_hlc="+result.CommitHLC+"&wait_timeout_ms=50", nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("expected get request 200, got %d body=%s", getW.Code, getW.Body.String())
	}
	var getResp domain.GetResponse[RequestView]
	if err := json.Unmarshal(getW.Body.Bytes(), &getResp); err != nil {
		t.Fatal(err)
	}
	if compareHLC(getResp.VisibleHLC, result.CommitHLC) < 0 {
		t.Fatalf("visible_hlc must be >= commit_hlc: visible=%s commit=%s", getResp.VisibleHLC, result.CommitHLC)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/requests?limit=10&min_hlc="+result.CommitHLC+"&wait_timeout_ms=50", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected list requests 200, got %d body=%s", listW.Code, listW.Body.String())
	}
	var listResp domain.ListResponse[RequestView]
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if compareHLC(listResp.VisibleHLC, result.CommitHLC) < 0 {
		t.Fatalf("list visible_hlc must be >= commit_hlc: visible=%s commit=%s", listResp.VisibleHLC, result.CommitHLC)
	}
}

func TestAdminResolveApprovalAndSubmitFireCarryAdminCausation(t *testing.T) {
	_, st, runtime, reception, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	promoted, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "repo_comment",
		RepoRef:    "github:alice/repo",
		IssueRef:   "101",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.Promoted || strings.TrimSpace(promoted.TaskID) == "" {
		t.Fatalf("expected promoted task for approval resolve test, got %+v", promoted)
	}

	approvalPayload, _ := json.Marshal(domain.ApprovalRequestOpenedPayload{
		ApprovalRequestID: "apr_ops_101",
		TaskID:            promoted.TaskID,
		StepExecutionID:   "exec_ops_101",
		GateType:          "approval",
		RequiredSlots:     []string{"owner"},
		DeadlineAt:        time.Now().UTC().Add(time.Hour),
	})
	approvalOpen := domain.EventEnvelope{
		EventID:         "evt_approval_open_ops_101",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     promoted.TaskID,
		EventType:       domain.EventTypeApprovalRequestOpened,
		Sequence:        100,
		GlobalHLC:       "2026-03-10T10:00:00.000000000Z#9001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.approval_request_opened",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         approvalPayload,
	}
	if err := st.AppendBatch(context.Background(), []domain.EventEnvelope{approvalOpen}); err != nil {
		t.Fatal(err)
	}

	resolveBody := []byte(`{"approval_request_id":"apr_ops_101","task_id":"` + promoted.TaskID + `","step_execution_id":"exec_ops_101","decision":"approve","note":"approved by ops test"}`)
	resolveReq := httptest.NewRequest(http.MethodPost, "/v1/admin/resolve/approval", bytes.NewReader(resolveBody))
	resolveW := httptest.NewRecorder()
	router.ServeHTTP(resolveW, resolveReq)
	if resolveW.Code != http.StatusAccepted {
		t.Fatalf("expected resolve approval accepted, got %d body=%s", resolveW.Code, resolveW.Body.String())
	}
	var resolveResp domain.WriteAcceptedResponse
	if err := json.Unmarshal(resolveW.Body.Bytes(), &resolveResp); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(resolveResp.AdminActionID) == "" {
		t.Fatalf("resolve response missing admin_action_id")
	}
	if resolveResp.RouteTargetKind != string(domain.RouteTargetTask) || resolveResp.RouteTargetID != promoted.TaskID {
		t.Fatalf("resolve response must target task: %+v", resolveResp)
	}

	schedulePayload, _ := json.Marshal(domain.ScheduledTaskRegisteredPayload{
		ScheduledTaskID:      "sch_ops_fire_1",
		SpecKind:             "cron",
		SpecText:             "* * * * *",
		Timezone:             "UTC",
		ScheduleRevision:     "rev-fire-1",
		TargetWorkflowID:     "issue-delivery",
		TargetWorkflowSource: "file://configs/workflows/issue-delivery/manifest.yaml",
		TargetWorkflowRev:    "v1",
		Enabled:              true,
		NextFireAt:           time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
		RegisteredAt:         time.Now().UTC(),
	})
	scheduleOpen := domain.EventEnvelope{
		EventID:         "evt_schedule_open_ops_fire_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "sch_ops_fire_1",
		EventType:       domain.EventTypeScheduledTaskRegistered,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T10:00:00.000000000Z#9002",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.scheduled_task_registered",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         schedulePayload,
	}
	if err := st.AppendBatch(context.Background(), []domain.EventEnvelope{scheduleOpen}); err != nil {
		t.Fatal(err)
	}

	fireBody := []byte(`{"scheduled_task_id":"sch_ops_fire_1","scheduled_for_window":"2026-03-10T09:00:00Z"}`)
	fireReq := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/fires", bytes.NewReader(fireBody))
	fireW := httptest.NewRecorder()
	router.ServeHTTP(fireW, fireReq)
	if fireW.Code != http.StatusAccepted {
		t.Fatalf("expected submit fire accepted, got %d body=%s", fireW.Code, fireW.Body.String())
	}
	var fireResp domain.WriteAcceptedResponse
	if err := json.Unmarshal(fireW.Body.Bytes(), &fireResp); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fireResp.AdminActionID) == "" {
		t.Fatalf("submit fire response missing admin_action_id")
	}

	var hasResolveCausation bool
	var hasFireCausation bool
	var resolveAuditHLC string
	var resolveBusinessHLC string
	var fireAuditHLC string
	var fireBusinessHLC string
	if err := st.Replay(context.Background(), "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeApprovalRequestResolved && evt.CausationID == resolveResp.AdminActionID {
			hasResolveCausation = true
			resolveBusinessHLC = evt.GlobalHLC
		}
		if evt.EventType == domain.EventTypeScheduleTriggered && evt.CausationID == fireResp.AdminActionID {
			hasFireCausation = true
			fireBusinessHLC = evt.GlobalHLC
		}
		if evt.EventType == domain.EventTypeAdminAuditRecorded {
			var payload domain.AdminAuditRecordedPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if payload.AdminActionID == resolveResp.AdminActionID {
				resolveAuditHLC = evt.GlobalHLC
			}
			if payload.AdminActionID == fireResp.AdminActionID {
				fireAuditHLC = evt.GlobalHLC
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasResolveCausation {
		t.Fatalf("approval resolved event must carry admin_action_id causation")
	}
	if !hasFireCausation {
		t.Fatalf("schedule triggered event must carry admin_action_id causation")
	}
	if compareHLC(resolveAuditHLC, resolveBusinessHLC) >= 0 {
		t.Fatalf("resolve audit must commit before business event: audit=%s business=%s", resolveAuditHLC, resolveBusinessHLC)
	}
	if compareHLC(fireAuditHLC, fireBusinessHLC) >= 0 {
		t.Fatalf("fire audit must commit before business event: audit=%s business=%s", fireAuditHLC, fireBusinessHLC)
	}
}

func TestAdminRuntimeErrorMappingUses404And412(t *testing.T) {
	_, st, runtime, reception, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	fireReq := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/fires", bytes.NewReader([]byte(`{"scheduled_task_id":"missing_schedule","scheduled_for_window":"2026-03-10T09:00:00Z"}`)))
	fireW := httptest.NewRecorder()
	router.ServeHTTP(fireW, fireReq)
	if fireW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing schedule source, got %d body=%s", fireW.Code, fireW.Body.String())
	}

	promoted, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "repo_comment",
		RepoRef:    "github:alice/repo",
		IssueRef:   "cancel-mapping-1",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.Promoted || promoted.TaskID == "" {
		t.Fatalf("expected promoted task, got %+v", promoted)
	}

	firstCancelReq := httptest.NewRequest(http.MethodPost, "/v1/admin/tasks/"+promoted.TaskID+"/cancel", nil)
	firstCancelW := httptest.NewRecorder()
	router.ServeHTTP(firstCancelW, firstCancelReq)
	if firstCancelW.Code != http.StatusAccepted {
		t.Fatalf("expected first cancel accepted, got %d body=%s", firstCancelW.Code, firstCancelW.Body.String())
	}

	secondCancelReq := httptest.NewRequest(http.MethodPost, "/v1/admin/tasks/"+promoted.TaskID+"/cancel", nil)
	secondCancelW := httptest.NewRecorder()
	router.ServeHTTP(secondCancelW, secondCancelReq)
	if secondCancelW.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for terminal task cancel, got %d body=%s", secondCancelW.Code, secondCancelW.Body.String())
	}
}

func TestSubmitEventRejectsRouteCriticalInsideBody(t *testing.T) {
	_, st, _, _, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	body := []byte(`{"input_kind":"repo_issue_comment","body_schema_id":"repo-issue-comment.v1","body":{"comment_text":"hello","reply_to_event_id":"evt_1"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for route-critical field inside body, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestResolveWaitPatchValidationAndSchemaCheck(t *testing.T) {
	_, st, runtime, reception, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	promoted, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "repo_comment",
		RepoRef:    "github:alice/repo",
		IssueRef:   "ops-patch-101",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.Promoted || strings.TrimSpace(promoted.TaskID) == "" {
		t.Fatalf("expected promoted active task, got %+v", promoted)
	}

	waitPayload, _ := json.Marshal(domain.HumanWaitRecordedPayload{
		HumanWaitID:     "wait_ops_patch_1",
		TaskID:          promoted.TaskID,
		StepExecutionID: "exec_ops_patch_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputSchemaID:   "recovery.schedule_trigger",
		ResumeOptions:   []string{"provide_input"},
		PromptRef:       "prompt:ops-patch",
		DeadlineAt:      time.Now().UTC().Add(time.Hour),
	})
	waitOpen := domain.EventEnvelope{
		EventID:         "evt_wait_ops_patch_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     promoted.TaskID,
		EventType:       domain.EventTypeHumanWaitRecorded,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T12:00:00.000000000Z#2001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.human_wait_recorded",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         waitPayload,
	}
	if err := st.AppendBatch(context.Background(), []domain.EventEnvelope{waitOpen}); err != nil {
		t.Fatal(err)
	}

	invalidBody := []byte(`{"human_wait_id":"wait_ops_patch_1","task_id":"` + promoted.TaskID + `","waiting_reason":"WaitingInput","decision":"provide-input","input_patch":{"reason":null}}`)
	invalidReq := httptest.NewRequest(http.MethodPost, "/v1/admin/resolve/wait", bytes.NewReader(invalidBody))
	invalidW := httptest.NewRecorder()
	router.ServeHTTP(invalidW, invalidReq)
	if invalidW.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 for invalid patched document, got %d body=%s", invalidW.Code, invalidW.Body.String())
	}

	validBody := []byte(`{"human_wait_id":"wait_ops_patch_1","task_id":"` + promoted.TaskID + `","waiting_reason":"WaitingInput","decision":"provide-input","input_patch":{"reason":"fixed by human"}}`)
	validReq := httptest.NewRequest(http.MethodPost, "/v1/admin/resolve/wait", bytes.NewReader(validBody))
	validW := httptest.NewRecorder()
	router.ServeHTTP(validW, validReq)
	if validW.Code != http.StatusAccepted {
		t.Fatalf("expected resolve wait accepted with valid patch, got %d body=%s", validW.Code, validW.Body.String())
	}

	var response domain.WriteAcceptedResponse
	if err := json.Unmarshal(validW.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(response.CommitHLC) == "" {
		t.Fatalf("resolve wait response should include commit_hlc")
	}
	if _, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  "cli",
		ConversationID: "after_wait_patch",
		ThreadID:       "root",
		ReceivedAt:     time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestResolveWaitRejectsMissingInputDraftBase(t *testing.T) {
	_, st, runtime, reception, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	promoted, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "repo_comment",
		RepoRef:    "github:alice/repo",
		IssueRef:   "ops-patch-missing-base-1",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.Promoted || strings.TrimSpace(promoted.TaskID) == "" {
		t.Fatalf("expected promoted active task, got %+v", promoted)
	}

	waitPayload, _ := json.Marshal(domain.HumanWaitRecordedPayload{
		HumanWaitID:     "wait_ops_missing_base_1",
		TaskID:          promoted.TaskID,
		StepExecutionID: "exec_ops_missing_base_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputSchemaID:   "recovery.schedule_trigger",
		ResumeOptions:   []string{"provide_input"},
		PromptRef:       "",
		DeadlineAt:      time.Now().UTC().Add(time.Hour),
	})
	waitOpen := domain.EventEnvelope{
		EventID:         "evt_wait_ops_missing_base_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     promoted.TaskID,
		EventType:       domain.EventTypeHumanWaitRecorded,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T12:10:00.000000000Z#2101",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.human_wait_recorded",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         waitPayload,
	}
	if err := st.AppendBatch(context.Background(), []domain.EventEnvelope{waitOpen}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"human_wait_id":"wait_ops_missing_base_1","task_id":"` + promoted.TaskID + `","waiting_reason":"WaitingInput","decision":"provide-input","input_patch":{"reason":"resume"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/resolve/wait", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 when wait input draft is unavailable, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEventTransportKindIsStableField(t *testing.T) {
	_, st, _, _, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	body := []byte(`{"input_kind":"web_form_message","body_schema_id":"web-form-message.v1","conversation_id":"conv_transport_kind_1","body":{"text":"hello transport"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/submit/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected submit event accepted, got %d body=%s", w.Code, w.Body.String())
	}
	var accepted domain.WriteAcceptedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.EventID == "" {
		t.Fatalf("missing event_id in accepted response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/events/"+accepted.EventID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("expected get event 200, got %d body=%s", getW.Code, getW.Body.String())
	}
	var eventResp domain.GetResponse[EventView]
	if err := json.Unmarshal(getW.Body.Bytes(), &eventResp); err != nil {
		t.Fatal(err)
	}
	if eventResp.Item.External == nil {
		t.Fatalf("expected external event data")
	}
	if eventResp.Item.External.TransportKind != "cli_admin_injected" {
		t.Fatalf("transport_kind should come from stored field, got %s", eventResp.Item.External.TransportKind)
	}
}

func TestListFiltersAppliedOnRequestsTasksAndEvents(t *testing.T) {
	_, st, runtime, reception, router := newHTTPManagerTestFixture(t)
	defer st.Close()

	if _, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  "cli",
		ConversationID: "conv_filter_match",
		ThreadID:       "root",
		ActorRef:       "alice",
		TraceID:        "trace_filter_1",
		ReceivedAt:     time.Now().UTC(),
	}, reception); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.IngestExternalEvent(context.Background(), domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  "cli",
		ConversationID: "conv_filter_other",
		ThreadID:       "root",
		ActorRef:       "bob",
		TraceID:        "trace_filter_2",
		ReceivedAt:     time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}

	reqList := httptest.NewRequest(http.MethodGet, "/v1/requests?conversation_id=conv_filter_match&actor=alice", nil)
	reqListW := httptest.NewRecorder()
	router.ServeHTTP(reqListW, reqList)
	if reqListW.Code != http.StatusOK {
		t.Fatalf("requests list failed: %d %s", reqListW.Code, reqListW.Body.String())
	}
	var reqListResp domain.ListResponse[RequestView]
	if err := json.Unmarshal(reqListW.Body.Bytes(), &reqListResp); err != nil {
		t.Fatal(err)
	}
	for _, item := range reqListResp.Items {
		if item.ConversationID != "conv_filter_match" || item.ActorRef != "alice" {
			t.Fatalf("unexpected request list filter result: %+v", item)
		}
	}

	eventListReq := httptest.NewRequest(http.MethodGet, "/v1/events?source_kind=direct_input&trace_id=trace_filter_1", nil)
	eventListW := httptest.NewRecorder()
	router.ServeHTTP(eventListW, eventListReq)
	if eventListW.Code != http.StatusOK {
		t.Fatalf("events list failed: %d %s", eventListW.Code, eventListW.Body.String())
	}
	var eventListResp domain.ListResponse[EventView]
	if err := json.Unmarshal(eventListW.Body.Bytes(), &eventListResp); err != nil {
		t.Fatal(err)
	}
	for _, item := range eventListResp.Items {
		if item.External == nil {
			continue
		}
		if item.External.SourceKind != "direct_input" || item.TraceID != "trace_filter_1" {
			t.Fatalf("unexpected event list filter result: %+v", item)
		}
	}
}

func TestDeadletterRedriveRequiresHook(t *testing.T) {
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
	mgr := NewHTTPManager(st, nil, nil, AdminHooks{}, SurfaceConfig{
		AdminEventInjectionEnabled:     true,
		AdminScheduleFireReplayEnabled: true,
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	mgr.RegisterRoutesGin(router)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/deadletters/dl_1/redrive", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected redrive without hook to be rejected, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDeadletterRedriveRejectsNonRetryableDeadletter(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	receiptPayload, _ := json.Marshal(domain.OutboxReceiptRecordedPayload{
		TaskID:        "task_dl_1",
		ActionID:      "action_dl_1",
		ReceiptKind:   "mcp",
		ReceiptStatus: "dead",
		ErrorMessage:  "permanent failure",
		RecordedAt:    time.Now().UTC(),
	})
	deadEvent := domain.EventEnvelope{
		EventID:         "evt_dl_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "task_dl_1",
		EventType:       domain.EventTypeOutboxReceiptRecorded,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T12:30:00.000000000Z#3001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.outbox_receipt_recorded",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         receiptPayload,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{deadEvent}); err != nil {
		t.Fatal(err)
	}

	hookCalled := false
	mgr := NewHTTPManager(st, nil, nil, AdminHooks{
		RedriveDeadletter: func(_ *http.Request, _ string) error {
			hookCalled = true
			return nil
		},
	}, SurfaceConfig{})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	mgr.RegisterRoutesGin(router)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/deadletters/dl_action_dl_1/redrive", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected non-retryable deadletter to be rejected, got %d body=%s", w.Code, w.Body.String())
	}
	if hookCalled {
		t.Fatalf("redrive hook should not be called for non-retryable deadletter")
	}
}
