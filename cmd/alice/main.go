package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"alice/internal/app"
	"alice/internal/domain"
	"alice/internal/platform"
)

const (
	defaultConfigPath = "configs/alice.yaml"
	defaultServerURL  = "http://127.0.0.1:8080"
)

type clientConfig struct {
	serverURL string
	token     string
	output    string
	timeout   time.Duration
	traceID   string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := flag.NewFlagSet("alice", flag.ContinueOnError)
	root.SetOutput(io.Discard)
	configPath := root.String("config", defaultConfigPath, "path to config file")
	serverURL := root.String("server", envOrDefault("ALICE_SERVER", defaultServerURL), "alice server base url")
	token := root.String("token", envOrDefault("ALICE_TOKEN", ""), "admin token")
	output := root.String("output", envOrDefault("ALICE_OUTPUT", "text"), "output format: text|json|ndjson")
	timeout := root.Duration("timeout", envDurationOrDefault("ALICE_TIMEOUT", 15*time.Second), "http timeout")
	traceID := root.String("trace-id", envOrDefault("ALICE_TRACE_ID", ""), "trace id")
	if err := root.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		printUsage(os.Stderr)
		return 1
	}
	rest := root.Args()
	if len(rest) == 0 {
		printUsage(os.Stderr)
		return 1
	}
	if rest[0] == "serve" {
		return runServe(*configPath, rest[1:])
	}
	cfg := clientConfig{
		serverURL: strings.TrimRight(strings.TrimSpace(*serverURL), "/"),
		token:     strings.TrimSpace(*token),
		output:    strings.TrimSpace(*output),
		timeout:   *timeout,
		traceID:   strings.TrimSpace(*traceID),
	}
	return runClient(cfg, rest)
}

func runServe(defaultConfig string, args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfig, "path to alice config")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	cfg, err := platform.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	application, err := app.Bootstrap(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if err := application.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	return 0
}

