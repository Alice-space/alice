package prompting

import (
	"strings"
)

func ComposePromptPrefix(promptPrefix string, personality string, noReplyToken string) (string, error) {
	parts := make([]string, 0, 3)
	if prefix := strings.TrimSpace(promptPrefix); prefix != "" {
		parts = append(parts, prefix)
	}
	if personality = strings.TrimSpace(personality); personality != "" {
		parts = append(parts, "Preferred response style/personality: "+personality+".")
	}
	if noReplyToken = strings.TrimSpace(noReplyToken); noReplyToken != "" {
		parts = append(parts, "If no reply is appropriate, return exactly this token and nothing else: "+noReplyToken)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n")), nil
}
