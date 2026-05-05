package llm_test

import (
	"strings"
	"testing"
	"time"

	agentbridge "github.com/Alice-space/alice/internal/llm"
)

func TestNewProvider_DefaultsToCodex(t *testing.T) {
	provider, err := agentbridge.NewProvider(agentbridge.FactoryConfig{
		Codex: agentbridge.CodexConfig{Command: "codex", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_Claude(t *testing.T) {
	provider, err := agentbridge.NewProvider(agentbridge.FactoryConfig{
		Provider: agentbridge.ProviderClaude,
		Claude:   agentbridge.ClaudeConfig{Command: "claude", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_Gemini(t *testing.T) {
	provider, err := agentbridge.NewProvider(agentbridge.FactoryConfig{
		Provider: agentbridge.ProviderGemini,
		Gemini:   agentbridge.GeminiConfig{Command: "gemini", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_Kimi(t *testing.T) {
	provider, err := agentbridge.NewProvider(agentbridge.FactoryConfig{
		Provider: agentbridge.ProviderKimi,
		Kimi:     agentbridge.KimiConfig{Command: "kimi", Timeout: 30 * time.Second},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil || provider.Backend() == nil {
		t.Fatal("expected non-nil provider and backend")
	}
}

func TestNewProvider_RejectsUnknownProvider(t *testing.T) {
	_, err := agentbridge.NewProvider(agentbridge.FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBackend_DefaultsToCodex(t *testing.T) {
	backend, err := agentbridge.NewBackend(agentbridge.FactoryConfig{
		Codex: agentbridge.CodexConfig{Command: "codex", Timeout: 30 * time.Second},
	})
	if err != nil || backend == nil {
		t.Fatalf("unexpected result err=%v backend=%v", err, backend)
	}
}

func TestNewBackend_RejectsUnknownProvider(t *testing.T) {
	_, err := agentbridge.NewBackend(agentbridge.FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewInteractiveProviderSession_SteerModes(t *testing.T) {
	tests := []struct {
		name     string
		cfg      agentbridge.FactoryConfig
		wantMode agentbridge.SteerMode
	}{
		{
			name:     "codex",
			cfg:      agentbridge.FactoryConfig{Provider: agentbridge.ProviderCodex},
			wantMode: agentbridge.SteerModeNative,
		},
		{
			name:     "kimi",
			cfg:      agentbridge.FactoryConfig{Provider: agentbridge.ProviderKimi},
			wantMode: agentbridge.SteerModeNative,
		},
		{
			name:     "opencode",
			cfg:      agentbridge.FactoryConfig{Provider: agentbridge.ProviderOpenCode},
			wantMode: agentbridge.SteerModeNativeEnqueue,
		},
		{
			name:     "claude",
			cfg:      agentbridge.FactoryConfig{Provider: agentbridge.ProviderClaude},
			wantMode: agentbridge.SteerModeNativeEnqueue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := agentbridge.NewInteractiveProviderSession(tt.cfg)
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
		provider, err := agentbridge.NewProvider(agentbridge.FactoryConfig{
			Provider: name,
			Claude:   agentbridge.ClaudeConfig{Command: "claude"},
		})
		if err != nil {
			t.Fatalf("provider=%q: unexpected error: %v", name, err)
		}
		if provider == nil {
			t.Fatalf("provider=%q: expected non-nil provider", name)
		}
	}
}