func runClient(cfg clientConfig, args []string) int {
	switch args[0] {
	case "submit":
		return runSubmit(cfg, args[1:])
	case "get":
		return runGet(cfg, args[1:])
	case "list":
		return runList(cfg, args[1:])
	case "resolve":
		return runResolve(cfg, args[1:])
	case "cancel":
		return runCancel(cfg, args[1:])
	case "admin":
		return runAdmin(cfg, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage(os.Stderr)
		return 1
	}
}

func runSubmit(cfg clientConfig, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing submit subcommand")
		return 1
	}
	switch args[0] {
	case "message":
		fs := flag.NewFlagSet("submit message", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		text := fs.String("text", "", "message text")
		actor := fs.String("actor", "", "actor_ref")
		sourceRef := fs.String("source-ref", "", "source_ref")
		idempotencyKey := fs.String("idempotency-key", "", "idempotency_key")
		conversationID := fs.String("conversation-id", "", "conversation_id")
		threadID := fs.String("thread-id", "", "thread_id")
		repoRef := fs.String("repo", "", "repo_ref")
		replyToEventID := fs.String("reply-to-event", "", "reply_to_event_id")
		traceID := fs.String("trace-id", "", "trace_id override")
		wait := fs.Bool("wait", false, "wait until read model catches up to commit_hlc")
		waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if strings.TrimSpace(*text) == "" {
			fmt.Fprintln(os.Stderr, "--text is required")
			return 1
		}
		body := domain.CLIMessageSubmitRequest{
			Text:           *text,
			ActorRef:       strings.TrimSpace(*actor),
			SourceRef:      strings.TrimSpace(*sourceRef),
			IdempotencyKey: strings.TrimSpace(*idempotencyKey),
			ConversationID: strings.TrimSpace(*conversationID),
			ThreadID:       strings.TrimSpace(*threadID),
			RepoRef:        strings.TrimSpace(*repoRef),
			ReplyToEventID: strings.TrimSpace(*replyToEventID),
			TraceID:        chooseNonEmpty(strings.TrimSpace(*traceID), cfg.traceID),
		}
		return requestWriteJSON(cfg, http.MethodPost, "/v1/ingress/cli/messages", nil, body, false, *wait, *waitTimeout)
	case "event":
		if strings.TrimSpace(cfg.token) == "" {
			fmt.Fprintln(os.Stderr, "--token is required for submit event")
			return 1
		}
		fs := flag.NewFlagSet("submit event", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		filePath := fs.String("file", "", "json file path (default: stdin)")
		actor := fs.String("actor", "", "actor_ref override")
		sourceRef := fs.String("source-ref", "", "source_ref override")
		idempotencyKey := fs.String("idempotency-key", "", "idempotency_key override")
		traceID := fs.String("trace-id", "", "trace_id override")
		wait := fs.Bool("wait", false, "wait until read model catches up to commit_hlc")
		waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		raw, err := loadJSONInput(*filePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		payload := map[string]any{}
		if err := json.Unmarshal(raw, &payload); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if v := strings.TrimSpace(*actor); v != "" {
			payload["actor_ref"] = v
		}
		if v := strings.TrimSpace(*sourceRef); v != "" {
			payload["source_ref"] = v
		}
		if v := strings.TrimSpace(*idempotencyKey); v != "" {
			payload["idempotency_key"] = v
		}
		if v := chooseNonEmpty(strings.TrimSpace(*traceID), cfg.traceID); v != "" {
			payload["trace_id"] = v
		}
		return requestWriteJSON(cfg, http.MethodPost, "/v1/admin/submit/events", nil, payload, true, *wait, *waitTimeout)
	case "fire":
		if strings.TrimSpace(cfg.token) == "" {
			fmt.Fprintln(os.Stderr, "--token is required for submit fire")
			return 1
		}
		fs := flag.NewFlagSet("submit fire", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scheduledTaskID := fs.String("scheduled-task-id", "", "scheduled_task_id")
		scheduledFor := fs.String("scheduled-for", "", "scheduled_for_window RFC3339")
		actor := fs.String("actor", "", "actor_ref")
		idempotencyKey := fs.String("idempotency-key", "", "idempotency_key")
		reason := fs.String("reason", "", "reason")
		traceID := fs.String("trace-id", "", "trace_id override")
		wait := fs.Bool("wait", false, "wait until read model catches up to commit_hlc")
		waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if strings.TrimSpace(*scheduledTaskID) == "" || strings.TrimSpace(*scheduledFor) == "" {
			fmt.Fprintln(os.Stderr, "--scheduled-task-id and --scheduled-for are required")
			return 1
		}
		scheduledForWindow, err := time.Parse(time.RFC3339, strings.TrimSpace(*scheduledFor))
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --scheduled-for: %v\n", err)
			return 1
		}
		body := domain.ScheduleFireReplayRequest{
			ScheduledTaskID:    strings.TrimSpace(*scheduledTaskID),
			ScheduledForWindow: scheduledForWindow,
			ActorRef:           strings.TrimSpace(*actor),
			IdempotencyKey:     strings.TrimSpace(*idempotencyKey),
			Reason:             strings.TrimSpace(*reason),
			TraceID:            chooseNonEmpty(strings.TrimSpace(*traceID), cfg.traceID),
		}
		return requestWriteJSON(cfg, http.MethodPost, "/v1/admin/submit/fires", nil, body, true, *wait, *waitTimeout)
	default:
		fmt.Fprintf(os.Stderr, "unknown submit subcommand: %s\n", args[0])
		return 1
	}
}

func runGet(cfg clientConfig, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing get subcommand")
		return 1
	}
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	minHLC := fs.String("min-hlc", "", "minimum visible hlc")
	waitTimeoutMS := fs.Int("wait-timeout-ms", 0, "wait timeout milliseconds")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	query := url.Values{}
	if v := strings.TrimSpace(*minHLC); v != "" {
		query.Set("min_hlc", v)
	}
	if *waitTimeoutMS > 0 {
		query.Set("wait_timeout_ms", strconv.Itoa(*waitTimeoutMS))
	}
	name := strings.TrimSpace(args[0])
	var path string
	if name == "ops" {
		path = "/v1/ops/overview"
	} else {
		rest := fs.Args()
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "missing object id")
			return 1
		}
		id := url.PathEscape(strings.TrimSpace(rest[0]))
		switch name {
		case "event":
			path = "/v1/events/" + id
		case "request":
			path = "/v1/requests/" + id
		case "task":
			path = "/v1/tasks/" + id
		case "schedule":
			path = "/v1/schedules/" + id
		case "approval":
			path = "/v1/approvals/" + id
		case "human-wait":
			path = "/v1/human-waits/" + id
		case "deadletter":
			path = "/v1/deadletters/" + id
		default:
			fmt.Fprintf(os.Stderr, "unknown get subcommand: %s\n", name)
			return 1
		}
	}
	return requestJSON(cfg, http.MethodGet, path, query, nil, false)
}

