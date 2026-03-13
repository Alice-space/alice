package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"
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

type rootOptions struct {
	configPath string
	serverURL  string
	token      string
	output     string
	timeout    time.Duration
	traceID    string
}

type readQueryOptions struct {
	minHLC        string
	waitTimeoutMS int
}

type listQueryOptions struct {
	limit          int
	cursor         string
	minHLC         string
	waitTimeoutMS  int
	status         string
	conversationID string
	actor          string
	updatedSince   string
	workflowID     string
	repo           string
	waitingReason  string
	enabled        string
	timezone       string
	entryKind      string
	taskID         string
	expiresBefore  string
	failureStage   string
	retryable      string
	eventType      string
	sourceKind     string
	traceID        string
}

type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := newRootCmd()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if msg := strings.TrimSpace(err.Error()); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		return resolveExitCode(err)
	}
	return 0
}

func newRootCmd() *cobra.Command {
	opts := &rootOptions{}
	root := &cobra.Command{
		Use:              "alice",
		Short:            "Alice is a workflow automation platform",
		Long:             `Alice is a workflow automation platform with event-driven architecture.`,
		SilenceUsage:     true,
		SilenceErrors:    true,
		TraverseChildren: true,
	}

	flags := root.PersistentFlags()
	flags.StringVar(&opts.configPath, "config", defaultConfigPath, "path to config file")
	flags.StringVar(&opts.serverURL, "server", envOrDefault("ALICE_SERVER", defaultServerURL), "alice server base url")
	flags.StringVar(&opts.token, "token", envOrDefault("ALICE_TOKEN", ""), "admin token")
	flags.StringVar(&opts.output, "output", envOrDefault("ALICE_OUTPUT", "text"), "output format: text|json|ndjson")
	flags.DurationVar(&opts.timeout, "timeout", envDurationOrDefault("ALICE_TIMEOUT", 120*time.Second), "http timeout")
	flags.StringVar(&opts.traceID, "trace-id", envOrDefault("ALICE_TRACE_ID", ""), "trace id")

	root.AddCommand(
		newServeCmd(opts),
		newSubmitCmd(opts),
		newGetCmd(opts),
		newListCmd(opts),
		newResolveCmd(opts),
		newCancelCmd(opts),
		newAdminCmd(opts),
	)

	return root
}

func newServeCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Alice server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := platform.LoadConfig(opts.configPath)
			if err != nil {
				return commandError(1, "%v", err)
			}
			application, err := app.Bootstrap(cfg)
			if err != nil {
				return commandError(1, "%v", err)
			}
			if err := application.Start(context.Background()); err != nil {
				return commandError(1, "%v", err)
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := application.Shutdown(shutdownCtx); err != nil {
				return commandError(1, "%v", err)
			}
			return nil
		},
	}
}

func newSubmitCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit messages or events",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing submit subcommand")
		},
	}
	cmd.AddCommand(
		newSubmitMessageCmd(opts),
		newSubmitEventCmd(opts),
		newSubmitFireCmd(opts),
	)
	return cmd
}

