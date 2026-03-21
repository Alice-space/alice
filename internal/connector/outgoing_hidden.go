package connector

import (
	"regexp"
	"strings"
)

var replyWillBlockPattern = regexp.MustCompile(`(?is)<reply_will\b[^>]*>.*?</reply_will>`)

func stripHiddenReplyMetadata(reply string) string {
	if strings.TrimSpace(reply) == "" {
		return ""
	}
	stripped := replyWillBlockPattern.ReplaceAllString(reply, "")
	return strings.TrimSpace(stripped)
}