func runList(cfg clientConfig, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing list subcommand")
		return 1
	}
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 50, "page size")
	cursor := fs.String("cursor", "", "cursor")
	minHLC := fs.String("min-hlc", "", "minimum visible hlc")
	waitTimeoutMS := fs.Int("wait-timeout-ms", 0, "wait timeout milliseconds")
	status := fs.String("status", "", "status filter")
	conversationID := fs.String("conversation-id", "", "conversation_id filter")
	actor := fs.String("actor", "", "actor filter")
	updatedSince := fs.String("updated-since", "", "updated_since filter (RFC3339)")
	workflowID := fs.String("workflow-id", "", "workflow_id filter")
	repo := fs.String("repo", "", "repo filter")
	waitingReason := fs.String("waiting-reason", "", "waiting_reason filter")
	enabled := fs.String("enabled", "", "enabled filter (true|false)")
	timezone := fs.String("timezone", "", "timezone filter")
	entryKind := fs.String("entry-kind", "", "entry_kind filter")
	taskID := fs.String("task-id", "", "task_id filter")
	expiresBefore := fs.String("expires-before", "", "expires_before filter (RFC3339)")
	failureStage := fs.String("failure-stage", "", "failure_stage filter")
	retryable := fs.String("retryable", "", "retryable filter (true|false)")
	eventType := fs.String("event-type", "", "event_type filter")
	sourceKind := fs.String("source-kind", "", "source_kind filter")
	traceID := fs.String("trace-id", "", "trace_id filter")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	query := url.Values{}
	if *limit > 0 {
		query.Set("limit", strconv.Itoa(*limit))
	}
	if v := strings.TrimSpace(*cursor); v != "" {
		query.Set("cursor", v)
	}
	if v := strings.TrimSpace(*minHLC); v != "" {
		query.Set("min_hlc", v)
	}
	if *waitTimeoutMS > 0 {
		query.Set("wait_timeout_ms", strconv.Itoa(*waitTimeoutMS))
	}
	if v := strings.TrimSpace(*status); v != "" {
		query.Set("status", v)
	}
	if v := strings.TrimSpace(*conversationID); v != "" {
		query.Set("conversation_id", v)
	}
	if v := strings.TrimSpace(*actor); v != "" {
		query.Set("actor", v)
	}
	if v := strings.TrimSpace(*updatedSince); v != "" {
		query.Set("updated_since", v)
	}
	if v := strings.TrimSpace(*workflowID); v != "" {
		query.Set("workflow_id", v)
	}
	if v := strings.TrimSpace(*repo); v != "" {
		query.Set("repo", v)
	}
	if v := strings.TrimSpace(*waitingReason); v != "" {
		query.Set("waiting_reason", v)
	}
	if v := strings.TrimSpace(*enabled); v != "" {
		query.Set("enabled", v)
	}
	if v := strings.TrimSpace(*timezone); v != "" {
		query.Set("timezone", v)
	}
	if v := strings.TrimSpace(*entryKind); v != "" {
		query.Set("entry_kind", v)
	}
	if v := strings.TrimSpace(*taskID); v != "" {
		query.Set("task_id", v)
	}
	if v := strings.TrimSpace(*expiresBefore); v != "" {
		query.Set("expires_before", v)
	}
	if v := strings.TrimSpace(*failureStage); v != "" {
		query.Set("failure_stage", v)
	}
	if v := strings.TrimSpace(*retryable); v != "" {
		query.Set("retryable", v)
	}
	if v := strings.TrimSpace(*eventType); v != "" {
		query.Set("event_type", v)
	}
	if v := strings.TrimSpace(*sourceKind); v != "" {
		query.Set("source_kind", v)
	}
	if v := strings.TrimSpace(*traceID); v != "" {
		query.Set("trace_id", v)
	}
	var path string
	switch strings.TrimSpace(args[0]) {
	case "requests":
		path = "/v1/requests"
	case "tasks":
		path = "/v1/tasks"
	case "schedules":
		path = "/v1/schedules"
	case "human-actions":
		path = "/v1/human-actions"
	case "deadletters":
		path = "/v1/deadletters"
	case "events":
		path = "/v1/events"
	case "approvals":
		path = "/v1/approvals"
	case "human-waits":
		path = "/v1/human-waits"
	default:
		fmt.Fprintf(os.Stderr, "unknown list subcommand: %s\n", args[0])
		return 1
	}
	return requestJSON(cfg, http.MethodGet, path, query, nil, false)
}

