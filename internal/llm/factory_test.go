package llm_test

import (
	"strings"
	"testing"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
)

func TestNewProvider_AllProvidersRequireStreaming(t *testing.T) {
	for _, provider := range []string{llm.ProviderCodex, llm.ProviderClaude, llm.ProviderKimi, llm.ProviderOpenCode} {
		_, err := llm.NewProvider(llm.FactoryConfig{Provider: provider})
		if err == nil {
			t.Errorf("expected error for %q (streaming required)", provider)
		}
		if !strings.Contains(err.Error(), "streaming") {
			t.Errorf("unexpected error for %q: %v", provider, err)
		}
	}
}

func TestNewBackend_RequiresStreaming(t *testing.T) {
	_, err := llm.NewBackend(llm.FactoryConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "streaming") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewInteractiveProviderSession_SupportedProviders(t *testing.T) {
	providers := map[string]llm.FactoryConfig{
		llm.ProviderCodex:    {Provider: llm.ProviderCodex, Codex: llm.CodexConfig{Command: "codex", Timeout: 30 * time.Minute}},
		llm.ProviderClaude:   {Provider: llm.ProviderClaude, Claude: llm.ClaudeConfig{Command: "claude", Timeout: 30 * time.Minute}},
		llm.ProviderKimi:     {Provider: llm.ProviderKimi, Kimi: llm.KimiConfig{Command: "kimi", Timeout: 30 * time.Minute}},
		llm.ProviderOpenCode: {Provider: llm.ProviderOpenCode, OpenCode: llm.OpenCodeConfig{Command: "opencode", Timeout: 30 * time.Minute}},
	}
	for name, cfg := range providers {
		t.Run(name, func(t *testing.T) {
			session, err := llm.NewInteractiveProviderSession(cfg)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", name, err)
			}
			if session == nil {
				t.Fatalf("%s: expected non-nil session", name)
			}
			session.Close()
		})
	}
}

func TestNewInteractiveProviderSession_RejectsUnknownProvider(t *testing.T) {
	_, err := llm.NewInteractiveProviderSession(llm.FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewInteractiveProviderSession_DefaultsToCodex(t *testing.T) {
	cfg := llm.FactoryConfig{
		Codex: llm.CodexConfig{Command: "codex", Timeout: 30 * time.Minute},
	}
	session, err := llm.NewInteractiveProviderSession(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	session.Close()
}