func newSubmitMessageCmd(opts *rootOptions) *cobra.Command {
	var text string
	var actor string
	var sourceRef string
	var idempotencyKey string
	var conversationID string
	var threadID string
	var repoRef string
	var replyToEventID string
	var traceID string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "message",
		Short: "Submit a CLI message",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(text) == "" {
				return commandError(1, "--text is required")
			}
			cfg := opts.clientConfig()
			body := domain.CLIMessageSubmitRequest{
				Text:           text,
				ActorRef:       strings.TrimSpace(actor),
				SourceRef:      strings.TrimSpace(sourceRef),
				IdempotencyKey: strings.TrimSpace(idempotencyKey),
				ConversationID: strings.TrimSpace(conversationID),
				ThreadID:       strings.TrimSpace(threadID),
				RepoRef:        strings.TrimSpace(repoRef),
				ReplyToEventID: strings.TrimSpace(replyToEventID),
				TraceID:        chooseNonEmpty(strings.TrimSpace(traceID), cfg.traceID),
			}
			return wrapExitCode(requestWriteJSON(cfg, http.MethodPost, "/v1/ingress/cli/messages", nil, body, false, wait, waitTimeout))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&text, "text", "", "message text")
	flags.StringVar(&actor, "actor", "", "actor_ref")
	flags.StringVar(&sourceRef, "source-ref", "", "source_ref")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency_key")
	flags.StringVar(&conversationID, "conversation-id", "", "conversation_id")
	flags.StringVar(&threadID, "thread-id", "", "thread_id")
	flags.StringVar(&repoRef, "repo", "", "repo_ref")
	flags.StringVar(&replyToEventID, "reply-to-event", "", "reply_to_event_id")
	flags.StringVar(&traceID, "trace-id", "", "trace_id override")
	flags.BoolVar(&wait, "wait", false, "wait until read model catches up to commit_hlc")
	flags.DurationVar(&waitTimeout, "wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
	return cmd
}

func newSubmitEventCmd(opts *rootOptions) *cobra.Command {
	var filePath string
	var actor string
	var sourceRef string
	var idempotencyKey string
	var traceID string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "event",
		Short: "Submit an external event",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "submit event"); err != nil {
				return err
			}
			raw, err := loadJSONInput(filePath)
			if err != nil {
				return commandError(1, "%v", err)
			}
			payload := map[string]any{}
			if err := json.Unmarshal(raw, &payload); err != nil {
				return commandError(1, "%v", err)
			}
			if v := strings.TrimSpace(actor); v != "" {
				payload["actor_ref"] = v
			}
			if v := strings.TrimSpace(sourceRef); v != "" {
				payload["source_ref"] = v
			}
			if v := strings.TrimSpace(idempotencyKey); v != "" {
				payload["idempotency_key"] = v
			}
			if v := chooseNonEmpty(strings.TrimSpace(traceID), cfg.traceID); v != "" {
				payload["trace_id"] = v
			}
			return wrapExitCode(requestWriteJSON(cfg, http.MethodPost, "/v1/admin/submit/events", nil, payload, true, wait, waitTimeout))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&filePath, "file", "", "json file path (default: stdin)")
	flags.StringVar(&actor, "actor", "", "actor_ref override")
	flags.StringVar(&sourceRef, "source-ref", "", "source_ref override")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency_key override")
	flags.StringVar(&traceID, "trace-id", "", "trace_id override")
	flags.BoolVar(&wait, "wait", false, "wait until read model catches up to commit_hlc")
	flags.DurationVar(&waitTimeout, "wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
	return cmd
}

func newSubmitFireCmd(opts *rootOptions) *cobra.Command {
	var scheduledTaskID string
	var scheduledFor string
	var actor string
	var idempotencyKey string
	var reason string
	var traceID string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "fire",
		Short: "Replay a schedule fire",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "submit fire"); err != nil {
				return err
			}
			if strings.TrimSpace(scheduledTaskID) == "" || strings.TrimSpace(scheduledFor) == "" {
				return commandError(1, "--scheduled-task-id and --scheduled-for are required")
			}
			scheduledForWindow, err := time.Parse(time.RFC3339, strings.TrimSpace(scheduledFor))
			if err != nil {
				return commandError(1, "invalid --scheduled-for: %v", err)
			}
			body := domain.ScheduleFireReplayRequest{
				ScheduledTaskID:    strings.TrimSpace(scheduledTaskID),
				ScheduledForWindow: scheduledForWindow,
				ActorRef:           strings.TrimSpace(actor),
				IdempotencyKey:     strings.TrimSpace(idempotencyKey),
				Reason:             strings.TrimSpace(reason),
				TraceID:            chooseNonEmpty(strings.TrimSpace(traceID), cfg.traceID),
			}
			return wrapExitCode(requestWriteJSON(cfg, http.MethodPost, "/v1/admin/submit/fires", nil, body, true, wait, waitTimeout))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&scheduledTaskID, "scheduled-task-id", "", "scheduled_task_id")
	flags.StringVar(&scheduledFor, "scheduled-for", "", "scheduled_for_window RFC3339")
	flags.StringVar(&actor, "actor", "", "actor_ref")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency_key")
	flags.StringVar(&reason, "reason", "", "reason")
	flags.StringVar(&traceID, "trace-id", "", "trace_id override")
	flags.BoolVar(&wait, "wait", false, "wait until read model catches up to commit_hlc")
	flags.DurationVar(&waitTimeout, "wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
	return cmd
}

