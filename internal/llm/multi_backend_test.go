package llm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	llm "github.com/Alice-space/alice/internal/llm"
)

type stubBackend struct {
	reply        string
	nextThreadID string
	err          error
	lastReq      llm.RunRequest
}

func (s *stubBackend) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	s.lastReq = req
	return llm.RunResult{Reply: s.reply, NextThreadID: s.nextThreadID}, s.err
}

func TestMultiBackend_RoutesToCorrectProvider(t *testing.T) {
	codexStub := &stubBackend{reply: "codex-reply"}
	claudeStub := &stubBackend{reply: "claude-reply"}

	m, err := llm.NewMultiBackend("codex", map[string]llm.Backend{
		"codex":  codexStub,
		"claude": claudeStub,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := m.Run(context.Background(), llm.RunRequest{Provider: "claude", UserText: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "claude-reply" {
		t.Fatalf("expected claude-reply, got %q", result.Reply)
	}
}

func TestMultiBackend_FallsBackToDefault(t *testing.T) {
	codexStub := &stubBackend{reply: "codex-reply"}

	m, err := llm.NewMultiBackend("codex", map[string]llm.Backend{
		"codex": codexStub,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := m.Run(context.Background(), llm.RunRequest{UserText: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reply != "codex-reply" {
		t.Fatalf("expected codex-reply, got %q", result.Reply)
	}
}

func TestMultiBackend_ErrorOnUnknownProvider(t *testing.T) {
	m, err := llm.NewMultiBackend("codex", map[string]llm.Backend{
		"codex": &stubBackend{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = m.Run(context.Background(), llm.RunRequest{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMultiBackend_RequiresAtLeastOneBackend(t *testing.T) {
	_, err := llm.NewMultiBackend("codex", map[string]llm.Backend{})
	if err == nil {
		t.Fatal("expected error for empty backends")
	}
}

func TestMultiBackend_PropagatesBackendError(t *testing.T) {
	wantErr := errors.New("backend failure")
	m, err := llm.NewMultiBackend("codex", map[string]llm.Backend{
		"codex": &stubBackend{err: wantErr},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = m.Run(context.Background(), llm.RunRequest{UserText: "hello"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped backend error, got: %v", err)
	}
}

func TestMultiBackend_ExplicitDefaultUsedWhenProviderEmpty(t *testing.T) {
	claudeStub := &stubBackend{reply: "claude"}
	m, err := llm.NewMultiBackend("claude", map[string]llm.Backend{
		"claude": claudeStub,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No Provider in request → should use the explicit default "claude".
	result, err := m.Run(context.Background(), llm.RunRequest{UserText: "hi"})
	if err != nil || result.Reply != "claude" {
		t.Fatalf("expected claude reply, got %q err=%v", result.Reply, err)
	}
}

func TestMultiBackend_NilBackendsSkipped(t *testing.T) {
	codexStub := &stubBackend{reply: "codex"}
	m, err := llm.NewMultiBackend("codex", map[string]llm.Backend{
		"codex":  codexStub,
		"claude": nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := m.Run(context.Background(), llm.RunRequest{UserText: "hi"})
	if err != nil || result.Reply != "codex" {
		t.Fatalf("unexpected result: %q %v", result.Reply, err)
	}
}

func TestMultiBackend_RejectsUnregisteredDefault(t *testing.T) {
	_, err := llm.NewMultiBackend("gemini", map[string]llm.Backend{
		"codex": &stubBackend{},
	})
	if err == nil {
		t.Fatal("expected error when defaultProvider is not a registered backend")
	}
}

func TestMultiBackend_MultipleBackendsNoCodexRequiresExplicitDefault(t *testing.T) {
	_, err := llm.NewMultiBackend("", map[string]llm.Backend{
		"claude": &stubBackend{},
		"gemini": &stubBackend{},
	})
	if err == nil {
		t.Fatal("expected error when multiple non-codex backends and no explicit default")
	}
}

func TestMultiBackend_MultipleBackendsWithCodexDefaultsToCodex(t *testing.T) {
	codexStub := &stubBackend{reply: "codex"}
	m, err := llm.NewMultiBackend("", map[string]llm.Backend{
		"codex":  codexStub,
		"claude": &stubBackend{reply: "claude"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := m.Run(context.Background(), llm.RunRequest{UserText: "hi"})
	if err != nil || result.Reply != "codex" {
		t.Fatalf("expected codex default, got %q err=%v", result.Reply, err)
	}
}
