package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastMessage string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		_, text, _, _ := parseEventLine(line)
		if strings.TrimSpace(text) != "" {
			lastMessage = text
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(lastMessage) == "" {
		return "", errors.New("codex returned no final agent message")
	}
	return lastMessage, nil
}

func parseEventLine(line string) (reasoning string, agentMessage string, fileChangeMessage string, threadID string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", "", ""
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return "", "", "", ""
	}

	eventType, _ := event["type"].(string)
	if eventType == "thread.started" {
		id, _ := event["thread_id"].(string)
		return "", "", "", strings.TrimSpace(id)
	}
	if eventType != "item.completed" {
		return "", "", "", ""
	}

	item, ok := event["item"].(map[string]any)
	if !ok {
		return "", "", "", ""
	}
	itemType, _ := item["type"].(string)
	text, _ := item["text"].(string)
	switch itemType {
	case "reasoning":
		return text, "", "", ""
	case "agent_message":
		return "", text, "", ""
	case "file_change":
		return "", "", parseFileChangeMessage(item), ""
	default:
		return "", "", "", ""
	}
}

func parseToolCallLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return ""
	}
	eventType, _ := event["type"].(string)
	if eventType != "item.completed" && eventType != "item.started" {
		return ""
	}
	item, ok := event["item"].(map[string]any)
	if !ok {
		return ""
	}
	itemType, _ := item["type"].(string)
	if itemType != "command_execution" {
		return ""
	}

	command := extractString(item, "command")
	status := extractString(item, "status")
	exitCode := extractInt(item, "exit_code")
	parts := make([]string, 0, 3)
	if command != "" {
		parts = append(parts, "command=`"+command+"`")
	}
	if status != "" {
		parts = append(parts, "status=`"+status+"`")
	}
	if eventType == "item.completed" {
		parts = append(parts, "exit_code=`"+strconv.Itoa(exitCode)+"`")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func parseUsageLine(line string) Usage {
	line = strings.TrimSpace(line)
	if line == "" {
		return Usage{}
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return Usage{}
	}
	eventType, _ := event["type"].(string)
	if eventType != "turn.completed" {
		return Usage{}
	}

	usagePayload, ok := event["usage"].(map[string]any)
	if !ok {
		return Usage{}
	}
	return Usage{
		InputTokens:       int64(extractInt(usagePayload, "input_tokens", "prompt_tokens")),
		CachedInputTokens: int64(extractInt(usagePayload, "cached_input_tokens")),
		OutputTokens:      int64(extractInt(usagePayload, "output_tokens", "completion_tokens")),
	}
}