func newGetCmd(opts *rootOptions) *cobra.Command {
	query := &readQueryOptions{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a resource by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing get subcommand")
		},
	}
	flags := cmd.PersistentFlags()
	flags.StringVar(&query.minHLC, "min-hlc", "", "minimum visible hlc")
	flags.IntVar(&query.waitTimeoutMS, "wait-timeout-ms", 0, "wait timeout milliseconds")
	cmd.AddCommand(
		newGetResourceCmd(opts, query, "event", "/v1/events/"),
		newGetResourceCmd(opts, query, "request", "/v1/requests/"),
		newGetResourceCmd(opts, query, "task", "/v1/tasks/"),
		newGetResourceCmd(opts, query, "schedule", "/v1/schedules/"),
		newGetResourceCmd(opts, query, "approval", "/v1/approvals/"),
		newGetResourceCmd(opts, query, "human-wait", "/v1/human-waits/"),
		newGetResourceCmd(opts, query, "deadletter", "/v1/deadletters/"),
		&cobra.Command{
			Use:   "ops",
			Short: "Get operations overview",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return wrapExitCode(requestJSON(opts.clientConfig(), http.MethodGet, "/v1/ops/overview", query.values(), nil, false))
			},
		},
	)
	return cmd
}

func newGetResourceCmd(opts *rootOptions, query *readQueryOptions, name, pathPrefix string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <id>",
		Short: "Get " + name,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := url.PathEscape(strings.TrimSpace(args[0]))
			return wrapExitCode(requestJSON(opts.clientConfig(), http.MethodGet, pathPrefix+id, query.values(), nil, false))
		},
	}
}

func newListCmd(opts *rootOptions) *cobra.Command {
	query := &listQueryOptions{limit: 50}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing list subcommand")
		},
	}
	flags := cmd.PersistentFlags()
	flags.IntVar(&query.limit, "limit", 50, "page size")
	flags.StringVar(&query.cursor, "cursor", "", "cursor")
	flags.StringVar(&query.minHLC, "min-hlc", "", "minimum visible hlc")
	flags.IntVar(&query.waitTimeoutMS, "wait-timeout-ms", 0, "wait timeout milliseconds")
	flags.StringVar(&query.status, "status", "", "status filter")
	flags.StringVar(&query.conversationID, "conversation-id", "", "conversation_id filter")
	flags.StringVar(&query.actor, "actor", "", "actor filter")
	flags.StringVar(&query.updatedSince, "updated-since", "", "updated_since filter (RFC3339)")
	flags.StringVar(&query.workflowID, "workflow-id", "", "workflow_id filter")
	flags.StringVar(&query.repo, "repo", "", "repo filter")
	flags.StringVar(&query.waitingReason, "waiting-reason", "", "waiting_reason filter")
	flags.StringVar(&query.enabled, "enabled", "", "enabled filter (true|false)")
	flags.StringVar(&query.timezone, "timezone", "", "timezone filter")
	flags.StringVar(&query.entryKind, "entry-kind", "", "entry_kind filter")
	flags.StringVar(&query.taskID, "task-id", "", "task_id filter")
	flags.StringVar(&query.expiresBefore, "expires-before", "", "expires_before filter (RFC3339)")
	flags.StringVar(&query.failureStage, "failure-stage", "", "failure_stage filter")
	flags.StringVar(&query.retryable, "retryable", "", "retryable filter (true|false)")
	flags.StringVar(&query.eventType, "event-type", "", "event_type filter")
	flags.StringVar(&query.sourceKind, "source-kind", "", "source_kind filter")
	flags.StringVar(&query.traceID, "trace-id", "", "trace_id filter")
	cmd.AddCommand(
		newListResourceCmd(opts, query, "requests", "/v1/requests"),
		newListResourceCmd(opts, query, "tasks", "/v1/tasks"),
		newListResourceCmd(opts, query, "schedules", "/v1/schedules"),
		newListResourceCmd(opts, query, "human-actions", "/v1/human-actions"),
		newListResourceCmd(opts, query, "deadletters", "/v1/deadletters"),
		newListResourceCmd(opts, query, "events", "/v1/events"),
		newListResourceCmd(opts, query, "approvals", "/v1/approvals"),
		newListResourceCmd(opts, query, "human-waits", "/v1/human-waits"),
	)
	return cmd
}

