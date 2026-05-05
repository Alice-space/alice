package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const loginCheckTestTimeout = 15 * time.Second

func TestCheckLogin_LoggedIn(t *testing.T) {
	home := t.TempDir()
	targetCodexHome := filepath.Join(home, ".codex-shared")
	t.Setenv("EXPECTED_CODEX_HOME", targetCodexHome)

	command := writeCodexStub(t, `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ] && [ "$CODEX_HOME" = "$EXPECTED_CODEX_HOME" ]; then
  printf 'Logged in using ChatGPT\n'
  exit 0
fi
printf 'unexpected args=%s %s CODEX_HOME=%s\n' "$1" "$2" "$CODEX_HOME" >&2
exit 99
`)

	report, err := CheckLogin(command, targetCodexHome, loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if !report.LoggedIn {
		t.Fatalf("expected logged-in report, got %#v", report)
	}
	if report.CodexHome != targetCodexHome {
		t.Fatalf("unexpected codex home: %q", report.CodexHome)
	}
	if report.Output != "Logged in using ChatGPT" {
		t.Fatalf("unexpected output: %q", report.Output)
	}
}

func TestCheckLogin_LoggedOut(t *testing.T) {
	targetCodexHome := filepath.Join(t.TempDir(), ".codex-shared")
	command := writeCodexStub(t, `#!/bin/sh
printf 'Not logged in\n'
exit 1
`)

	report, err := CheckLogin(command, targetCodexHome, loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if report.LoggedIn {
		t.Fatalf("expected logged-out report, got %#v", report)
	}
	if report.Output != "Not logged in" {
		t.Fatalf("unexpected output: %q", report.Output)
	}
}

func TestCheckLogin_UsesDefaultCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("EXPECTED_CODEX_HOME", filepath.Join(home, ".codex"))

	command := writeCodexStub(t, `#!/bin/sh
if [ "$CODEX_HOME" = "$EXPECTED_CODEX_HOME" ]; then
  printf 'Logged in using ChatGPT\n'
  exit 0
fi
printf 'unexpected CODEX_HOME=%s\n' "$CODEX_HOME" >&2
exit 2
`)

	report, err := CheckLogin(command, "", loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if !report.LoggedIn {
		t.Fatalf("expected logged-in report, got %#v", report)
	}
}

func writeCodexStub(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "codex-stub.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write codex stub failed: %v", err)
	}
	return path
}
