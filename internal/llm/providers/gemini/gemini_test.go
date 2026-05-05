package gemini

import (
	"strings"
	"testing"
)

func TestParseJSONResponse_Valid(t *testing.T) {
	raw := `{"session_id":"sess-1","response":"hello","stats":{"models":{"gemini-2.5-pro":{"tokens":{"input":12,"cached":3,"candidates":4}}}}}`
	resp, err := parseJSONResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionID != "sess-1" {
		t.Fatalf("expected session_id=sess-1, got %q", resp.SessionID)
	}
	if resp.Response != "hello" {
		t.Fatalf("expected response=hello, got %q", resp.Response)
	}
	inputTokens, cachedInputTokens, outputTokens := resp.usageTotals()
	if inputTokens != 12 || cachedInputTokens != 3 || outputTokens != 4 {
		t.Fatalf("unexpected usage totals: input=%d cached=%d output=%d", inputTokens, cachedInputTokens, outputTokens)
	}
}

func TestParseJSONResponse_EmptyResponse(t *testing.T) {
	raw := `{"session_id":"sess-1","response":""}`
	_, err := parseJSONResponse(raw)
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "no final response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseJSONResponse_InvalidJSON(t *testing.T) {
	_, err := parseJSONResponse("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBuildExecArgs_NewSession(t *testing.T) {
	args := buildExecArgs("", "hello", "gemini-2.5-pro")
	if len(args) < 2 {
		t.Fatalf("too few args: %v", args)
	}
	// prompt via -p
	pIdx := -1
	for i, a := range args {
		if a == "-p" {
			pIdx = i
			break
		}
	}
	if pIdx < 0 || args[pIdx+1] != "hello" {
		t.Fatalf("expected -p hello, got %v", args)
	}
	// --output-format json
	found := false
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --output-format json, got %v", args)
	}
}

func TestBuildExecArgs_ResumeSession(t *testing.T) {
	args := buildExecArgs("sess-abc", "continue", "")
	found := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "sess-abc" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --resume sess-abc, got %v", args)
	}
}

func TestDecorateNodeVersionError_Node18(t *testing.T) {
	base := &testError{"gemini exec failed"}
	detail := "invalid regular expression flags: Node.js v18.0.0"
	err := decorateNodeVersionError(base, detail)
	if !strings.Contains(err.Error(), "Node >= 20") {
		t.Fatalf("expected node version hint in error, got: %v", err)
	}
}

func TestDecorateNodeVersionError_NoDecoration(t *testing.T) {
	base := &testError{"some other error"}
	err := decorateNodeVersionError(base, "unrelated detail")
	if err.Error() != base.Error() {
		t.Fatalf("expected undecorated error, got: %v", err)
	}
}

func TestLimitedBuffer_CapsToBound(t *testing.T) {
	b := &limitedBuffer{limit: 5}
	n, err := b.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 11 {
		t.Fatalf("Write must report full length written, got %d", n)
	}
	if b.String() != "hello" {
		t.Fatalf("expected capped to 5 bytes, got %q", b.String())
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