func runResolve(cfg clientConfig, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing resolve subcommand")
		return 1
	}
	if strings.TrimSpace(cfg.token) == "" {
		fmt.Fprintln(os.Stderr, "--token is required for resolve commands")
		return 1
	}
	switch args[0] {
	case "approval":
		fs := flag.NewFlagSet("resolve approval", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		approvalID := fs.String("approval-request-id", "", "approval_request_id")
		taskID := fs.String("task-id", "", "task_id")
		stepExecutionID := fs.String("step-execution-id", "", "step_execution_id")
		decision := fs.String("decision", "", "approve|reject|confirm|resume-budget")
		actor := fs.String("actor", "", "actor_ref")
		idempotencyKey := fs.String("idempotency-key", "", "idempotency_key")
		note := fs.String("note", "", "note")
		traceID := fs.String("trace-id", "", "trace_id override")
		wait := fs.Bool("wait", false, "wait until read model catches up to commit_hlc")
		waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if strings.TrimSpace(*approvalID) == "" || strings.TrimSpace(*taskID) == "" || strings.TrimSpace(*stepExecutionID) == "" || strings.TrimSpace(*decision) == "" {
			fmt.Fprintln(os.Stderr, "--approval-request-id --task-id --step-execution-id --decision are required")
			return 1
		}
		body := domain.ResolveApprovalRequest{
			ApprovalRequestID: strings.TrimSpace(*approvalID),
			TaskID:            strings.TrimSpace(*taskID),
			StepExecutionID:   strings.TrimSpace(*stepExecutionID),
			Decision:          strings.TrimSpace(*decision),
			ActorRef:          strings.TrimSpace(*actor),
			IdempotencyKey:    strings.TrimSpace(*idempotencyKey),
			Note:              strings.TrimSpace(*note),
			TraceID:           chooseNonEmpty(strings.TrimSpace(*traceID), cfg.traceID),
		}
		return requestWriteJSON(cfg, http.MethodPost, "/v1/admin/resolve/approval", nil, body, true, *wait, *waitTimeout)
	case "wait":
		fs := flag.NewFlagSet("resolve wait", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		humanWaitID := fs.String("human-wait-id", "", "human_wait_id")
		taskID := fs.String("task-id", "", "task_id")
		stepExecutionID := fs.String("step-execution-id", "", "step_execution_id")
		waitingReason := fs.String("waiting-reason", "", "WaitingInput|WaitingRecovery")
		decision := fs.String("decision", "", "provide-input|resume-recovery|rewind")
		targetStepID := fs.String("target-step-id", "", "target_step_id")
		actor := fs.String("actor", "", "actor_ref")
		idempotencyKey := fs.String("idempotency-key", "", "idempotency_key")
		patchFile := fs.String("patch-file", "", "json merge patch file path")
		note := fs.String("note", "", "note")
		traceID := fs.String("trace-id", "", "trace_id override")
		wait := fs.Bool("wait", false, "wait until read model catches up to commit_hlc")
		waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if strings.TrimSpace(*humanWaitID) == "" || strings.TrimSpace(*taskID) == "" || strings.TrimSpace(*waitingReason) == "" || strings.TrimSpace(*decision) == "" {
			fmt.Fprintln(os.Stderr, "--human-wait-id --task-id --waiting-reason --decision are required")
			return 1
		}
		var patch json.RawMessage
		if v := strings.TrimSpace(*patchFile); v != "" {
			raw, err := loadJSONInput(v)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return 1
			}
			patch = raw
		}
		body := domain.ResolveHumanWaitRequest{
			HumanWaitID:     strings.TrimSpace(*humanWaitID),
			TaskID:          strings.TrimSpace(*taskID),
			StepExecutionID: strings.TrimSpace(*stepExecutionID),
			WaitingReason:   strings.TrimSpace(*waitingReason),
			Decision:        strings.TrimSpace(*decision),
			TargetStepID:    strings.TrimSpace(*targetStepID),
			ActorRef:        strings.TrimSpace(*actor),
			IdempotencyKey:  strings.TrimSpace(*idempotencyKey),
			InputPatch:      patch,
			Note:            strings.TrimSpace(*note),
			TraceID:         chooseNonEmpty(strings.TrimSpace(*traceID), cfg.traceID),
		}
		return requestWriteJSON(cfg, http.MethodPost, "/v1/admin/resolve/wait", nil, body, true, *wait, *waitTimeout)
	default:
		fmt.Fprintf(os.Stderr, "unknown resolve subcommand: %s\n", args[0])
		return 1
	}
}

