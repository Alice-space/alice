package llm

import (
	"strings"
	"testing"
	"time"
)

func TestNewProvider_DefaultsToCodex(t *testing.T) {
	provider, err := NewProvider(FactoryConfig{
		Codex: CodexConfig{
			Command: "codex",
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new provider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Backend() == nil {
		t.Fatal("expected non-nil backend")
	}
	if provider.MCPRegistrar() == nil {
		t.Fatal("expected non-nil mcp registrar")
	}
}

func TestNewProvider_Claude(t *testing.T) {
	provider, err := NewProvider(FactoryConfig{
		Provider: ProviderClaude,
		Claude: ClaudeConfig{
			Command: "claude",
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new provider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Backend() == nil {
		t.Fatal("expected non-nil backend")
	}
	if provider.MCPRegistrar() == nil {
		t.Fatal("expected non-nil mcp registrar")
	}
}

func TestNewProvider_Kimi(t *testing.T) {
	provider, err := NewProvider(FactoryConfig{
		Provider: ProviderKimi,
		Kimi: KimiConfig{
			Command: "kimi",
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new provider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Backend() == nil {
		t.Fatal("expected non-nil backend")
	}
	if provider.MCPRegistrar() != nil {
		t.Fatal("kimi should not expose an mcp registrar")
	}
}

func TestNewProvider_RejectsUnknownProvider(t *testing.T) {
	_, err := NewProvider(FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBackend_DefaultsToCodex(t *testing.T) {
	backend, err := NewBackend(FactoryConfig{
		Codex: CodexConfig{
			Command: "codex",
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new backend failed: %v", err)
	}
	if backend == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestNewBackend_Claude(t *testing.T) {
	backend, err := NewBackend(FactoryConfig{
		Provider: ProviderClaude,
		Claude: ClaudeConfig{
			Command: "claude",
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new backend failed: %v", err)
	}
	if backend == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestNewBackend_Kimi(t *testing.T) {
	backend, err := NewBackend(FactoryConfig{
		Provider: ProviderKimi,
		Kimi: KimiConfig{
			Command: "kimi",
			Timeout: 30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("new backend failed: %v", err)
	}
	if backend == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestNewBackend_RejectsUnknownProvider(t *testing.T) {
	_, err := NewBackend(FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewMCPRegistrar_DefaultsToCodex(t *testing.T) {
	registrar, err := NewMCPRegistrar(FactoryConfig{
		Codex: CodexConfig{
			Command: "codex",
		},
	})
	if err != nil {
		t.Fatalf("new mcp registrar failed: %v", err)
	}
	if registrar == nil {
		t.Fatal("expected non-nil mcp registrar")
	}
}

func TestNewMCPRegistrar_Claude(t *testing.T) {
	registrar, err := NewMCPRegistrar(FactoryConfig{
		Provider: ProviderClaude,
		Claude: ClaudeConfig{
			Command: "claude",
		},
	})
	if err != nil {
		t.Fatalf("new mcp registrar failed: %v", err)
	}
	if registrar == nil {
		t.Fatal("expected non-nil mcp registrar")
	}
}

func TestNewMCPRegistrar_RejectsUnknownProvider(t *testing.T) {
	_, err := NewMCPRegistrar(FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
