package claude

import (
	"slices"
	"testing"
)

func TestExtractAssistantToolNames(t *testing.T) {
	message := map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "ignore me",
			},
			map[string]any{
				"type": "tool_use",
				"name": "Bash",
				"id":   "toolu_1",
			},
			map[string]any{
				"type": "tool_use",
				"name": "Read",
				"id":   "toolu_2",
			},
			map[string]any{
				"type": "tool_use",
				"id":   "toolu_3",
			},
		},
	}

	names := extractAssistantToolNames(message)
	if !slices.Equal(names, []string{"Bash", "Read"}) {
		t.Fatalf("unexpected tool names: %#v", names)
	}
}

func TestParseEventLine_ResultWithUsage(t *testing.T) {
	line := `{"type":"result","session_id":"s1","result":"hello","is_error":false,"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":20}}`

	event := parseEventLine(line)

	if event.InputTokens != 100 {
		t.Fatalf("unexpected input tokens: %d", event.InputTokens)
	}
	if event.OutputTokens != 50 {
		t.Fatalf("unexpected output tokens: %d", event.OutputTokens)
	}
	if event.CachedInputTokens != 20 {
		t.Fatalf("unexpected cached input tokens: %d", event.CachedInputTokens)
	}
	if !event.HasResultEvent {
		t.Fatal("expected result event flag to be set")
	}
}

func TestParseEventLine_ResultWithoutUsage(t *testing.T) {
	line := `{"type":"result","session_id":"s1","result":"hello","is_error":false}`

	event := parseEventLine(line)

	if event.InputTokens != 0 {
		t.Fatalf("unexpected input tokens: %d", event.InputTokens)
	}
	if event.OutputTokens != 0 {
		t.Fatalf("unexpected output tokens: %d", event.OutputTokens)
	}
	if event.CachedInputTokens != 0 {
		t.Fatalf("unexpected cached input tokens: %d", event.CachedInputTokens)
	}
	if !event.HasResultEvent {
		t.Fatal("expected result event flag to be set")
	}
}