func runCancel(cfg clientConfig, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing cancel subcommand")
		return 1
	}
	if strings.TrimSpace(cfg.token) == "" {
		fmt.Fprintln(os.Stderr, "--token is required for cancel commands")
		return 1
	}
	switch args[0] {
	case "task":
		fs := flag.NewFlagSet("cancel task", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		taskID := fs.String("task-id", "", "task_id")
		stepExecutionID := fs.String("step-execution-id", "", "step_execution_id")
		actor := fs.String("actor", "", "actor_ref")
		idempotencyKey := fs.String("idempotency-key", "", "idempotency_key")
		reason := fs.String("reason", "", "reason")
		traceID := fs.String("trace-id", "", "trace_id override")
		wait := fs.Bool("wait", false, "wait until read model catches up to commit_hlc")
		waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if strings.TrimSpace(*taskID) == "" {
			fmt.Fprintln(os.Stderr, "--task-id is required")
			return 1
		}
		body := domain.CancelTaskRequest{
			TaskID:          strings.TrimSpace(*taskID),
			StepExecutionID: strings.TrimSpace(*stepExecutionID),
			ActorRef:        strings.TrimSpace(*actor),
			IdempotencyKey:  strings.TrimSpace(*idempotencyKey),
			Reason:          strings.TrimSpace(*reason),
			TraceID:         chooseNonEmpty(strings.TrimSpace(*traceID), cfg.traceID),
		}
		return requestWriteJSON(cfg, http.MethodPost, "/v1/admin/tasks/"+url.PathEscape(strings.TrimSpace(*taskID))+"/cancel", nil, body, true, *wait, *waitTimeout)
	default:
		fmt.Fprintf(os.Stderr, "unknown cancel subcommand: %s\n", args[0])
		return 1
	}
}

