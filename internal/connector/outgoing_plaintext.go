package connector

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	atTokenPattern             = regexp.MustCompile(`(?s)<at\b[^>]*>.*?</at>`)
	plainTextFencePattern      = regexp.MustCompile("(?s)```(?:[^`\n]*)\n?(.*?)```")
	plainTextLinkPattern       = regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)
	plainTextHeadingPattern    = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	plainTextQuotePattern      = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)
	plainTextListPattern       = regexp.MustCompile(`(?m)^[ \t]*([-*+]|\d+\.)[ \t]+`)
	plainTextBoldPattern       = regexp.MustCompile(`\*\*(.*?)\*\*|__(.*?)__`)
	plainTextItalicPattern     = regexp.MustCompile(`\*(.*?)\*|_(.*?)_`)
	plainTextStrikethrough     = regexp.MustCompile(`~~(.*?)~~`)
	plainTextInlineCodePattern = regexp.MustCompile("`([^`]+)`")
	plainTextWhitespacePattern = regexp.MustCompile(`[ \t]+\n`)
	plainTextBlankLinePattern  = regexp.MustCompile(`\n{3,}`)
	plainTextSpaceCollapse     = regexp.MustCompile(`[ \t]{2,}`)
)

func sanitizeMarkdownForPlainText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	protected := make([]string, 0, 4)
	text = atTokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		placeholder := plainTextPlaceholder(len(protected))
		protected = append(protected, token)
		return placeholder
	})

	text = plainTextFencePattern.ReplaceAllString(text, "$1")
	text = plainTextLinkPattern.ReplaceAllString(text, "$1")
	text = plainTextHeadingPattern.ReplaceAllString(text, "")
	text = plainTextQuotePattern.ReplaceAllString(text, "")
	text = plainTextListPattern.ReplaceAllString(text, "- ")
	text = plainTextBoldPattern.ReplaceAllStringFunc(text, markdownCaptureContent)
	text = plainTextItalicPattern.ReplaceAllStringFunc(text, markdownCaptureContent)
	text = plainTextStrikethrough.ReplaceAllString(text, "$1")
	text = plainTextInlineCodePattern.ReplaceAllString(text, "$1")
	text = plainTextWhitespacePattern.ReplaceAllString(text, "\n")
	text = plainTextBlankLinePattern.ReplaceAllString(text, "\n\n")
	text = plainTextSpaceCollapse.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	for idx, token := range protected {
		text = strings.ReplaceAll(text, plainTextPlaceholder(idx), token)
	}
	return strings.TrimSpace(text)
}

func markdownCaptureContent(match string) string {
	switch {
	case strings.HasPrefix(match, "**") && strings.HasSuffix(match, "**") && len(match) >= 4:
		return match[2 : len(match)-2]
	case strings.HasPrefix(match, "__") && strings.HasSuffix(match, "__") && len(match) >= 4:
		return match[2 : len(match)-2]
	case strings.HasPrefix(match, "*") && strings.HasSuffix(match, "*") && len(match) >= 2:
		return match[1 : len(match)-1]
	case strings.HasPrefix(match, "_") && strings.HasSuffix(match, "_") && len(match) >= 2:
		return match[1 : len(match)-1]
	default:
		return match
	}
}

func plainTextPlaceholder(index int) string {
	return "\x00AT" + strconv.Itoa(index) + "\x00"
}
