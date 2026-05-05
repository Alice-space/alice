package llm_test

import (
	"strings"
	"testing"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
)

func TestNewProvider_DefaultsToCodex(t *testing.T) {
	provider, err := llm.NewProvider(llm.FactoryConfig{
		Codex: llm.CodexConfig{Command: "codex", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_Claude(t *testing.T) {
	provider, err := llm.NewProvider(llm.FactoryConfig{
		Provider: llm.ProviderClaude,
		Claude:   llm.ClaudeConfig{Command: "claude", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_Gemini(t *testing.T) {
	provider, err := llm.NewProvider(llm.FactoryConfig{
		Provider: llm.ProviderGemini,
		Gemini:   llm.GeminiConfig{Command: "gemini", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_Kimi(t *testing.T) {
	provider, err := llm.NewProvider(llm.FactoryConfig{
		Provider: llm.ProviderKimi,
		Kimi:     llm.KimiConfig{Command: "kimi", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_RejectsUnknownProvider(t *testing.T) {
	_, err := llm.NewProvider(llm.FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBackend_DefaultsToCodex(t *testing.T) {
	backend, err := llm.NewBackend(llm.FactoryConfig{
		Codex: llm.CodexConfig{Command: "codex", Timeout: 30 * time.Second},
	})
	if err != nil || backend == nil {
		t.Fatalf("unexpected result err=%v backend=%v", err, backend)
	}
}

func TestNewBackend_RejectsUnknownProvider(t *testing.T) {
	_, err := llm.NewBackend(llm.FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewInteractiveProviderSession_SteerModes(t *testing.T) {
	tests := []struct {
		name     string
		cfg      llm.FactoryConfig
		wantMode llm.SteerMode
	}{
		{
			name:     "codex",
			cfg:      llm.FactoryConfig{Provider: llm.ProviderCodex},
			wantMode: llm.SteerModeNative,
		},
		{
			name:     "kimi",
			cfg:      llm.FactoryConfig{Provider: llm.ProviderKimi},
			wantMode: llm.SteerModeNative,
		},
		{
			name:     "opencode",
			cfg:      llm.FactoryConfig{Provider: llm.ProviderOpenCode},
			wantMode: llm.SteerModeNativeEnqueue,
		},
		{
			name:     "claude",
			cfg:      llm.FactoryConfig{Provider: llm.ProviderClaude},
			wantMode: llm.SteerModeNativeEnqueue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := llm.NewInteractiveProviderSession(tt.cfg)
			if err != nil {
				t.Fatalf("NewInteractiveProviderSession failed: %v", err)
			}
			defer session.Close()
			if got := session.SteerMode(); got != tt.wantMode {
				t.Fatalf("SteerMode() = %q, want %q", got, tt.wantMode)
			}
		})
	}
}

func TestNewProvider_CaseInsensitiveProvider(t *testing.T) {
	for _, name := range []string{"Claude", "CLAUDE", "claude"} {
		provider, err := llm.NewProvider(llm.FactoryConfig{
			Provider: name,
			Claude:   llm.ClaudeConfig{Command: "claude"},
		})
		if err != nil {
			t.Fatalf("provider=%q: unexpected error: %v", name, err)
		}
		if provider == nil {
			t.Fatalf("provider=%q: expected non-nil provider", name)
		}
	}
}
