package connector

import (
	"regexp"
	"strings"
)

func stripHiddenReplyMetadata(reply string, contract outputContract) string {
	if strings.TrimSpace(reply) == "" {
		return ""
	}
	stripped := reply
	for _, tag := range contract.hiddenTags() {
		stripped = hiddenTagBlockPattern(tag).ReplaceAllString(stripped, "")
	}
	return strings.TrimSpace(stripped)
}

func hiddenTagBlockPattern(tag string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `\b[^>]*>.*?</` + regexp.QuoteMeta(tag) + `>`)
}
