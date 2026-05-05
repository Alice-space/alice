package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const loginCheckTestTimeout = 15 * time.Second

func TestCheckLogin_LoggedIn(t *testing.T) {
	command := writeClaudeStub(t, `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  printf '{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}\n'
  exit 0
fi
printf 'unexpected args=%s %s\n' "$1" "$2" >&2
exit 99
`)

	report, err := CheckLogin(command, loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if !report.LoggedIn {
		t.Fatalf("expected logged-in report, got %#v", report)
	}
	if report.AuthMethod != "oauth_token" {
		t.Fatalf("unexpected auth method: %q", report.AuthMethod)
	}
	if report.APIProvider != "firstParty" {
		t.Fatalf("unexpected api provider: %q", report.APIProvider)
	}
}

func TestCheckLogin_LoggedOut(t *testing.T) {
	command := writeClaudeStub(t, `#!/bin/sh
printf '{"loggedIn":false,"authMethod":"","apiProvider":""}\n'
exit 1
`)

	report, err := CheckLogin(command, loginCheckTestTimeout)
	if err != nil {
		t.Fatalf("check login failed: %v", err)
	}
	if report.LoggedIn {
		t.Fatalf("expected logged-out report, got %#v", report)
	}
	if report.Output != `{"loggedIn":false,"authMethod":"","apiProvider":""}` {
		t.Fatalf("unexpected output: %q", report.Output)
	}
}

func writeClaudeStub(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "claude-stub.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write claude stub failed: %v", err)
	}
	return path
}
