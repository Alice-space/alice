package feishu

import (
	"encoding/json"
	"regexp"
	"strings"
)

var markdownLinkPattern = regexp.MustCompile(`\[(.*?)\]\((https?://[^\s)]+)\)`)
var markdownStrongPattern = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var markdownEmPattern = regexp.MustCompile(`\*([^*]+)\*`)
var markdownStrongUnderscorePattern = regexp.MustCompile(`__([^_]+)__`)
var markdownEmUnderscorePattern = regexp.MustCompile(`_([^_]+)_`)
var markdownStrikePattern = regexp.MustCompile(`~~([^~]+)~~`)

func extractReplyTextFromCard(content string) string {
	var payload struct {
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return ""
	}

	const replyPrefix = "**回复**\n"
	for _, element := range payload.Body.Elements {
		if strings.ToLower(strings.TrimSpace(element.Tag)) != "markdown" {
			continue
		}
		md := strings.TrimSpace(element.Content)
		if md == "" {
			continue
		}
		if strings.HasPrefix(md, replyPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(md, replyPrefix))
		}
	}
	return ""
}

func extractTextFromPost(content string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return ""
	}

	var locale map[string]any
	for _, key := range []string{"zh_cn", "en_us"} {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if parsed, ok := value.(map[string]any); ok {
			locale = parsed
			break
		}
	}
	if locale == nil {
		return ""
	}

	contentRows, ok := locale["content"].([]any)
	if !ok || len(contentRows) == 0 {
		return ""
	}

	lines := make([]string, 0, len(contentRows))
	for _, row := range contentRows {
		elements, ok := row.([]any)
		if !ok {
			continue
		}
		var lineBuilder strings.Builder
		for _, element := range elements {
			item, ok := element.(map[string]any)
			if !ok {
				continue
			}
			tag, _ := item["tag"].(string)
			normalizedTag := strings.ToLower(strings.TrimSpace(tag))
			switch normalizedTag {
			case "text":
				text, _ := item["text"].(string)
				if strings.TrimSpace(text) == "" {
					continue
				}
				if lineBuilder.Len() > 0 {
					lineBuilder.WriteString(" ")
				}
				lineBuilder.WriteString(strings.TrimSpace(text))
			case "a":
				text, _ := item["text"].(string)
				href, _ := item["href"].(string)
				value := strings.TrimSpace(text)
				if value == "" {
					value = strings.TrimSpace(href)
				}
				if value == "" {
					continue
				}
				if lineBuilder.Len() > 0 {
					lineBuilder.WriteString(" ")
				}
				lineBuilder.WriteString(value)
			}
		}
		line := strings.TrimSpace(lineBuilder.String())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func textMessageContent(text string) string {
	contentBytes, _ := json.Marshal(map[string]string{"text": text})
	return string(contentBytes)
}

func imageMessageContent(imageKey string) string {
	contentBytes, _ := json.Marshal(map[string]string{"image_key": strings.TrimSpace(imageKey)})
	return string(contentBytes)
}

func fileMessageContent(fileKey string) string {
	contentBytes, _ := json.Marshal(map[string]string{"file_key": strings.TrimSpace(fileKey)})
	return string(contentBytes)
}

func richTextMessageContent(lines []string) string {
	paragraphs := make([][]map[string]any, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		paragraphs = append(paragraphs, []map[string]any{
			{
				"tag":  "text",
				"text": line,
			},
		})
	}
	if len(paragraphs) == 0 {
		paragraphs = append(paragraphs, []map[string]any{
			{
				"tag":  "text",
				"text": " ",
			},
		})
	}

	contentBytes, _ := json.Marshal(map[string]any{
		"zh_cn": map[string]any{
			"title":   "",
			"content": paragraphs,
		},
	})
	return string(contentBytes)
}

func richTextMarkdownMessageContent(markdown string) string {
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	paragraphs := make([][]map[string]any, 0, len(lines))

	inCodeBlock := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if line == "" {
			continue
		}

		if !inCodeBlock {
			line = normalizeMarkdownLine(line)
		}
		if line == "" {
			continue
		}

		paragraph := markdownLineToPostParagraph(line)
		if len(paragraph) == 0 {
			continue
		}
		paragraphs = append(paragraphs, paragraph)
	}

	if len(paragraphs) == 0 {
		paragraphs = append(paragraphs, []map[string]any{
			{
				"tag":  "text",
				"text": " ",
			},
		})
	}

	contentBytes, _ := json.Marshal(map[string]any{
		"zh_cn": map[string]any{
			"title":   "",
			"content": paragraphs,
		},
	})
	return string(contentBytes)
}

func normalizeMarkdownLine(line string) string {
	line = strings.TrimSpace(line)
	for strings.HasPrefix(line, "#") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
	}

	switch {
	case strings.HasPrefix(line, "- [ ] "), strings.HasPrefix(line, "* [ ] "):
		line = "☐ " + strings.TrimSpace(line[6:])
	case strings.HasPrefix(line, "- [x] "), strings.HasPrefix(line, "- [X] "), strings.HasPrefix(line, "* [x] "), strings.HasPrefix(line, "* [X] "):
		line = "☑ " + strings.TrimSpace(line[6:])
	case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "), strings.HasPrefix(line, "+ "):
		line = "• " + strings.TrimSpace(line[2:])
	}

	line = markdownStrongPattern.ReplaceAllString(line, "$1")
	line = markdownEmPattern.ReplaceAllString(line, "$1")
	line = markdownStrongUnderscorePattern.ReplaceAllString(line, "$1")
	line = markdownEmUnderscorePattern.ReplaceAllString(line, "$1")
	line = markdownStrikePattern.ReplaceAllString(line, "$1")
	return strings.TrimSpace(line)
}

func markdownLineToPostParagraph(line string) []map[string]any {
	matches := markdownLinkPattern.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return []map[string]any{
			{
				"tag":  "text",
				"text": line,
			},
		}
	}

	paragraph := make([]map[string]any, 0, len(matches)*2+1)
	cursor := 0
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		start := match[0]
		end := match[1]
		labelStart := match[2]
		labelEnd := match[3]
		hrefStart := match[4]
		hrefEnd := match[5]

		if start > cursor {
			text := line[cursor:start]
			if strings.TrimSpace(text) != "" {
				paragraph = append(paragraph, map[string]any{
					"tag":  "text",
					"text": text,
				})
			}
		}

		label := strings.TrimSpace(line[labelStart:labelEnd])
		href := strings.TrimSpace(line[hrefStart:hrefEnd])
		if href != "" {
			if label == "" {
				label = href
			}
			paragraph = append(paragraph, map[string]any{
				"tag":  "a",
				"text": label,
				"href": href,
			})
		}
		cursor = end
	}

	if cursor < len(line) {
		text := line[cursor:]
		if strings.TrimSpace(text) != "" {
			paragraph = append(paragraph, map[string]any{
				"tag":  "text",
				"text": text,
			})
		}
	}

	if len(paragraph) == 0 {
		return []map[string]any{
			{
				"tag":  "text",
				"text": line,
			},
		}
	}
	return paragraph
}
