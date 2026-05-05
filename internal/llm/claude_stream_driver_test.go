package llm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeStreamDriverNativeEnqueue(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stdin.jsonl")
	command := writeClaudeFake(t, dir, `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"claude-session"}'
i=0
while IFS= read -r line; do
  i=$((i + 1))
  printf '%s\n' "$line" >> "$CLAUDE_STDIN_LOG"
  if [ "$i" -eq 1 ]; then
    echo '{"type":"assistant","session_id":"claude-session","message":{"role":"assistant","content":[{"type":"text","text":"first accepted"}]}}'
  else
    echo '{"type":"assistant","session_id":"claude-session","message":{"role":"assistant","content":[{"type":"text","text":"second accepted"}]}}'
    echo '{"type":"result","session_id":"claude-session","is_error":false,"usage":{"input_tokens":3,"cache_read_input_tokens":1,"output_tokens":4}}'
  fi
done
`)

	driver := newClaudeStreamDriver(ClaudeConfig{
		Command: command,
		Env:     map[string]string{"CLAUDE_STDIN_LOG": logPath},
	})
	session := NewInteractiveSession(driver)
	defer session.Close()

	first, err := session.Submit(context.Background(), RunRequest{UserText: "first"})
	if err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	if first.Mode != SubmitStarted {
		t.Fatalf("first mode = %q, want %q", first.Mode, SubmitStarted)
	}
	waitForJSONLines(t, logPath, 1)

	second, err := session.Submit(context.Background(), RunRequest{UserText: "second"})
	if err != nil {
		t.Fatalf("second submit failed: %v", err)
	}
	if second.Mode != SubmitSteered {
		t.Fatalf("second mode = %q, want %q", second.Mode, SubmitSteered)
	}
	lines := waitForJSONLines(t, logPath, 2)
	if !strings.Contains(lines[0], "first") || !strings.Contains(lines[1], "second") {
		t.Fatalf("stdin lines = %#v, want first and second prompts", lines)
	}
	completed := waitForTurnEvent(t, session.Events(), TurnEventCompleted)
	if completed.Usage.InputTokens != 3 || completed.Usage.CachedInputTokens != 1 || completed.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %#v", completed.Usage)
	}
}

func TestClaudeStreamDriverInterruptWritesControlRequest(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stdin.jsonl")
	command := writeClaudeFake(t, dir, `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"claude-session"}'
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$CLAUDE_STDIN_LOG"
done
`)

	driver := newClaudeStreamDriver(ClaudeConfig{
		Command: command,
		Env:     map[string]string{"CLAUDE_STDIN_LOG": logPath},
	})
	session := NewInteractiveSession(driver)
	defer session.Close()

	if _, err := session.Submit(context.Background(), RunRequest{UserText: "first"}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	waitForJSONLines(t, logPath, 1)
	if err := session.Interrupt(context.Background()); err != nil {
		t.Fatalf("interrupt failed: %v", err)
	}
	lines := waitForJSONLines(t, logPath, 2)

	var control map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &control); err != nil {
		t.Fatalf("decode control line: %v", err)
	}
	request, _ := control["request"].(map[string]any)
	if control["type"] != "control_request" || request["subtype"] != "interrupt" {
		t.Fatalf("control line = %#v, want interrupt control_request", control)
	}
}

func writeClaudeFake(t *testing.T, dir string, script string) string {
	t.Helper()
	path := filepath.Join(dir, "claude-fake.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude command: %v", err)
	}
	return path
}

func waitForJSONLines(t *testing.T, path string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		lines := splitNonEmptyLines(string(raw))
		if len(lines) >= want {
			return lines
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %d lines in %s; got %q", want, path, string(raw))
	return nil
}

func splitNonEmptyLines(text string) []string {
	raw := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
