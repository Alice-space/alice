package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMCPServerConfig(t *testing.T) {
	output := `alice-feishu:
  Scope: Local config (private to you in this project)
  Status: ✓ Connected
  Type: stdio
  Command: /home/codexbot/alice/bin/alice-mcp-server
  Args: -c /home/codexbot/alice/config.yaml
  Environment:
`
	config, err := parseMCPServerConfig(output)
	if err != nil {
		t.Fatalf("parse mcp config failed: %v", err)
	}
	if config.TransportType != "stdio" {
		t.Fatalf("unexpected transport type: %q", config.TransportType)
	}
	if config.Command != "/home/codexbot/alice/bin/alice-mcp-server" {
		t.Fatalf("unexpected command: %q", config.Command)
	}
	if len(config.Args) != 2 || config.Args[0] != "-c" || config.Args[1] != "/home/codexbot/alice/config.yaml" {
		t.Fatalf("unexpected args: %#v", config.Args)
	}
}

func TestMCPServerMatches(t *testing.T) {
	current := mcpServerConfig{
		TransportType: "stdio",
		Command:       "/bin/alice-mcp-server",
		Args:          []string{"-c", "/tmp/config.yaml"},
	}

	if !mcpServerMatches(current, "/bin/alice-mcp-server", []string{"-c", "/tmp/config.yaml"}) {
		t.Fatal("expected server config to match")
	}
	if mcpServerMatches(current, "/bin/other", []string{"-c", "/tmp/config.yaml"}) {
		t.Fatal("command mismatch should not match")
	}
	if mcpServerMatches(current, "/bin/alice-mcp-server", []string{"-c", "/tmp/other.yaml"}) {
		t.Fatal("args mismatch should not match")
	}
}

func TestIsServerNotFoundError(t *testing.T) {
	if !isServerNotFoundError("No MCP server found with name: alice-feishu") {
		t.Fatal("expected not found error to be detected")
	}
	if !isServerNotFoundError("No project-local MCP server found with name: alice-feishu") {
		t.Fatal("expected scoped not found error to be detected")
	}
	if isServerNotFoundError("other error") {
		t.Fatal("unexpected not found match for unrelated error")
	}
}

func TestEnsureMCPServerRegistered_AddWhenMissing(t *testing.T) {
	tempDir := t.TempDir()
	tracePath := filepath.Join(tempDir, "trace.log")
	fakeClaudePath := filepath.Join(tempDir, "fake-claude.sh")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "mcp" ] && [ "$2" = "get" ]; then
  echo "No MCP server found with name: $3"
  exit 1
fi
if [ "$1" = "mcp" ] && [ "$2" = "add" ]; then
  echo "$@" > %q
  exit 0
fi
echo "unexpected command: $@" >&2
exit 2
`, tracePath)
	if err := os.WriteFile(fakeClaudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script failed: %v", err)
	}

	err := EnsureMCPServerRegistered(context.Background(), MCPRegistration{
		ClaudeCommand: fakeClaudePath,
		ServerName:    "alice-feishu",
		ServerCommand: "/bin/alice-mcp-server",
		ServerArgs:    []string{"-c", "/tmp/config.yaml"},
	})
	if err != nil {
		t.Fatalf("ensure mcp server failed: %v", err)
	}

	traceBytes, readErr := os.ReadFile(tracePath)
	if readErr != nil {
		t.Fatalf("read trace failed: %v", readErr)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "mcp add alice-feishu -- /bin/alice-mcp-server -c /tmp/config.yaml") {
		t.Fatalf("unexpected add command trace: %q", trace)
	}
}

func TestEnsureMCPServerRegistered_NoOpWhenMatched(t *testing.T) {
	tempDir := t.TempDir()
	tracePath := filepath.Join(tempDir, "trace.log")
	fakeClaudePath := filepath.Join(tempDir, "fake-claude.sh")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "mcp" ] && [ "$2" = "get" ]; then
  cat <<'EOF'
alice-feishu:
  Scope: Local config (private to you in this project)
  Status: ✓ Connected
  Type: stdio
  Command: /bin/alice-mcp-server
  Args: -c /tmp/config.yaml
  Environment:
EOF
  exit 0
fi
echo "$@" >> %q
exit 0
`, tracePath)
	if err := os.WriteFile(fakeClaudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script failed: %v", err)
	}

	err := EnsureMCPServerRegistered(context.Background(), MCPRegistration{
		ClaudeCommand: fakeClaudePath,
		ServerName:    "alice-feishu",
		ServerCommand: "/bin/alice-mcp-server",
		ServerArgs:    []string{"-c", "/tmp/config.yaml"},
	})
	if err != nil {
		t.Fatalf("ensure mcp server failed: %v", err)
	}

	traceBytes, _ := os.ReadFile(tracePath)
	if strings.TrimSpace(string(traceBytes)) != "" {
		t.Fatalf("expected no remove/add command, got: %q", string(traceBytes))
	}
}

func TestEnsureMCPServerRegistered_ReplacesStaleConfig(t *testing.T) {
	tempDir := t.TempDir()
	tracePath := filepath.Join(tempDir, "trace.log")
	fakeClaudePath := filepath.Join(tempDir, "fake-claude.sh")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "mcp" ] && [ "$2" = "get" ]; then
  cat <<'EOF'
alice-feishu:
  Scope: Local config (private to you in this project)
  Status: ✗ Failed to connect
  Type: stdio
  Command: /bin/old-mcp-server
  Args: -c /tmp/old.yaml
  Environment:
EOF
  exit 0
fi
if [ "$1" = "mcp" ] && [ "$2" = "remove" ]; then
  echo "remove:$@" >> %q
  exit 0
fi
if [ "$1" = "mcp" ] && [ "$2" = "add" ]; then
  echo "add:$@" >> %q
  exit 0
fi
echo "unexpected command: $@" >&2
exit 2
`, tracePath, tracePath)
	if err := os.WriteFile(fakeClaudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script failed: %v", err)
	}

	err := EnsureMCPServerRegistered(context.Background(), MCPRegistration{
		ClaudeCommand: fakeClaudePath,
		ServerName:    "alice-feishu",
		ServerCommand: "/bin/alice-mcp-server",
		ServerArgs:    []string{"-c", "/tmp/config.yaml"},
	})
	if err != nil {
		t.Fatalf("ensure mcp server failed: %v", err)
	}

	traceBytes, readErr := os.ReadFile(tracePath)
	if readErr != nil {
		t.Fatalf("read trace failed: %v", readErr)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "remove:mcp remove alice-feishu") {
		t.Fatalf("expected remove command, got: %q", trace)
	}
	if !strings.Contains(trace, "add:mcp add alice-feishu -- /bin/alice-mcp-server -c /tmp/config.yaml") {
		t.Fatalf("expected add command, got: %q", trace)
	}
}
