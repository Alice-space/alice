package bus

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alice/internal/domain"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

type fixedReception struct {
	decision domain.PromotionDecision
}

func (f fixedReception) Assess(context.Context, domain.ReceptionInput) (*domain.PromotionDecision, error) {
	cp := f.decision
	return &cp, nil
}

func newTestRuntime(t *testing.T, st *store.Store) *Runtime {
	t.Helper()
	reg := workflow.NewRegistry(nil)
	workflowRoot := filepath.Join("..", "..", "configs", "workflows")
	if err := reg.LoadRoots(context.Background(), []string{workflowRoot}); err != nil {
		t.Fatal(err)
	}
	return NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		Config{ShardCount: 8},
		nil,
	)
}

func TestPromoteAndBindWorkflowPath(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, err := store.Open(store.Config{RootDir: tmp, SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	result, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "github",
		SourceRef:      "issue",
		RepoRef:        "github:alice/repo",
		IssueRef:       "12",
		ConversationID: "c1",
		ThreadID:       "t1",
		ReceivedAt:     time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Promoted || result.TaskID == "" {
		t.Fatalf("expected promoted task, got %+v", result)
	}

	events := []domain.EventEnvelope{}
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	hasReqPromoted := false
	hasTaskBound := false
	for _, evt := range events {
		if evt.EventType == domain.EventTypeRequestPromoted {
			hasReqPromoted = true
		}
		if evt.EventType == domain.EventTypeTaskPromotedAndBound {
			hasTaskBound = true
		}
	}
	if !hasReqPromoted || !hasTaskBound {
		t.Fatalf("expected promoted+bound events, got %v", events)
	}
	enc := domain.NewCanonicalRouteKeyEncoder()
	target, err := st.Indexes.GetRouteTarget(ctx, enc.RepoIssue("github:alice/repo", "12"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != domain.RouteTargetTask || target.ID != result.TaskID {
		t.Fatalf("route should point to task %s, got %+v", result.TaskID, target)
	}
}

func TestTaskRouteHitDoesNotCreateNewRequest(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	first, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "github",
		RepoRef:    "github:alice/repo",
		IssueRef:   "42",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Promoted {
		t.Fatalf("expected first event promoted")
	}
	second, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "github",
		RepoRef:    "github:alice/repo",
		IssueRef:   "42",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if second.TaskID != first.TaskID {
		t.Fatalf("expected existing task route hit: got %s want %s", second.TaskID, first.TaskID)
	}

	reqOpened := 0
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeEphemeralRequestOpened {
			reqOpened++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if reqOpened != 1 {
		t.Fatalf("expected single request opened, got %d", reqOpened)
	}

	humanEventID := "evt_human_1"
	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventID:         humanEventID,
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human-action",
		ActionKind:      string(domain.HumanActionCancel),
		StepExecutionID: "exec_cancel_1",
		ReplyToEventID:  "",
		RepoRef:         "github:alice/repo",
		IssueRef:        "42",
		ReceivedAt:      time.Now().UTC(),
	}, reception); err != nil {
		t.Fatal(err)
	}
	stepCancelled := false
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeStepExecutionCancelled {
			stepCancelled = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !stepCancelled {
		t.Fatalf("expected step cancellation event for human-action task hit")
	}
}

func TestDirectAnswerPathClosesRequest(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := fixedReception{decision: domain.PromotionDecision{
		IntentKind:  "direct_query",
		Confidence:  0.9,
		DecisionID:  "dec_direct",
		ProducedAt:  time.Now().UTC(),
		ReasonCodes: []string{"test"},
	}}
	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "im",
		ConversationID: "conv-1",
		ThreadID:       "root",
		ReceivedAt:     time.Now().UTC(),
	}, reception); err != nil {
		t.Fatal(err)
	}

	var hasReply, hasTerminal, hasAnswered bool
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		switch evt.EventType {
		case domain.EventTypeReplyRecorded:
			hasReply = true
		case domain.EventTypeTerminalResultRecorded:
			hasTerminal = true
		case domain.EventTypeRequestAnswered:
			hasAnswered = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasReply || !hasTerminal || !hasAnswered {
		t.Fatalf("direct answer path is not closed: reply=%v terminal=%v answered=%v", hasReply, hasTerminal, hasAnswered)
	}
}

func TestDedupeDropsDuplicateIngress(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := fixedReception{decision: domain.PromotionDecision{
		IntentKind: "direct_query", Confidence: 0.9, DecisionID: "dec_1", ProducedAt: time.Now().UTC(),
	}}
	evt := domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "web",
		ConversationID: "c1",
		ThreadID:       "t1",
		IdempotencyKey: "msg-1",
		ReceivedAt:     time.Now().UTC(),
	}
	if _, err := runtime.IngestExternalEvent(ctx, evt, reception); err != nil {
		t.Fatal(err)
	}
	second, err := runtime.IngestExternalEvent(ctx, evt, reception)
	if err != nil {
		t.Fatal(err)
	}
	if second.RouteMatched != "dedupe" {
		t.Fatalf("expected dedupe hit, got %s", second.RouteMatched)
	}
	count := 0
	if err := st.Replay(ctx, "", func(domain.EventEnvelope) error {
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count == 0 || count > 10 {
		t.Fatalf("unexpected event count after dedupe test: %d", count)
	}
}

func TestSequenceRestoredAcrossRuntimeRestart(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r1 := newTestRuntime(t, st)
	first, err := r1.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "web",
		ConversationID: "conv-seq",
		ThreadID:       "root",
		ReceivedAt:     time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	maxSeq := uint64(0)
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.AggregateID == first.RequestID && evt.Sequence > maxSeq {
			maxSeq = evt.Sequence
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	r2 := newTestRuntime(t, st)
	if err := r2.RestoreStateFromLog(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "admin",
		RequestID:  first.RequestID,
		ReceivedAt: time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}

	restoredSeq := uint64(0)
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.AggregateID == first.RequestID && evt.Sequence > restoredSeq {
			restoredSeq = evt.Sequence
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if restoredSeq <= maxSeq {
		t.Fatalf("sequence did not continue after restart: before=%d after=%d", maxSeq, restoredSeq)
	}
}

func TestScheduleTriggerWritesSchedulePayloadAndPromotes(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)

	registered, err := runtime.newEnvelope(domain.AggregateKindTask, "sch_1", domain.EventTypeScheduledTaskRegistered, domain.ScheduledTaskRegisteredPayload{
		ScheduledTaskID:      "sch_1",
		SpecKind:             "cron",
		SpecText:             "*/5 * * * *",
		Timezone:             "UTC",
		ScheduleRevision:     "rev-1",
		TargetWorkflowID:     "issue-delivery",
		TargetWorkflowSource: "file://configs/workflows/issue-delivery/manifest.yaml",
		TargetWorkflowRev:    "v1",
		Enabled:              true,
		NextFireAt:           time.Now().UTC(),
		RegisteredAt:         time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{registered}); err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.RecordScheduleFire(ctx, domain.RecordScheduleFireCommand{
		ScheduledTaskID:       "sch_1",
		ScheduledForWindowUTC: time.Now().UTC().Truncate(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	hasSchedulePayload := false
	hasPromotedAndBound := false
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		switch evt.EventType {
		case domain.EventTypeScheduleTriggered:
			hasSchedulePayload = true
			var payload domain.ScheduleTriggeredPayload
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				return err
			}
			if payload.ScheduledTaskID != "sch_1" {
				t.Fatalf("unexpected schedule payload task id: %s", payload.ScheduledTaskID)
			}
		case domain.EventTypeTaskPromotedAndBound:
			hasPromotedAndBound = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasSchedulePayload || !hasPromotedAndBound {
		t.Fatalf("schedule trigger pipeline incomplete: payload=%v promote=%v", hasSchedulePayload, hasPromotedAndBound)
	}
}

func TestScheduleTriggerMissingSourceRejected(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)

	if _, err := runtime.RecordScheduleFire(ctx, domain.RecordScheduleFireCommand{
		ScheduledTaskID:       "sch_missing",
		ScheduledForWindowUTC: time.Now().UTC().Truncate(time.Minute),
	}); err == nil {
		t.Fatalf("expected missing schedule source rejection")
	}
	hasWait := false
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeHumanWaitRecorded {
			hasWait = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if hasWait {
		t.Fatalf("should not append human wait for rejected scheduler fire")
	}
}

func TestRouteByRequestRecoveredForRouteRevocation(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r1 := newTestRuntime(t, st)
	openResult, err := r1.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "im",
		ConversationID: "conv-r",
		ThreadID:       "root",
		ReceivedAt:     time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if openResult.RequestID == "" {
		t.Fatalf("request id should be set")
	}

	r2 := newTestRuntime(t, st)
	if err := r2.RestoreStateFromLog(ctx); err != nil {
		t.Fatal(err)
	}
	ref, ok := r2.workflow.Registry().Reference("issue-delivery", "v1")
	if !ok {
		t.Fatalf("missing workflow ref")
	}
	if err := r2.PromoteAndBindWorkflow(ctx, domain.PromoteAndBindWorkflowCommand{
		RequestID:        openResult.RequestID,
		TaskID:           "task_recover_1",
		BindingID:        "bind_recover_1",
		WorkflowID:       ref.WorkflowID,
		WorkflowSource:   ref.WorkflowSource,
		WorkflowRev:      ref.WorkflowRev,
		ManifestDigest:   ref.ManifestDigest,
		EntryStepID:      "triage",
		RouteSnapshotRef: "route_snapshot:" + openResult.RequestID,
		At:               time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	enc := domain.NewCanonicalRouteKeyEncoder()
	target, err := st.Indexes.GetRouteTarget(ctx, enc.Conversation("im", "conv-r", "root"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Found() {
		t.Fatalf("conversation route key should be revoked after promotion, got %+v", target)
	}
}

func TestHumanActionApproveResolvesGateAndResumes(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	first, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "github",
		RepoRef:    "github:alice/repo",
		IssueRef:   "73",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Promoted {
		t.Fatalf("expected promoted task")
	}
	approveOpen, err := runtime.newEnvelope(domain.AggregateKindTask, first.TaskID, domain.EventTypeApprovalRequestOpened, domain.ApprovalRequestOpenedPayload{
		ApprovalRequestID: "apr_73",
		TaskID:            first.TaskID,
		StepExecutionID:   "exec_73",
		GateType:          "approval",
		RequiredSlots:     []string{"owner"},
		DeadlineAt:        time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{approveOpen}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "human-action",
		ActionKind:        string(domain.HumanActionApprove),
		TaskID:            first.TaskID,
		ApprovalRequestID: "apr_73",
		StepExecutionID:   "exec_73",
		WaitingReason:     string(domain.WaitingReasonInput),
		IdempotencyKey:    "human-approve-73",
		ReceivedAt:        time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	var hasResolved, hasResumed bool
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		switch evt.EventType {
		case domain.EventTypeApprovalRequestResolved:
			hasResolved = true
		case domain.EventTypeTaskResumed:
			hasResumed = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !hasResolved || !hasResumed {
		t.Fatalf("expected approval resolved and task resumed events, got resolved=%v resumed=%v", hasResolved, hasResumed)
	}
}

func TestHumanActionRequiresActiveWaitOrGate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	first, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "github",
		RepoRef:    "github:alice/repo",
		IssueRef:   "74",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human-action",
		ActionKind:      string(domain.HumanActionProvideInput),
		TaskID:          first.TaskID,
		HumanWaitID:     "wait-missing",
		StepExecutionID: "exec_74",
		WaitingReason:   string(domain.WaitingReasonInput),
		IdempotencyKey:  "human-wait-missing",
		ReceivedAt:      time.Now().UTC(),
	}, nil); err == nil {
		t.Fatalf("expected missing wait to be rejected")
	}
}

func TestExplicitRouteRequiresActiveObject(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)

	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "web",
		TaskID:     "task_not_found",
		ReceivedAt: time.Now().UTC(),
	}, nil); err == nil {
		t.Fatalf("expected inactive task route to be rejected")
	}
	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "web",
		RequestID:  "req_not_found",
		ReceivedAt: time.Now().UTC(),
	}, nil); err == nil {
		t.Fatalf("expected inactive request route to be rejected")
	}
}

func TestEventContractRejectsExternalEventIngestedOnTaskAggregate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	_, err = runtime.newEnvelope(domain.AggregateKindTask, "task_1", domain.EventTypeExternalEventIngested, domain.ExternalEventIngestedPayload{})
	if err == nil {
		t.Fatalf("expected aggregate contract mismatch error")
	}
	_ = ctx
}

func TestDedupeResultKeepsCommitAndRouteTarget(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)

	evt := domain.ExternalEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  "cli",
		ConversationID: "conv_dedupe_contract",
		ThreadID:       "root",
		IdempotencyKey: "idem_dedupe_contract",
		ReceivedAt:     time.Now().UTC(),
	}
	first, err := runtime.IngestExternalEvent(ctx, evt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(first.CommitHLC) == "" || strings.TrimSpace(first.RouteTargetKind) == "" || strings.TrimSpace(first.RouteTargetID) == "" {
		t.Fatalf("first write must include commit and route target: %+v", first)
	}

	second, err := runtime.IngestExternalEvent(ctx, evt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.RouteMatched != "dedupe" {
		t.Fatalf("expected dedupe route matched, got %s", second.RouteMatched)
	}
	if strings.TrimSpace(second.CommitHLC) == "" || strings.TrimSpace(second.RouteTargetKind) == "" || strings.TrimSpace(second.RouteTargetID) == "" {
		t.Fatalf("dedupe result must keep commit and route target: %+v", second)
	}
	if second.CommitHLC != first.CommitHLC {
		t.Fatalf("dedupe commit_hlc mismatch: got=%s want=%s", second.CommitHLC, first.CommitHLC)
	}
	if second.EventID != first.EventID {
		t.Fatalf("dedupe event_id mismatch: got=%s want=%s", second.EventID, first.EventID)
	}
}

func TestCancelledTaskRevokesRouteKeys(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	first, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "repo_comment",
		RepoRef:    "github:alice/repo",
		IssueRef:   "route-revoke-1",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Promoted || first.TaskID == "" {
		t.Fatalf("expected promoted task, got %+v", first)
	}

	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human-action",
		TransportKind:   "cli_admin",
		ActionKind:      string(domain.HumanActionCancel),
		TaskID:          first.TaskID,
		StepExecutionID: "exec_cancel_route",
		ReceivedAt:      time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}

	enc := domain.NewCanonicalRouteKeyEncoder()
	target, err := st.Indexes.GetRouteTarget(ctx, enc.RepoIssue("github:alice/repo", "route-revoke-1"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Found() {
		t.Fatalf("task route key should be revoked after cancel, got %+v", target)
	}

	next, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "repo_comment",
		RepoRef:    "github:alice/repo",
		IssueRef:   "route-revoke-1",
		ReceivedAt: time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.TaskID != "" || next.RequestID == "" {
		t.Fatalf("expected new request route after cancelled task, got %+v", next)
	}
}

func TestProvideInputRequiresValidMergePatchAgainstSchema(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	first, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "github",
		RepoRef:    "github:alice/repo",
		IssueRef:   "patch-validate-1",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Promoted || first.TaskID == "" {
		t.Fatalf("expected promoted task, got %+v", first)
	}

	waitPayload, _ := json.Marshal(domain.HumanWaitRecordedPayload{
		HumanWaitID:     "wait_patch_1",
		TaskID:          first.TaskID,
		StepExecutionID: "exec_patch_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputSchemaID:   "recovery.schedule_trigger",
		ResumeOptions:   []string{"provide_input"},
		PromptRef:       "prompt:patch",
		DeadlineAt:      time.Now().UTC().Add(time.Hour),
	})
	waitOpen := domain.EventEnvelope{
		EventID:         "evt_wait_patch_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     first.TaskID,
		EventType:       domain.EventTypeHumanWaitRecorded,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T11:00:00.000000000Z#1001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.human_wait_recorded",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         waitPayload,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{waitOpen}); err != nil {
		t.Fatal(err)
	}

	_, err = runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human-action",
		TransportKind:   "cli_admin",
		ActionKind:      string(domain.HumanActionProvideInput),
		TaskID:          first.TaskID,
		HumanWaitID:     "wait_patch_1",
		StepExecutionID: "exec_patch_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputPatch:      json.RawMessage(`{"reason":null}`),
		ReceivedAt:      time.Now().UTC(),
	}, nil)
	if err == nil {
		t.Fatalf("expected schema validation error for missing reason field")
	}

	if _, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human-action",
		TransportKind:   "cli_admin",
		ActionKind:      string(domain.HumanActionProvideInput),
		TaskID:          first.TaskID,
		HumanWaitID:     "wait_patch_1",
		StepExecutionID: "exec_patch_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputPatch:      json.RawMessage(`{"reason":"approved by human"}`),
		ReceivedAt:      time.Now().UTC(),
	}, nil); err != nil {
		t.Fatalf("expected valid patch to pass, got %v", err)
	}
}

func TestProvideInputRejectsWhenWaitInputDraftIsUnavailable(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	first, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:  domain.EventTypeExternalEventIngested,
		SourceKind: "github",
		RepoRef:    "github:alice/repo",
		IssueRef:   "patch-missing-draft-1",
		ReceivedAt: time.Now().UTC(),
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Promoted || first.TaskID == "" {
		t.Fatalf("expected promoted task, got %+v", first)
	}

	waitPayload, _ := json.Marshal(domain.HumanWaitRecordedPayload{
		HumanWaitID:     "wait_missing_draft_1",
		TaskID:          first.TaskID,
		StepExecutionID: "exec_missing_draft_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputSchemaID:   "recovery.schedule_trigger",
		ResumeOptions:   []string{"provide_input"},
		PromptRef:       "",
		DeadlineAt:      time.Now().UTC().Add(time.Hour),
	})
	waitOpen := domain.EventEnvelope{
		EventID:         "evt_wait_missing_draft_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     first.TaskID,
		EventType:       domain.EventTypeHumanWaitRecorded,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T11:30:00.000000000Z#1101",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.human_wait_recorded",
		PayloadVersion:  domain.DefaultPayloadVersion,
		Payload:         waitPayload,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{waitOpen}); err != nil {
		t.Fatal(err)
	}

	_, err = runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:       domain.EventTypeExternalEventIngested,
		SourceKind:      "human-action",
		TransportKind:   "cli_admin",
		ActionKind:      string(domain.HumanActionProvideInput),
		TaskID:          first.TaskID,
		HumanWaitID:     "wait_missing_draft_1",
		StepExecutionID: "exec_missing_draft_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputPatch:      json.RawMessage(`{"reason":"resume now"}`),
		ReceivedAt:      time.Now().UTC(),
	}, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "input draft is unavailable") {
		t.Fatalf("expected unavailable input draft error, got %v", err)
	}
}

func TestReplyToRouteDefersToDifferentGovernanceDomainTarget(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runtime := newTestRuntime(t, st)
	reception := policy.NewStaticReception(domain.NewULIDGenerator())

	replySeed, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:         domain.EventTypeExternalEventIngested,
		SourceKind:        "repo_comment",
		ReplyToEventID:    "parent_reply_domain_1",
		RepoRef:           "github:alice/repo",
		IssueRef:          "domain-reply-seed",
		ConversationID:    "conv_domain_reply_1",
		ThreadID:          "root",
		TransportKind:     "cli",
		ReceivedAt:        time.Now().UTC(),
		IdempotencyKey:    "idem_reply_seed_1",
		WorkflowObjectRef: "",
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !replySeed.Promoted || replySeed.TaskID == "" {
		t.Fatalf("expected promoted reply seed task, got %+v", replySeed)
	}

	controlSeed, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:        domain.EventTypeExternalEventIngested,
		SourceKind:       "control_plane",
		ControlObjectRef: "control://domain/42",
		ConversationID:   "conv_domain_control_1",
		ThreadID:         "root",
		TransportKind:    "cli",
		ReceivedAt:       time.Now().UTC(),
		IdempotencyKey:   "idem_control_seed_1",
	}, reception)
	if err != nil {
		t.Fatal(err)
	}
	if !controlSeed.Promoted || controlSeed.TaskID == "" {
		t.Fatalf("expected promoted control seed task, got %+v", controlSeed)
	}
	if controlSeed.TaskID == replySeed.TaskID {
		t.Fatalf("expected different tasks for reply/control seeds, got one task=%s", controlSeed.TaskID)
	}

	routed, err := runtime.IngestExternalEvent(ctx, domain.ExternalEvent{
		EventType:        domain.EventTypeExternalEventIngested,
		SourceKind:       "control_plane",
		ReplyToEventID:   "parent_reply_domain_1",
		ControlObjectRef: "control://domain/42",
		TransportKind:    "cli",
		ReceivedAt:       time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if routed.TaskID != controlSeed.TaskID {
		t.Fatalf("expected governance target task %s, got %+v", controlSeed.TaskID, routed)
	}
	if routed.RouteMatched != "control_object_ref" {
		t.Fatalf("expected control_object_ref route match, got %+v", routed)
	}
}
