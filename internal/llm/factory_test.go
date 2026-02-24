package llm

import (
	"strings"
	"testing"
	"time"
)

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

func TestNewMCPRegistrar_RejectsUnknownProvider(t *testing.T) {
	_, err := NewMCPRegistrar(FactoryConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}
