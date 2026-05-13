package opencode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const loginCheckTestTimeout = 15 * time.Second

func TestCheckLogin_Ready(t *testing.T) {
	command := writeOpenCodeStub(t, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opencode v1.2.3\n'
  exit 0
fi
printf 'unexpected args=%s\n' "$1" >&2
exit 99
`)

	report, err := CheckLogin(command, loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if !report.Ready {
		t.Fatalf("expected ready report, got %#v", report)
	}
	if report.Version != "opencode v1.2.3" {
		t.Fatalf("unexpected version: %q", report.Version)
	}
}

func TestCheckLogin_NotReady(t *testing.T) {
	command := writeOpenCodeStub(t, `#!/bin/sh
printf 'error: not authenticated\n' >&2
exit 1
`)

	report, err := CheckLogin(command, loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if report.Ready {
		t.Fatalf("expected not-ready report, got %#v", report)
	}
}

func TestCheckLogin_DefaultCommand(t *testing.T) {
	command := writeOpenCodeStub(t, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opencode v2.0.0\n'
  exit 0
fi
printf 'unexpected args=%s\n' "$1" >&2
exit 99
`)

	report, err := CheckLogin(command, 0)
	if err != nil {
		t.Fatalf("check login with zero timeout failed: %v", err)
	}
	if !report.Ready {
		t.Fatalf("expected ready report with zero timeout, got %#v", report)
	}
	if report.Command != command {
		t.Fatalf("unexpected command: %q", report.Command)
	}
}

func writeOpenCodeStub(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "opencode-stub.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write opencode stub failed: %v", err)
	}
	return path
}
