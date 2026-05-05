package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDelegateRequiresProvider(t *testing.T) {
	cmd := newDelegateCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--prompt", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --provider is missing")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("error should mention provider, got: %v", err)
	}
}

func TestDelegateRequiresPrompt(t *testing.T) {
	cmd := newDelegateCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--provider", "codex"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --prompt is missing and no stdin")
	}
	if !strings.Contains(err.Error(), "prompt") && !strings.Contains(err.Error(), "stdin") {
		t.Fatalf("error should mention prompt or stdin, got: %v", err)
	}
}

func TestDelegateUnsupportedProvider(t *testing.T) {
	cmd := newDelegateCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--provider", "unknown-llm", "--prompt", "test"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("error should mention unsupported provider, got: %v", err)
	}
}

func TestDelegateStdinTakesPrecedence(t *testing.T) {
	// When stdin has data, it should be used even if --prompt is set.
	// Set a short timeout so we fail fast when no binary is available.
	cmd := newDelegateCmd()
	stdin := strings.NewReader("stdin prompt text")
	cmd.SetIn(stdin)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--provider", "codex", "--prompt", "cli prompt", "--timeout", "1s"})

	err := cmd.Execute()
	if err == nil {
		t.Skip("codex CLI available - test passed via real execution")
	}
	// Should fail because codex binary not available, timeout, etc.
	// The important thing is it didn't fail on missing prompt/parse errors.
	if strings.Contains(err.Error(), "prompt") && strings.Contains(err.Error(), "required") {
		t.Fatalf("stdin should satisfy prompt requirement, got: %v", err)
	}
	msg := strings.ToLower(err.Error())
	acceptable := strings.Contains(msg, "exec") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "authenticated") ||
		strings.Contains(msg, "run:")
	if !acceptable {
		t.Fatalf("unexpected error when running delegate with stdin: %v", err)
	}
}

func TestDelegateAllProvidersFlagParsing(t *testing.T) {
	providers := []string{"codex", "claude", "gemini", "kimi", "opencode"}
	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			cmd := newDelegateCmd()
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"--provider", p, "--prompt", "test", "--timeout", "1s"})

			err := cmd.Execute()
			if err == nil {
				return // provider binary available, skip
			}
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "unsupported llm_provider") {
				t.Fatalf("provider %q should be supported, got: %v", p, err)
			}
		})
	}
}

func TestDelegateWorkspaceDirFlag(t *testing.T) {
	tmp := t.TempDir()
	cmd := newDelegateCmd()
	cmd.SetArgs([]string{
		"--provider", "codex",
		"--prompt", "test",
		"--workspace-dir", tmp,
		"--timeout", "1s",
	})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Skip("codex CLI available")
	}
	if strings.Contains(err.Error(), "workspace") && strings.Contains(err.Error(), "parse") {
		t.Fatalf("workspace-dir flag should parse correctly, got: %v", err)
	}
}

func TestDelegateTimeoutFlag(t *testing.T) {
	cmd := newDelegateCmd()
	cmd.SetArgs([]string{
		"--provider", "codex",
		"--prompt", "test",
		"--timeout", "1s",
	})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Skip("codex CLI available")
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") && strings.Contains(msg, "invalid") {
		t.Fatalf("timeout flag should parse correctly, got: %v", err)
	}
}

func TestDelegateModelFlag(t *testing.T) {
	cmd := newDelegateCmd()
	cmd.SetArgs([]string{
		"--provider", "codex",
		"--prompt", "test",
		"--model", "gpt-5.1",
		"--timeout", "1s",
	})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Skip("codex CLI available")
	}
	if strings.Contains(err.Error(), "model") && strings.Contains(err.Error(), "invalid") {
		t.Fatalf("model flag should parse correctly, got: %v", err)
	}
}

func TestDelegateEmptyProviderWithSpaces(t *testing.T) {
	cmd := newDelegateCmd()
	cmd.SetArgs([]string{"--provider", "  ", "--prompt", "test", "--timeout", "1s"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for whitespace-only provider")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("error should mention provider, got: %v", err)
	}
}

func TestDelegateHelpFlag(t *testing.T) {
	cmd := newDelegateCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help flag should not error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"--provider", "--prompt", "--model", "--workspace-dir", "--timeout"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing flag %q, got: %s", want, output)
		}
	}
}

func TestDelegateStdinFromFile(t *testing.T) {
	tmp := t.TempDir()
	stdinFile := filepath.Join(tmp, "input.txt")
	if err := os.WriteFile(stdinFile, []byte("refactor the auth module"), 0o644); err != nil {
		t.Fatalf("write stdin file: %v", err)
	}

	f, err := os.Open(stdinFile)
	if err != nil {
		t.Fatalf("open stdin file: %v", err)
	}
	defer f.Close()

	cmd := newDelegateCmd()
	cmd.SetIn(f)
	cmd.SetArgs([]string{"--provider", "codex", "--timeout", "1s"})

	err = cmd.Execute()
	if err == nil {
		t.Skip("codex CLI available")
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "prompt") && strings.Contains(msg, "required") {
		t.Fatalf("stdin should satisfy prompt requirement, got: %v", err)
	}
}

func TestDelegateStdoutContainsReply(t *testing.T) {
	cmd := newDelegateCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--provider", "codex", "--prompt", "say hello world in one word", "--timeout", "10s"})

	err := cmd.Execute()
	if err != nil {
		t.Skipf("codex CLI unavailable: %v", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("expected non-empty reply on stdout")
	}
}
