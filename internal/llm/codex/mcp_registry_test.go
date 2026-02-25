package codex

import "testing"

func TestParseMCPServerConfig(t *testing.T) {
	output := `{"name":"alice-feishu","transport":{"type":"stdio","command":"/home/codexbot/alice/bin/alice-mcp-server","args":["-c","/home/codexbot/alice/config.yaml"]}}`
	config, err := parseMCPServerConfig(output)
	if err != nil {
		t.Fatalf("parse mcp config failed: %v", err)
	}
	if config.Transport.Type != "stdio" {
		t.Fatalf("unexpected transport type: %q", config.Transport.Type)
	}
	if config.Transport.Command != "/home/codexbot/alice/bin/alice-mcp-server" {
		t.Fatalf("unexpected transport command: %q", config.Transport.Command)
	}
	if len(config.Transport.Args) != 2 || config.Transport.Args[0] != "-c" {
		t.Fatalf("unexpected transport args: %#v", config.Transport.Args)
	}
}

func TestMCPServerMatches(t *testing.T) {
	current := mcpServerConfig{}
	current.Transport.Type = "stdio"
	current.Transport.Command = "/bin/alice-mcp-server"
	current.Transport.Args = []string{"-c", "/tmp/config.yaml"}

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
	if !isServerNotFoundError("Error: No MCP server named 'alice-feishu' found.") {
		t.Fatal("expected not found error to be detected")
	}
	if isServerNotFoundError("other error") {
		t.Fatal("unexpected not found match for unrelated error")
	}
}
