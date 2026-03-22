package llm

import (
	"context"
	"testing"
)

type captureBackend struct {
	lastReq RunRequest
	reply   string
}

func (b *captureBackend) Run(_ context.Context, req RunRequest) (RunResult, error) {
	b.lastReq = req
	return RunResult{Reply: b.reply}, nil
}

func TestMultiBackend_RoutesByRequestedProvider(t *testing.T) {
	codex := &captureBackend{reply: "codex"}
	claude := &captureBackend{reply: "claude"}
	backend, err := NewMultiBackend("codex", map[string]Backend{
		"codex":  codex,
		"claude": claude,
	})
	if err != nil {
		t.Fatalf("build multi backend failed: %v", err)
	}

	result, err := backend.Run(context.Background(), RunRequest{
		Provider: "claude",
		UserText: "hello",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.Reply != "claude" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if claude.lastReq.Provider != "claude" {
		t.Fatalf("unexpected provider routed to claude backend: %q", claude.lastReq.Provider)
	}
	if codex.lastReq.Provider != "" {
		t.Fatalf("codex backend should not receive request, got %#v", codex.lastReq)
	}
}

func TestMultiBackend_UsesDefaultProviderWhenRequestOmitsProvider(t *testing.T) {
	codex := &captureBackend{reply: "codex"}
	backend, err := NewMultiBackend("codex", map[string]Backend{
		"codex": codex,
	})
	if err != nil {
		t.Fatalf("build multi backend failed: %v", err)
	}

	_, err = backend.Run(context.Background(), RunRequest{UserText: "hello"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if codex.lastReq.Provider != "codex" {
		t.Fatalf("unexpected default provider: %q", codex.lastReq.Provider)
	}
}