func runAdmin(cfg clientConfig, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "missing admin subcommand")
		return 1
	}
	if strings.TrimSpace(cfg.token) == "" {
		fmt.Fprintln(os.Stderr, "--token is required for admin commands")
		return 1
	}
	switch args[0] {
	case "replay":
		fs := flag.NewFlagSet("admin replay", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fromHLC := fs.String("from-hlc", "", "starting hlc")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		if strings.TrimSpace(*fromHLC) == "" {
			fmt.Fprintln(os.Stderr, "--from-hlc is required")
			return 1
		}
		return requestJSON(cfg, http.MethodPost, "/v1/admin/replay/from/"+url.PathEscape(strings.TrimSpace(*fromHLC)), nil, nil, true)
	case "reconcile":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "missing admin reconcile target")
			return 1
		}
		switch args[1] {
		case "outbox":
			return requestJSON(cfg, http.MethodPost, "/v1/admin/reconcile/outbox", nil, nil, true)
		case "schedules":
			return requestJSON(cfg, http.MethodPost, "/v1/admin/reconcile/schedules", nil, nil, true)
		default:
			fmt.Fprintf(os.Stderr, "unknown admin reconcile target: %s\n", args[1])
			return 1
		}
	case "rebuild":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "missing admin rebuild target")
			return 1
		}
		switch args[1] {
		case "indexes":
			return requestJSON(cfg, http.MethodPost, "/v1/admin/rebuild/indexes", nil, nil, true)
		default:
			fmt.Fprintf(os.Stderr, "unknown admin rebuild target: %s\n", args[1])
			return 1
		}
	case "redrive":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "missing admin redrive target")
			return 1
		}
		switch args[1] {
		case "deadletter":
			fs := flag.NewFlagSet("admin redrive deadletter", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			deadletterID := fs.String("deadletter-id", "", "deadletter id")
			if err := fs.Parse(args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return 1
			}
			if strings.TrimSpace(*deadletterID) == "" {
				fmt.Fprintln(os.Stderr, "--deadletter-id is required")
				return 1
			}
			return requestJSON(cfg, http.MethodPost, "/v1/admin/deadletters/"+url.PathEscape(strings.TrimSpace(*deadletterID))+"/redrive", nil, nil, true)
		default:
			fmt.Fprintf(os.Stderr, "unknown admin redrive target: %s\n", args[1])
			return 1
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown admin subcommand: %s\n", args[0])
		return 1
	}
}

func requestJSON(cfg clientConfig, method, path string, query url.Values, body any, admin bool) int {
	statusCode, raw, err := doRequest(cfg, method, path, query, body, admin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 5
	}
	if statusCode >= 200 && statusCode < 300 {
		printResponse(os.Stdout, cfg.output, raw)
		return 0
	}
	printResponse(os.Stderr, "text", raw)
	return mapHTTPStatusToExit(statusCode)
}

func requestWriteJSON(cfg clientConfig, method, path string, query url.Values, body any, admin bool, wait bool, waitTimeout time.Duration) int {
	statusCode, raw, err := doRequest(cfg, method, path, query, body, admin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 5
	}
	if statusCode < 200 || statusCode >= 300 {
		printResponse(os.Stderr, "text", raw)
		return mapHTTPStatusToExit(statusCode)
	}
	printResponse(os.Stdout, cfg.output, raw)
	if !wait {
		return 0
	}
	var accepted domain.WriteAcceptedResponse
	if err := json.Unmarshal(raw, &accepted); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse write response for wait: %v\n", err)
		return 1
	}
	if waitTimeout <= 0 {
		waitTimeout = 30 * time.Second
	}
	waitStatus, waitErr := waitForVisibility(cfg, accepted, waitTimeout)
	if waitErr != nil {
		fmt.Fprintln(os.Stderr, waitErr.Error())
		if waitStatus > 0 {
			return waitStatus
		}
		return 5
	}
	return 0
}

