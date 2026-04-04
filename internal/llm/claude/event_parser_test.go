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