func newListResourceCmd(opts *rootOptions, query *listQueryOptions, name, path string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: "List " + name,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return wrapExitCode(requestJSON(opts.clientConfig(), http.MethodGet, path, query.values(), nil, false))
		},
	}
}

func newResolveCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve approvals or waits",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing resolve subcommand")
		},
	}
	cmd.AddCommand(
		newResolveApprovalCmd(opts),
		newResolveWaitCmd(opts),
	)
	return cmd
}

func newResolveApprovalCmd(opts *rootOptions) *cobra.Command {
	var approvalID string
	var taskID string
	var stepExecutionID string
	var decision string
	var actor string
	var idempotencyKey string
	var note string
	var traceID string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "approval",
		Short: "Resolve an approval request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "resolve commands"); err != nil {
				return err
			}
			if strings.TrimSpace(approvalID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(stepExecutionID) == "" || strings.TrimSpace(decision) == "" {
				return commandError(1, "--approval-request-id --task-id --step-execution-id --decision are required")
			}
			body := domain.ResolveApprovalRequest{
				ApprovalRequestID: strings.TrimSpace(approvalID),
				TaskID:            strings.TrimSpace(taskID),
				StepExecutionID:   strings.TrimSpace(stepExecutionID),
				Decision:          strings.TrimSpace(decision),
				ActorRef:          strings.TrimSpace(actor),
				IdempotencyKey:    strings.TrimSpace(idempotencyKey),
				Note:              strings.TrimSpace(note),
				TraceID:           chooseNonEmpty(strings.TrimSpace(traceID), cfg.traceID),
			}
			return wrapExitCode(requestWriteJSON(cfg, http.MethodPost, "/v1/admin/resolve/approval", nil, body, true, wait, waitTimeout))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&approvalID, "approval-request-id", "", "approval_request_id")
	flags.StringVar(&taskID, "task-id", "", "task_id")
	flags.StringVar(&stepExecutionID, "step-execution-id", "", "step_execution_id")
	flags.StringVar(&decision, "decision", "", "approve|reject|confirm|resume-budget")
	flags.StringVar(&actor, "actor", "", "actor_ref")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency_key")
	flags.StringVar(&note, "note", "", "note")
	flags.StringVar(&traceID, "trace-id", "", "trace_id override")
	flags.BoolVar(&wait, "wait", false, "wait until read model catches up to commit_hlc")
	flags.DurationVar(&waitTimeout, "wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
	return cmd
}

