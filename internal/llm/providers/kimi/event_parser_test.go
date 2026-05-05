package kimi

import (
	"strings"
	"testing"
)

func TestParseEventLine_AssistantContentString(t *testing.T) {
	event := parseEventLine(`{"role":"assistant","content":"  最终答复  "}`)

	if event.Text != "最终答复" {
		t.Fatalf("expected text %q, got %q", "最终答复", event.Text)
	}
	if event.ToolCall != "" {
		t.Fatalf("expected empty tool call, got %q", event.ToolCall)
	}
}

func TestParseEventLine_AssistantContentArrayAndToolCalls(t *testing.T) {
	event := parseEventLine(`{"role":"assistant","content":[{"type":"text","text":"第一行"},{"type":"think","text":"隐藏思考"},{"type":"text","text":"第二行"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"query\":\"kimi\"}"}}]}`)

	if event.Text != "第一行\n第二行" {
		t.Fatalf("expected text %q, got %q", "第一行\n第二行", event.Text)
	}
	if !strings.Contains(event.ToolCall, "function") {
		t.Fatalf("expected tool call to contain function type, got %q", event.ToolCall)
	}
	if !strings.Contains(event.ToolCall, "id=`call_1`") {
		t.Fatalf("expected tool call to contain id, got %q", event.ToolCall)
	}
	if !strings.Contains(event.ToolCall, "name=`search`") {
		t.Fatalf("expected tool call to contain function name, got %q", event.ToolCall)
	}
	if !strings.Contains(event.ToolCall, "arguments=`{\"query\":\"kimi\"}`") {
		t.Fatalf("expected tool call to contain arguments, got %q", event.ToolCall)
	}
}

func TestParseEventLine_IgnoresToolRole(t *testing.T) {
	event := parseEventLine(`{"role":"tool","content":"tool output should not become the final assistant message"}`)

	if event.Text != "" {
		t.Fatalf("expected empty text for tool role, got %q", event.Text)
	}
	if event.ToolCall != "" {
		t.Fatalf("expected empty tool call for tool role, got %q", event.ToolCall)
	}
}

func TestParseFinalMessage_UsesLastAssistantAndIgnoresToolRole(t *testing.T) {
	output := strings.Join([]string{
		`{"role":"assistant","content":"第一轮回答"}`,
		`{"role":"tool","content":"工具输出"}`,
		`{"role":"assistant","content":[{"type":"text","text":"最终回答"}]}`,
	}, "\n")

	msg, err := ParseFinalMessage(output)
	if err != nil {
		t.Fatalf("ParseFinalMessage returned error: %v", err)
	}
	if msg != "最终回答" {
		t.Fatalf("expected final message %q, got %q", "最终回答", msg)
	}
}

func TestParseFinalMessage_NoAssistantMessage(t *testing.T) {
	_, err := ParseFinalMessage(`{"role":"tool","content":"only tool output"}`)
	if err == nil {
		t.Fatal("expected error when no assistant message is present")
	}
}
