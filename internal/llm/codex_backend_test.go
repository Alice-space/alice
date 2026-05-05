package llm

import "testing"

func TestCodexBackend_ProfileOverrideWithoutProviderProfileDoesNotLeakOuterName(t *testing.T) {
	backend := newCodexBackend(CodexConfig{
		Command: "codex-default",
		ProfileOverrides: map[string]ProfileRunnerConfig{
			"work": {
				Command: "codex-work",
			},
		},
	})

	runner, providerProfile := backend.resolveRunnerAndProviderProfile("work")
	if runner.Command != "codex-work" {
		t.Fatalf("expected profile override runner, got command %q", runner.Command)
	}
	if providerProfile != "" {
		t.Fatalf("outer profile name should not be forwarded to codex CLI, got %q", providerProfile)
	}
}

func TestCodexBackend_ProfileOverrideUsesExplicitProviderProfile(t *testing.T) {
	backend := newCodexBackend(CodexConfig{
		Command: "codex-default",
		ProfileOverrides: map[string]ProfileRunnerConfig{
			"work": {
				Command:         "codex-work",
				ProviderProfile: "work-cli",
			},
		},
	})

	runner, providerProfile := backend.resolveRunnerAndProviderProfile("work")
	if runner.Command != "codex-work" {
		t.Fatalf("expected profile override runner, got command %q", runner.Command)
	}
	if providerProfile != "work-cli" {
		t.Fatalf("expected explicit provider profile, got %q", providerProfile)
	}
}

func TestCodexBackend_UnmatchedProfilePreservesDirectProviderProfile(t *testing.T) {
	backend := newCodexBackend(CodexConfig{
		Command: "codex-default",
	})

	runner, providerProfile := backend.resolveRunnerAndProviderProfile("direct-cli")
	if runner.Command != "codex-default" {
		t.Fatalf("expected default runner, got command %q", runner.Command)
	}
	if providerProfile != "direct-cli" {
		t.Fatalf("expected unmatched profile to remain provider profile, got %q", providerProfile)
	}
}