func newResolveWaitCmd(opts *rootOptions) *cobra.Command {
	var humanWaitID string
	var taskID string
	var stepExecutionID string
	var waitingReason string
	var decision string
	var targetStepID string
	var actor string
	var idempotencyKey string
	var patchFile string
	var note string
	var traceID string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Resolve a human wait",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "resolve commands"); err != nil {
				return err
			}
			if strings.TrimSpace(humanWaitID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(waitingReason) == "" || strings.TrimSpace(decision) == "" {
				return commandError(1, "--human-wait-id --task-id --waiting-reason --decision are required")
			}
			var patch json.RawMessage
			if v := strings.TrimSpace(patchFile); v != "" {
				raw, err := loadJSONInput(v)
				if err != nil {
					return commandError(1, "%v", err)
				}
				patch = raw
			}
			body := domain.ResolveHumanWaitRequest{
				HumanWaitID:     strings.TrimSpace(humanWaitID),
				TaskID:          strings.TrimSpace(taskID),
				StepExecutionID: strings.TrimSpace(stepExecutionID),
				WaitingReason:   strings.TrimSpace(waitingReason),
				Decision:        strings.TrimSpace(decision),
				TargetStepID:    strings.TrimSpace(targetStepID),
				ActorRef:        strings.TrimSpace(actor),
				IdempotencyKey:  strings.TrimSpace(idempotencyKey),
				InputPatch:      patch,
				Note:            strings.TrimSpace(note),
				TraceID:         chooseNonEmpty(strings.TrimSpace(traceID), cfg.traceID),
			}
			return wrapExitCode(requestWriteJSON(cfg, http.MethodPost, "/v1/admin/resolve/wait", nil, body, true, wait, waitTimeout))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&humanWaitID, "human-wait-id", "", "human_wait_id")
	flags.StringVar(&taskID, "task-id", "", "task_id")
	flags.StringVar(&stepExecutionID, "step-execution-id", "", "step_execution_id")
	flags.StringVar(&waitingReason, "waiting-reason", "", "WaitingInput|WaitingRecovery")
	flags.StringVar(&decision, "decision", "", "provide-input|resume-recovery|rewind")
	flags.StringVar(&targetStepID, "target-step-id", "", "target_step_id")
	flags.StringVar(&actor, "actor", "", "actor_ref")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency_key")
	flags.StringVar(&patchFile, "patch-file", "", "json merge patch file path")
	flags.StringVar(&note, "note", "", "note")
	flags.StringVar(&traceID, "trace-id", "", "trace_id override")
	flags.BoolVar(&wait, "wait", false, "wait until read model catches up to commit_hlc")
	flags.DurationVar(&waitTimeout, "wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
	return cmd
}

func newCancelCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing cancel subcommand")
		},
	}
	cmd.AddCommand(newCancelTaskCmd(opts))
	return cmd
}

func newCancelTaskCmd(opts *rootOptions) *cobra.Command {
	var taskID string
	var stepExecutionID string
	var actor string
	var idempotencyKey string
	var reason string
	var traceID string
	var wait bool
	var waitTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "task",
		Short: "Cancel a task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "cancel commands"); err != nil {
				return err
			}
			if strings.TrimSpace(taskID) == "" {
				return commandError(1, "--task-id is required")
			}
			body := domain.CancelTaskRequest{
				TaskID:          strings.TrimSpace(taskID),
				StepExecutionID: strings.TrimSpace(stepExecutionID),
				ActorRef:        strings.TrimSpace(actor),
				IdempotencyKey:  strings.TrimSpace(idempotencyKey),
				Reason:          strings.TrimSpace(reason),
				TraceID:         chooseNonEmpty(strings.TrimSpace(traceID), cfg.traceID),
			}
			path := "/v1/admin/tasks/" + url.PathEscape(strings.TrimSpace(taskID)) + "/cancel"
			return wrapExitCode(requestWriteJSON(cfg, http.MethodPost, path, nil, body, true, wait, waitTimeout))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&taskID, "task-id", "", "task_id")
	flags.StringVar(&stepExecutionID, "step-execution-id", "", "step_execution_id")
	flags.StringVar(&actor, "actor", "", "actor_ref")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency_key")
	flags.StringVar(&reason, "reason", "", "reason")
	flags.StringVar(&traceID, "trace-id", "", "trace_id override")
	flags.BoolVar(&wait, "wait", false, "wait until read model catches up to commit_hlc")
	flags.DurationVar(&waitTimeout, "wait-timeout", 30*time.Second, "wait timeout (e.g. 30s)")
	return cmd
}

func newAdminCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin operations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing admin subcommand")
		},
	}
	cmd.AddCommand(
		newAdminReplayCmd(opts),
		newAdminReconcileCmd(opts),
		newAdminRebuildCmd(opts),
		newAdminRedriveCmd(opts),
	)
	return cmd
}

func newAdminReplayCmd(opts *rootOptions) *cobra.Command {
	var fromHLC string
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay events from an HLC",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "admin commands"); err != nil {
				return err
			}
			if strings.TrimSpace(fromHLC) == "" {
				return commandError(1, "--from-hlc is required")
			}
			path := "/v1/admin/replay/from/" + url.PathEscape(strings.TrimSpace(fromHLC))
			return wrapExitCode(requestJSON(cfg, http.MethodPost, path, nil, nil, true))
		},
	}
	cmd.Flags().StringVar(&fromHLC, "from-hlc", "", "starting hlc")
	return cmd
}

func newAdminReconcileCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Run reconciliation jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing admin reconcile target")
		},
	}
	cmd.AddCommand(
		newAdminSimplePostCmd(opts, "outbox", "Reconcile outbox", "/v1/admin/reconcile/outbox"),
		newAdminSimplePostCmd(opts, "schedules", "Reconcile schedules", "/v1/admin/reconcile/schedules"),
	)
	return cmd
}

func newAdminRebuildCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Run rebuild jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing admin rebuild target")
		},
	}
	cmd.AddCommand(
		newAdminSimplePostCmd(opts, "indexes", "Rebuild indexes", "/v1/admin/rebuild/indexes"),
	)
	return cmd
}

func newAdminRedriveCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redrive",
		Short: "Redrive deadletters",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandError(1, "missing admin redrive target")
		},
	}
	cmd.AddCommand(newAdminRedriveDeadletterCmd(opts))
	return cmd
}

func newAdminRedriveDeadletterCmd(opts *rootOptions) *cobra.Command {
	var deadletterID string
	cmd := &cobra.Command{
		Use:   "deadletter",
		Short: "Redrive a deadletter",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "admin commands"); err != nil {
				return err
			}
			if strings.TrimSpace(deadletterID) == "" {
				return commandError(1, "--deadletter-id is required")
			}
			path := "/v1/admin/deadletters/" + url.PathEscape(strings.TrimSpace(deadletterID)) + "/redrive"
			return wrapExitCode(requestJSON(cfg, http.MethodPost, path, nil, nil, true))
		},
	}
	cmd.Flags().StringVar(&deadletterID, "deadletter-id", "", "deadletter id")
	return cmd
}

func newAdminSimplePostCmd(opts *rootOptions, use, short, path string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := opts.clientConfig()
			if err := ensureAdminToken(cfg, "admin commands"); err != nil {
				return err
			}
			return wrapExitCode(requestJSON(cfg, http.MethodPost, path, nil, nil, true))
		},
	}
}

func (o *rootOptions) clientConfig() clientConfig {
	return clientConfig{
		serverURL: strings.TrimRight(strings.TrimSpace(o.serverURL), "/"),
		token:     strings.TrimSpace(o.token),
		output:    strings.TrimSpace(o.output),
		timeout:   o.timeout,
		traceID:   strings.TrimSpace(o.traceID),
	}
}

func (o *readQueryOptions) values() url.Values {
	query := url.Values{}
	if v := strings.TrimSpace(o.minHLC); v != "" {
		query.Set("min_hlc", v)
	}
	if o.waitTimeoutMS > 0 {
		query.Set("wait_timeout_ms", strconv.Itoa(o.waitTimeoutMS))
	}
	return query
}

