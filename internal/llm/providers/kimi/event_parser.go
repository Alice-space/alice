package kimi

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

type parsedEvent struct {
	SessionID string
	Text      string
	ToolCall  string
}

// Kimi CLI persists token usage in internal session files (`wire.jsonl`
// StatusUpdate.payload.token_usage and `context.jsonl` `_usage` rows), but
// `kimi --print --output-format stream-json` does not emit those events on
// stdout. The installed JsonPrinter only forwards assistant messages,
// notifications, tool results, and plan displays, so agentbridge cannot obtain
// per-run token usage from the JSONL stream it parses here.
func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastAssistant string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, shared.DefaultScannerBuf), shared.MaxScannerTokenSize)

	for scanner.Scan() {
		event := parseEventLine(scanner.Text())
		if strings.TrimSpace(event.Text) != "" {
			lastAssistant = strings.TrimSpace(event.Text)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(lastAssistant) == "" {
		return "", errors.New("kimi returned no final assistant message")
	}
	return lastAssistant, nil
}

func parseEventLine(line string) parsedEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return parsedEvent{}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return parsedEvent{}
	}

	message := payload
	if nested, ok := payload["message"].(map[string]any); ok {
		message = nested
	}

	sessionID := shared.ExtractString(payload, "session_id", "sessionId", "thread_id", "threadId")
	if sessionID == "" {
		sessionID = shared.ExtractString(message, "session_id", "sessionId", "thread_id", "threadId")
	}

	role := strings.ToLower(strings.TrimSpace(shared.ExtractString(message, "role")))
	switch role {
	case "assistant":
		return parsedEvent{
			SessionID: sessionID,
			Text:      parseAssistantText(message["content"]),
			ToolCall:  parseToolCalls(message["tool_calls"]),
		}
	case "tool":
		return parsedEvent{SessionID: sessionID}
	default:
		return parsedEvent{SessionID: sessionID}
	}
}

func parseAssistantText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			switch block := raw.(type) {
			case string:
				text := strings.TrimSpace(block)
				if text != "" {
					parts = append(parts, text)
				}
			case map[string]any:
				itemType := strings.ToLower(strings.TrimSpace(shared.ExtractString(block, "type")))
				switch itemType {
				case "", "text":
					text := strings.TrimSpace(shared.ExtractString(block, "text"))
					if text != "" {
						parts = append(parts, text)
					}
				case "thinking", "think":
					continue
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func parseToolCalls(raw any) string {
	calls, ok := raw.([]any)
	if !ok || len(calls) == 0 {
		return ""
	}

	parts := make([]string, 0, len(calls))
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, formatToolCall(call))
	}
	return strings.Join(parts, "; ")
}

func formatToolCall(call map[string]any) string {
	parts := make([]string, 0, 4)

	callType := strings.TrimSpace(shared.ExtractString(call, "type"))
	if callType == "" {
		callType = "tool_call"
	}
	parts = append(parts, callType)

	if id := strings.TrimSpace(shared.ExtractString(call, "id")); id != "" {
		parts = append(parts, "id=`"+id+"`")
	}

	name, arguments := extractToolFunction(call)
	if name != "" {
		parts = append(parts, "name=`"+name+"`")
	}
	if arguments != "" {
		parts = append(parts, "arguments=`"+arguments+"`")
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func extractToolFunction(call map[string]any) (string, string) {
	function := map[string]any{}
	if nested, ok := call["function"].(map[string]any); ok {
		function = nested
	}

	name := strings.TrimSpace(shared.ExtractString(function, "name"))
	if name == "" {
		name = strings.TrimSpace(shared.ExtractString(call, "name"))
	}

	arguments := strings.TrimSpace(shared.ExtractString(function, "arguments", "args"))
	if arguments == "" {
		arguments = strings.TrimSpace(shared.ExtractString(call, "arguments", "args"))
	}

	return name, arguments
}
