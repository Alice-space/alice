package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"alice/internal/domain"
)

type recordedRequest struct {
	Method     string
	Path       string
	RawQuery   string
	AdminToken string
}

func TestWriteCommandWaitPollsReadEndpointWithCommitHLC(t *testing.T) {
	var mu sync.Mutex
	var callPaths []string
	pollCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callPaths = append(callPaths, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/ingress/cli/messages":
			_ = json.NewEncoder(w).Encode(domain.WriteAcceptedResponse{
				Accepted:        true,
				EventID:         "evt_wait_1",
				RequestID:       "req_wait_1",
				RouteTargetKind: "request",
				RouteTargetID:   "req_wait_1",
				CommitHLC:       "2026-03-10T10:00:00.000000000Z#0005",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/requests/req_wait_1":
			minHLC := r.URL.Query().Get("min_hlc")
			if minHLC != "2026-03-10T10:00:00.000000000Z#0005" {
				http.Error(w, "missing min_hlc", http.StatusBadRequest)
				return
			}
			pollCount++
			visible := "2026-03-10T09:59:59.000000000Z#0001"
			if pollCount >= 2 {
				visible = "2026-03-10T10:00:00.000000000Z#0006"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item":        map[string]any{"request_id": "req_wait_1"},
				"visible_hlc": visible,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	code := run([]string{"--server", srv.URL, "submit", "message", "--text", "hello", "--wait", "--wait-timeout", "2s"})
	if code != 0 {
		t.Fatalf("expected wait flow exit code 0, got %d", code)
	}
	if pollCount < 2 {
		t.Fatalf("expected polling read endpoint at least twice, got %d", pollCount)
	}
	mu.Lock()
	defer mu.Unlock()
	hasPost := false
	hasGet := false
	for _, call := range callPaths {
		if strings.HasPrefix(call, "POST /v1/ingress/cli/messages") {
			hasPost = true
		}
		if strings.HasPrefix(call, "GET /v1/requests/req_wait_1?") && strings.Contains(call, "min_hlc=2026-03-10T10%3A00%3A00.000000000Z%230005") {
			hasGet = true
		}
	}
	if !hasPost || !hasGet {
		t.Fatalf("expected both write and wait-poll calls, got %v", callPaths)
	}

}

func TestClientModeBaselineCommandsHitHTTPSurface(t *testing.T) {
	var mu sync.Mutex
	var calls []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, recordedRequest{
			Method:     r.Method,
			Path:       r.URL.Path,
			RawQuery:   r.URL.RawQuery,
			AdminToken: r.Header.Get("X-Admin-Token"),
		})
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/v1/admin/") && r.Header.Get("X-Admin-Token") != "tok" {
			http.Error(w, "missing admin token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"input_kind":"web_form_message","body_schema_id":"web-form-message.v1","body":{"text":"hello"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		args          []string
		wantCode      int
		wantMethod    string
		wantPath      string
		wantQueryPart string
		wantAdmin     bool
	}{
		{
			name:       "submit message",
			args:       []string{"--server", srv.URL, "submit", "message", "--text", "hi"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/ingress/cli/messages",
		},
		{
			name:       "submit event",
			args:       []string{"--server", srv.URL, "--token", "tok", "submit", "event", "--file", eventFile},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/submit/events",
			wantAdmin:  true,
		},
		{
			name:       "submit fire",
			args:       []string{"--server", srv.URL, "--token", "tok", "submit", "fire", "--scheduled-task-id", "sch_1", "--scheduled-for", "2026-03-10T09:00:00Z"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/submit/fires",
			wantAdmin:  true,
		},
		{
			name:       "get request",
			args:       []string{"--server", srv.URL, "get", "request", "req_1"},
			wantCode:   0,
			wantMethod: http.MethodGet,
			wantPath:   "/v1/requests/req_1",
		},
		{
			name:          "list tasks",
			args:          []string{"--server", srv.URL, "list", "tasks"},
			wantCode:      0,
			wantMethod:    http.MethodGet,
			wantPath:      "/v1/tasks",
			wantQueryPart: "limit=50",
		},
		{
			name:          "list requests with filters",
			args:          []string{"--server", srv.URL, "list", "requests", "--status", "open", "--conversation-id", "conv_1", "--actor", "alice", "--updated-since", "2026-03-10T00:00:00Z"},
			wantCode:      0,
			wantMethod:    http.MethodGet,
			wantPath:      "/v1/requests",
			wantQueryPart: "conversation_id=conv_1",
		},
		{
			name:       "resolve approval",
			args:       []string{"--server", srv.URL, "--token", "tok", "resolve", "approval", "--approval-request-id", "apr_1", "--task-id", "task_1", "--step-execution-id", "exec_1", "--decision", "approve"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/resolve/approval",
			wantAdmin:  true,
		},
		{
			name:       "resolve wait",
			args:       []string{"--server", srv.URL, "--token", "tok", "resolve", "wait", "--human-wait-id", "wait_1", "--task-id", "task_1", "--waiting-reason", "WaitingRecovery", "--decision", "resume-recovery"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/resolve/wait",
			wantAdmin:  true,
		},
		{
			name:       "cancel task",
			args:       []string{"--server", srv.URL, "--token", "tok", "cancel", "task", "--task-id", "task_1"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/tasks/task_1/cancel",
			wantAdmin:  true,
		},
		{
			name:       "admin replay",
			args:       []string{"--server", srv.URL, "--token", "tok", "admin", "replay", "--from-hlc", "2026-03-10T10:00:00.000000000Z#0001"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/replay/from/2026-03-10T10:00:00.000000000Z#0001",
			wantAdmin:  true,
		},
		{
			name:       "admin reconcile outbox",
			args:       []string{"--server", srv.URL, "--token", "tok", "admin", "reconcile", "outbox"},
			wantCode:   0,
			wantMethod: http.MethodPost,
			wantPath:   "/v1/admin/reconcile/outbox",
			wantAdmin:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			calls = calls[:0]
			mu.Unlock()
			gotCode := run(tt.args)
			if gotCode != tt.wantCode {
				t.Fatalf("exit code mismatch: got=%d want=%d", gotCode, tt.wantCode)
			}
			mu.Lock()
			if len(calls) != 1 {
				mu.Unlock()
				t.Fatalf("expected exactly one HTTP call, got %d", len(calls))
			}
			call := calls[0]
			mu.Unlock()
			if call.Method != tt.wantMethod {
				t.Fatalf("method mismatch: got=%s want=%s", call.Method, tt.wantMethod)
			}
			if call.Path != tt.wantPath {
				t.Fatalf("path mismatch: got=%s want=%s", call.Path, tt.wantPath)
			}
			if tt.wantQueryPart != "" && !strings.Contains(call.RawQuery, tt.wantQueryPart) {
				t.Fatalf("query mismatch: got=%s want contains %s", call.RawQuery, tt.wantQueryPart)
			}
			if tt.wantAdmin && call.AdminToken != "tok" {
				t.Fatalf("admin token header mismatch: got=%s", call.AdminToken)
			}
		})
	}
}
