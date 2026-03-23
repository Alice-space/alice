package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func extractText(content *string) (string, error) {
	return extractTextWithMentions(content, nil)
}

func extractOptionalTextWithMentions(content *string, mentions []*larkim.MentionEvent) (string, error) {
	text, err := extractTextWithMentions(content, mentions)
	if err != nil {
		if errors.Is(err, ErrIgnoreMessage) {
			return "", nil
		}
		return "", err
	}
	return text, nil
}

func extractTextWithMentions(content *string, mentions []*larkim.MentionEvent) (string, error) {
	if strings.TrimSpace(deref(content)) == "" {
		return "", ErrIgnoreMessage
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(deref(content)), &payload); err != nil {
		return "", fmt.Errorf("invalid text content json: %w", err)
	}

	stripped := mentionPattern.ReplaceAllString(payload.Text, "")
	stripped = stripMentionKeys(stripped, mentions)
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		return "", ErrIgnoreMessage
	}

	text := mentionPattern.ReplaceAllString(payload.Text, "")
	text = replaceMentionKeysWithDisplayNames(text, mentions)
	text = strings.TrimSpace(text)
	return text, nil
}

func stripMentionKeys(text string, mentions []*larkim.MentionEvent) string {
	cleaned := text
	for _, mention := range mentions {
		if mention == nil {
			continue
		}
		key := strings.TrimSpace(deref(mention.Key))
		if key == "" {
			continue
		}
		cleaned = strings.ReplaceAll(cleaned, key, "")
	}
	return cleaned
}

func replaceMentionKeysWithDisplayNames(text string, mentions []*larkim.MentionEvent) string {
	replaced := text
	for _, mention := range mentions {
		if mention == nil {
			continue
		}
		key := strings.TrimSpace(deref(mention.Key))
		if key == "" {
			continue
		}
		replaced = strings.ReplaceAll(replaced, key, formatMentionDisplayName(deref(mention.Name)))
	}
	return replaced
}

func formatMentionDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "@") {
		return name
	}
	return "@" + name
}