func doRequest(cfg clientConfig, method, path string, query url.Values, body any, admin bool) (int, []byte, error) {
	if strings.TrimSpace(cfg.serverURL) == "" {
		return 0, nil, fmt.Errorf("--server is required")
	}
	if admin && strings.TrimSpace(cfg.token) == "" {
		return 0, nil, fmt.Errorf("--token is required")
	}
	fullURL := cfg.serverURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if admin {
		req.Header.Set("X-Admin-Token", cfg.token)
	}
	if strings.TrimSpace(cfg.traceID) != "" {
		req.Header.Set("X-Trace-ID", strings.TrimSpace(cfg.traceID))
	}
	resp, err := (&http.Client{Timeout: cfg.timeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, nil
}

func waitForVisibility(cfg clientConfig, accepted domain.WriteAcceptedResponse, timeout time.Duration) (int, error) {
	commitHLC := strings.TrimSpace(accepted.CommitHLC)
	if commitHLC == "" {
		return 1, fmt.Errorf("write response missing commit_hlc, cannot wait")
	}
	targetKind := strings.TrimSpace(accepted.RouteTargetKind)
	targetID := strings.TrimSpace(accepted.RouteTargetID)
	if targetKind == "" {
		if strings.TrimSpace(accepted.TaskID) != "" {
			targetKind = "task"
			targetID = strings.TrimSpace(accepted.TaskID)
		} else if strings.TrimSpace(accepted.RequestID) != "" {
			targetKind = "request"
			targetID = strings.TrimSpace(accepted.RequestID)
		}
	}
	if targetKind == "" || targetID == "" {
		return 1, fmt.Errorf("write response missing route target, cannot wait")
	}
	var readPath string
	switch strings.ToLower(targetKind) {
	case "request":
		readPath = "/v1/requests/" + url.PathEscape(targetID)
	case "task":
		readPath = "/v1/tasks/" + url.PathEscape(targetID)
	default:
		return 1, fmt.Errorf("unsupported route target kind for wait: %s", targetKind)
	}

	deadline := time.Now().UTC().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 5, fmt.Errorf("wait timed out before visible_hlc reached commit_hlc=%s", commitHLC)
		}
		waitMS := int(remaining.Milliseconds())
		if waitMS > 1000 {
			waitMS = 1000
		}
		query := url.Values{}
		query.Set("min_hlc", commitHLC)
		query.Set("wait_timeout_ms", strconv.Itoa(waitMS))
		statusCode, raw, err := doRequest(cfg, http.MethodGet, readPath, query, nil, false)
		if err != nil {
			return 5, err
		}
		if statusCode < 200 || statusCode >= 300 {
			printResponse(os.Stderr, "text", raw)
			return mapHTTPStatusToExit(statusCode), fmt.Errorf("wait read endpoint returned %d", statusCode)
		}
		visible := extractVisibleHLC(raw)
		if compareHLC(visible, commitHLC) >= 0 {
			return 0, nil
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func extractVisibleHLC(raw []byte) string {
	var payload struct {
		VisibleHLC string `json:"visible_hlc"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		return strings.TrimSpace(payload.VisibleHLC)
	}
	return ""
}

func compareHLC(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	at, an := parseHLC(a)
	bt, bn := parseHLC(b)
	if !at.IsZero() && !bt.IsZero() {
		if at.Before(bt) {
			return -1
		}
		if at.After(bt) {
			return 1
		}
		if an < bn {
			return -1
		}
		if an > bn {
			return 1
		}
		return 0
	}
	return strings.Compare(a, b)
}

func parseHLC(v string) (time.Time, int64) {
	parts := strings.Split(strings.TrimSpace(v), "#")
	if len(parts) != 2 {
		return time.Time{}, 0
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0
	}
	return t.UTC(), n
}

func printResponse(w io.Writer, mode string, raw []byte) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "text":
		var out bytes.Buffer
		if err := json.Indent(&out, trimmed, "", "  "); err == nil {
			_, _ = fmt.Fprintln(w, out.String())
			return
		}
		_, _ = fmt.Fprintln(w, string(trimmed))
	case "ndjson":
		_, _ = fmt.Fprintln(w, string(trimmed))
	default:
		_, _ = fmt.Fprintln(w, string(trimmed))
	}
}

func mapHTTPStatusToExit(status int) int {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return 2
	case status == http.StatusNotFound || status == http.StatusGone:
		return 3
	case status == http.StatusConflict || status == http.StatusPreconditionFailed:
		return 4
	case status >= 500:
		return 6
	default:
		return 1
	}
}

func loadJSONInput(filePath string) ([]byte, error) {
	p := strings.TrimSpace(filePath)
	if p == "" || p == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, errors.New("stdin is empty")
		}
		return raw, nil
	}
	resolved := p
	if !filepath.IsAbs(p) {
		resolved = filepath.Clean(p)
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("empty file: %s", resolved)
	}
	return raw, nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  alice serve")
	_, _ = fmt.Fprintln(w, "  alice submit message|event|fire ...")
	_, _ = fmt.Fprintln(w, "  alice get event|request|task|schedule|approval|human-wait|deadletter|ops ...")
	_, _ = fmt.Fprintln(w, "  alice list requests|tasks|schedules|human-actions|deadletters|events|approvals|human-waits ...")
	_, _ = fmt.Fprintln(w, "  alice resolve approval|wait ...")
	_, _ = fmt.Fprintln(w, "  alice cancel task ...")
	_, _ = fmt.Fprintln(w, "  alice admin replay|reconcile|rebuild|redrive ...")
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