func (o *listQueryOptions) values() url.Values {
	query := url.Values{}
	if o.limit > 0 {
		query.Set("limit", strconv.Itoa(o.limit))
	}
	if v := strings.TrimSpace(o.cursor); v != "" {
		query.Set("cursor", v)
	}
	if v := strings.TrimSpace(o.minHLC); v != "" {
		query.Set("min_hlc", v)
	}
	if o.waitTimeoutMS > 0 {
		query.Set("wait_timeout_ms", strconv.Itoa(o.waitTimeoutMS))
	}
	if v := strings.TrimSpace(o.status); v != "" {
		query.Set("status", v)
	}
	if v := strings.TrimSpace(o.conversationID); v != "" {
		query.Set("conversation_id", v)
	}
	if v := strings.TrimSpace(o.actor); v != "" {
		query.Set("actor", v)
	}
	if v := strings.TrimSpace(o.updatedSince); v != "" {
		query.Set("updated_since", v)
	}
	if v := strings.TrimSpace(o.workflowID); v != "" {
		query.Set("workflow_id", v)
	}
	if v := strings.TrimSpace(o.repo); v != "" {
		query.Set("repo", v)
	}
	if v := strings.TrimSpace(o.waitingReason); v != "" {
		query.Set("waiting_reason", v)
	}
	if v := strings.TrimSpace(o.enabled); v != "" {
		query.Set("enabled", v)
	}
	if v := strings.TrimSpace(o.timezone); v != "" {
		query.Set("timezone", v)
	}
	if v := strings.TrimSpace(o.entryKind); v != "" {
		query.Set("entry_kind", v)
	}
	if v := strings.TrimSpace(o.taskID); v != "" {
		query.Set("task_id", v)
	}
	if v := strings.TrimSpace(o.expiresBefore); v != "" {
		query.Set("expires_before", v)
	}
	if v := strings.TrimSpace(o.failureStage); v != "" {
		query.Set("failure_stage", v)
	}
	if v := strings.TrimSpace(o.retryable); v != "" {
		query.Set("retryable", v)
	}
	if v := strings.TrimSpace(o.eventType); v != "" {
		query.Set("event_type", v)
	}
	if v := strings.TrimSpace(o.sourceKind); v != "" {
		query.Set("source_kind", v)
	}
	if v := strings.TrimSpace(o.traceID); v != "" {
		query.Set("trace_id", v)
	}
	return query
}

func ensureAdminToken(cfg clientConfig, scope string) error {
	if strings.TrimSpace(cfg.token) == "" {
		return commandError(1, "--token is required for %s", scope)
	}
	return nil
}

func commandError(code int, format string, args ...any) error {
	return &exitCodeError{code: code, err: fmt.Errorf(format, args...)}
}

func wrapExitCode(code int) error {
	if code == 0 {
		return nil
	}
	return &exitCodeError{code: code}
}

func resolveExitCode(err error) int {
	var exitErr *exitCodeError
	if errors.As(err, &exitErr) {
		if exitErr.code > 0 {
			return exitErr.code
		}
	}
	return 1
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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	client := resty.New().
		SetBaseURL(strings.TrimRight(strings.TrimSpace(cfg.serverURL), "/")).
		SetTimeout(cfg.timeout).
		SetHeader("Accept", "application/json")

	req := client.R().SetContext(ctx)
	if len(query) > 0 {
		req.SetQueryParamsFromValues(query)
	}
	if body != nil {
		req.SetHeader("Content-Type", "application/json")
		req.SetBody(body)
	}
	if admin {
		req.SetHeader("X-Admin-Token", cfg.token)
	}
	if strings.TrimSpace(cfg.traceID) != "" {
		req.SetHeader("X-Trace-ID", strings.TrimSpace(cfg.traceID))
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode(), resp.Body(), nil
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
