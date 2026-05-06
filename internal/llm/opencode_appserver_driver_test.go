package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeAppServerDriverNativeEnqueue(t *testing.T) {
	requests := make(chan string, 4)
	eventPayloads := make(chan string, 8)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/event":
			serveOpenCodeEventStream(w, r, eventPayloads)
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			requests <- "create"
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "session-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/session/session-1/prompt_async":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.Contains(mustJSON(t, body), "second") {
				requests <- "steer"
			} else {
				requests <- "prompt_async"
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	driver := newOpenCodeAppServerDriver(OpenCodeConfig{ServerURL: server.URL})
	session := NewInteractiveSession(driver)
	defer session.Close()

	first, err := session.Submit(context.Background(), RunRequest{UserText: "first"})
	if err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	if first.Mode != SubmitStarted {
		t.Fatalf("first mode = %q, want %q", first.Mode, SubmitStarted)
	}
	waitForRequest(t, requests, "prompt_async")

	second, err := session.Submit(context.Background(), RunRequest{UserText: "second"})
	if err != nil {
		t.Fatalf("second submit failed: %v", err)
	}
	if second.Mode != SubmitSteered {
		t.Fatalf("second mode = %q, want %q", second.Mode, SubmitSteered)
	}
	waitForRequest(t, requests, "steer")

	sendOpenCodeEvent(t, eventPayloads, map[string]any{
		"type": "message.part.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"part": map[string]any{
				"id":        "part-1",
				"sessionID": "session-1",
				"messageID": "msg-assistant",
				"type":      "text",
				"text":      "done",
				"time":      map[string]any{"start": 1, "end": 2},
			},
		},
	})
	textEvent := waitForTurnEvent(t, session.Events(), TurnEventAssistantText)
	if textEvent.Text != "done" {
		t.Fatalf("assistant text = %q, want done", textEvent.Text)
	}
	sendOpenCodeEvent(t, eventPayloads, map[string]any{
		"type": "message.updated",
		"properties": map[string]any{
			"sessionID": "session-1",
			"info": map[string]any{
				"id":        "msg-assistant",
				"sessionID": "session-1",
				"role":      "assistant",
				"time":      map[string]any{"completed": 3},
				"finish":    "stop",
				"tokens": map[string]any{
					"input":  3,
					"output": 5,
					"cache":  map[string]any{"read": 1, "write": 0},
				},
			},
		},
	})
	completed := waitForTurnEvent(t, session.Events(), TurnEventCompleted)
	if completed.Usage.InputTokens != 3 || completed.Usage.OutputTokens != 5 || completed.Usage.CachedInputTokens != 1 {
		t.Fatalf("completion usage = %#v, want 3/5/1", completed.Usage)
	}
}

func TestOpenCodeAppServerDriverInterruptUsesAbort(t *testing.T) {
	promptAsyncCalled := make(chan struct{})
	abortCalled := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/event":
			serveOpenCodeEventStream(w, r, nil)
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "session-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/session/session-1/prompt_async":
			close(promptAsyncCalled)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/session/session-1/abort":
			close(abortCalled)
			_ = json.NewEncoder(w).Encode(true)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	driver := newOpenCodeAppServerDriver(OpenCodeConfig{ServerURL: server.URL})
	session := NewInteractiveSession(driver)
	defer session.Close()

	if _, err := session.Submit(context.Background(), RunRequest{ThreadID: "session-1", UserText: "first"}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	waitClosed(t, promptAsyncCalled, "prompt_async should be called")
	if err := session.Interrupt(context.Background()); err != nil {
		t.Fatalf("interrupt failed: %v", err)
	}
	waitClosed(t, abortCalled, "abort should be called")
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sendOpenCodeEvent(t *testing.T, ch chan<- string, event any) {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ch <- string(raw):
	case <-time.After(5 * time.Second):
		t.Fatal("timed out enqueueing opencode event")
	}
}

func serveOpenCodeEventStream(w http.ResponseWriter, r *http.Request, events <-chan string) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	for {
		select {
		case payload, ok := <-events:
			if !ok {
				return
			}
			_, _ = w.Write([]byte("data: " + payload + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func waitForRequest(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-ch:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for request %q", want)
		}
	}
}

func waitForTurnEvent(t *testing.T, events <-chan TurnEvent, want TurnEventKind) TurnEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("events closed while waiting for %q", want)
			}
			if event.Kind == want {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %q", want)
		}
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

func TestPromptBody_IncludesVariant(t *testing.T) {
	d := newOpenCodeAppServerDriver(OpenCodeConfig{})
	body := d.promptBody(RunRequest{
		UserText: "hello",
		Model:    "deepseek/deepseek-v4-pro",
		Variant:  "max",
	})
	v, ok := body["variant"].(string)
	if !ok {
		t.Fatal("expected variant in prompt body")
	}
	if v != "max" {
		t.Errorf("variant = %q, want %q", v, "max")
	}
}

func TestPromptBody_OmitsVariantWhenEmpty(t *testing.T) {
	d := newOpenCodeAppServerDriver(OpenCodeConfig{})
	body := d.promptBody(RunRequest{
		UserText: "hello",
		Model:    "deepseek/deepseek-v4-pro",
		Variant:  "",
	})
	if _, ok := body["variant"]; ok {
		t.Error("expected no variant in prompt body when variant is empty")
	}
}
