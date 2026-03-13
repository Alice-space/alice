package kimi

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
)

type parsedEvent struct {
	Text     string
	ToolCall string
}

func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastMessage string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		event := parseEventLine(scanner.Text())
		if strings.TrimSpace(event.Text) != "" {
			lastMessage = strings.TrimSpace(event.Text)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(lastMessage) == "" {
		return "", errors.New("kimi returned no final assistant message")
	}
	return lastMessage, nil
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
	role, _ := payload["role"].(string)
	if role != "assistant" {
		return parsedEvent{}
	}
	content, ok := payload["content"].([]any)
	if !ok {
		return parsedEvent{}
	}

	textParts := make([]string, 0, len(content))
	toolParts := make([]string, 0, len(content))
	for _, raw := range content {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := item["type"].(string)
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "text":
			text, _ := item["text"].(string)
			if strings.TrimSpace(text) != "" {
				textParts = append(textParts, strings.TrimSpace(text))
			}
		case "think":
			continue
		default:
			if strings.TrimSpace(itemType) == "" {
				continue
			}
			summary := strings.TrimSpace(itemType)
			if name, _ := item["name"].(string); strings.TrimSpace(name) != "" {
				summary += " name=`" + strings.TrimSpace(name) + "`"
			}
			if id, _ := item["id"].(string); strings.TrimSpace(id) != "" {
				summary += " id=`" + strings.TrimSpace(id) + "`"
			}
			toolParts = append(toolParts, summary)
		}
	}
	return parsedEvent{
		Text:     strings.Join(textParts, "\n"),
		ToolCall: strings.Join(toolParts, "; "),
	}
}
