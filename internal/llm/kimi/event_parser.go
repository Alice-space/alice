package kimi

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
)

type parsedEvent struct {
	SessionID string
	Text      string
	ToolCall  string
}

func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastAssistant string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

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

	sessionID := extractString(payload, "session_id", "sessionId", "thread_id", "threadId")
	if sessionID == "" {
		sessionID = extractString(message, "session_id", "sessionId", "thread_id", "threadId")
	}

	role := strings.ToLower(strings.TrimSpace(extractString(message, "role")))
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
				itemType := strings.ToLower(strings.TrimSpace(extractString(block, "type")))
				switch itemType {
				case "", "text":
					text := strings.TrimSpace(extractString(block, "text"))
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

	callType := strings.TrimSpace(extractString(call, "type"))
	if callType == "" {
		callType = "tool_call"
	}
	parts = append(parts, callType)

	if id := strings.TrimSpace(extractString(call, "id")); id != "" {
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

	name := strings.TrimSpace(extractString(function, "name"))
	if name == "" {
		name = strings.TrimSpace(extractString(call, "name"))
	}

	arguments := strings.TrimSpace(extractString(function, "arguments", "args"))
	if arguments == "" {
		arguments = strings.TrimSpace(extractString(call, "arguments", "args"))
	}

	return name, arguments
}

func extractString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
