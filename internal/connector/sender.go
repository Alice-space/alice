package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type LarkSender struct {
	client      *lark.Client
	resourceDir string
}

var markdownLinkPattern = regexp.MustCompile(`\[(.*?)\]\((https?://[^\s)]+)\)`)
var markdownInlineCodePattern = regexp.MustCompile("`([^`]+)`")
var markdownStrongPattern = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var markdownEmPattern = regexp.MustCompile(`\*([^*]+)\*`)
var markdownStrongUnderscorePattern = regexp.MustCompile(`__([^_]+)__`)
var markdownEmUnderscorePattern = regexp.MustCompile(`_([^_]+)_`)
var markdownStrikePattern = regexp.MustCompile(`~~([^~]+)~~`)

func NewLarkSender(client *lark.Client, resourceDir string) *LarkSender {
	return &LarkSender{
		client:      client,
		resourceDir: strings.TrimSpace(resourceDir),
	}
}

func (s *LarkSender) SendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	content := textMessageContent(text)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("text").
			Content(content).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	return nil
}

func (s *LarkSender) ReplyText(ctx context.Context, sourceMessageID, text string) (string, error) {
	return s.replyMessagePreferThread(
		ctx,
		sourceMessageID,
		"text",
		textMessageContent(text),
		"reply success but response message_id is empty",
	)
}

func (s *LarkSender) ReplyRichText(ctx context.Context, sourceMessageID string, lines []string) (string, error) {
	return s.replyMessagePreferThread(
		ctx,
		sourceMessageID,
		"post",
		richTextMessageContent(lines),
		"reply rich text success but response message_id is empty",
	)
}

func (s *LarkSender) ReplyRichTextMarkdown(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	return s.replyMessagePreferThread(
		ctx,
		sourceMessageID,
		"post",
		richTextMarkdownMessageContent(markdown),
		"reply markdown rich text success but response message_id is empty",
	)
}

func (s *LarkSender) ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	return s.replyMessagePreferThread(
		ctx,
		sourceMessageID,
		"interactive",
		cardContent,
		"reply card success but response message_id is empty",
	)
}

func (s *LarkSender) replyMessagePreferThread(
	ctx context.Context,
	sourceMessageID, msgType, content, emptyMessageIDErr string,
) (string, error) {
	messageID, err := s.replyMessage(ctx, sourceMessageID, msgType, content, true, emptyMessageIDErr)
	if err == nil {
		return messageID, nil
	}

	var apiErr *feishuAPIError
	if !errors.As(err, &apiErr) {
		return "", err
	}
	return s.replyMessage(ctx, sourceMessageID, msgType, content, false, emptyMessageIDErr)
}

func (s *LarkSender) replyMessage(
	ctx context.Context,
	sourceMessageID, msgType, content string,
	replyInThread bool,
	emptyMessageIDErr string,
) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			ReplyInThread(replyInThread).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", &feishuAPIError{
			Code:      resp.Code,
			Msg:       resp.Msg,
			RequestID: resp.RequestId(),
		}
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", errors.New(emptyMessageIDErr)
	}
	return strings.TrimSpace(*resp.Data.MessageId), nil
}

type feishuAPIError struct {
	Code      int
	Msg       string
	RequestID string
}

func (e *feishuAPIError) Error() string {
	if e == nil {
		return "feishu api error"
	}
	return fmt.Sprintf("feishu api error code=%d msg=%s request_id=%s", e.Code, e.Msg, e.RequestID)
}

func (s *LarkSender) PatchCard(ctx context.Context, messageID, cardContent string) error {
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardContent).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Patch(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	return nil
}

func (s *LarkSender) GetMessageText(ctx context.Context, messageID string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", errors.New("message id is empty")
	}

	req := larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		Build()
	resp, err := s.client.Im.V1.Message.Get(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 || resp.Data.Items[0] == nil {
		return "", errors.New("get message success but items is empty")
	}

	item := resp.Data.Items[0]
	msgType := strings.ToLower(strings.TrimSpace(deref(item.MsgType)))
	content := ""
	if item.Body != nil {
		content = deref(item.Body.Content)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("get message success but content is empty")
	}

	switch msgType {
	case "text":
		text, err := extractText(&content)
		if err == nil {
			return text, nil
		}
	case "interactive":
		if text := extractReplyTextFromCard(content); text != "" {
			return text, nil
		}
	case "post":
		if text := extractTextFromPost(content); text != "" {
			return text, nil
		}
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err == nil {
		text := strings.TrimSpace(payload.Text)
		if text != "" {
			text = mentionPattern.ReplaceAllString(text, "")
			text = strings.TrimSpace(text)
			if text != "" {
				return text, nil
			}
		}
	}
	return clipText(content, 1200), nil
}

func (s *LarkSender) DownloadAttachment(ctx context.Context, sourceMessageID string, attachment *Attachment) error {
	if attachment == nil {
		return errors.New("attachment is nil")
	}
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return errors.New("source message id is empty")
	}
	if strings.TrimSpace(s.resourceDir) == "" {
		return errors.New("resource dir is empty")
	}

	kind := strings.ToLower(strings.TrimSpace(attachment.Kind))
	switch kind {
	case "image":
		imageKey := strings.TrimSpace(attachment.ImageKey)
		fileKey := strings.TrimSpace(attachment.FileKey)
		if imageKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, imageKey, "image")
			if err == nil {
				return s.writeAttachmentFile(sourceMessageID, kind, imageKey, fileName, fileReader, attachment)
			}
			if fallbackName, fallbackReader, fallbackErr := s.downloadImage(ctx, imageKey); fallbackErr == nil {
				return s.writeAttachmentFile(sourceMessageID, kind, imageKey, fallbackName, fallbackReader, attachment)
			}
			if fileKey == "" {
				return err
			}
		}
		if fileKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
			if err != nil {
				return err
			}
			return s.writeAttachmentFile(sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
		}
		return errors.New("image attachment missing image_key and file_key")
	case "sticker":
		fileKey := strings.TrimSpace(attachment.FileKey)
		imageKey := strings.TrimSpace(attachment.ImageKey)
		if fileKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
			if err == nil {
				return s.writeAttachmentFile(sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
			}
			if imageKey == "" {
				return err
			}
		}
		if imageKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, imageKey, "image")
			if err != nil {
				return err
			}
			return s.writeAttachmentFile(sourceMessageID, kind, imageKey, fileName, fileReader, attachment)
		}
		return errors.New("sticker attachment missing file_key and image_key")
	case "audio", "file":
		fileKey := strings.TrimSpace(attachment.FileKey)
		if fileKey == "" {
			return fmt.Errorf("%s attachment missing file_key", kind)
		}
		fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
		if err != nil {
			return err
		}
		return s.writeAttachmentFile(sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
	default:
		return fmt.Errorf("unsupported attachment kind: %s", kind)
	}
}

func (s *LarkSender) downloadImage(ctx context.Context, imageKey string) (string, io.Reader, error) {
	req := larkim.NewGetImageReqBuilder().
		ImageKey(imageKey).
		Build()
	resp, err := s.client.Im.V1.Image.Get(ctx, req)
	if err != nil {
		return "", nil, err
	}
	if !resp.Success() {
		return "", nil, fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	if resp.File == nil {
		return "", nil, errors.New("download image success but file body is empty")
	}
	return strings.TrimSpace(resp.FileName), resp.File, nil
}

func (s *LarkSender) downloadMessageResource(ctx context.Context, messageID, resourceKey, resourceType string) (string, io.Reader, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(resourceKey).
		Type(resourceType).
		Build()
	resp, err := s.client.Im.V1.MessageResource.Get(ctx, req)
	if err != nil {
		return "", nil, err
	}
	if !resp.Success() {
		return "", nil, fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	if resp.File == nil {
		return "", nil, errors.New("download message resource success but file body is empty")
	}
	return strings.TrimSpace(resp.FileName), resp.File, nil
}

func (s *LarkSender) writeAttachmentFile(
	sourceMessageID, kind, key, suggestedFileName string,
	reader io.Reader,
	attachment *Attachment,
) error {
	if reader == nil {
		return errors.New("attachment file reader is nil")
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return errors.New("attachment file is empty")
	}

	resourceDir := strings.TrimSpace(s.resourceDir)
	subDir := filepath.Join(
		resourceDir,
		time.Now().Format("2006-01-02"),
		sanitizePathToken(sourceMessageID),
	)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return err
	}

	baseName := sanitizePathToken(strings.TrimSpace(suggestedFileName))
	if baseName == "" {
		baseName = sanitizePathToken(kind + "_" + key)
	}
	baseName = ensureAttachmentExtension(baseName, kind)

	targetPath := filepath.Join(subDir, baseName)
	if _, statErr := os.Stat(targetPath); statErr == nil {
		targetPath = filepath.Join(
			subDir,
			ensureAttachmentExtension(
				sanitizePathToken(kind+"_"+key+"_"+time.Now().Format("150405")),
				kind,
			),
		)
	}
	if err := os.WriteFile(targetPath, raw, 0o600); err != nil {
		return err
	}

	attachment.LocalPath = targetPath
	if strings.TrimSpace(attachment.FileName) == "" {
		attachment.FileName = filepath.Base(targetPath)
	}
	return nil
}

func sanitizePathToken(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		" ", "_",
		"\n", "_",
		"\r", "_",
		"\t", "_",
		":", "_",
	)
	value = replacer.Replace(value)
	value = strings.Trim(value, "._")
	if value == "" {
		return "unknown"
	}
	return value
}

func ensureAttachmentExtension(fileName, kind string) string {
	if filepath.Ext(fileName) != "" {
		return fileName
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image", "sticker":
		return fileName + ".img"
	case "audio":
		return fileName + ".audio"
	default:
		return fileName + ".bin"
	}
}

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

	line = markdownInlineCodePattern.ReplaceAllString(line, "$1")
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
			text := strings.TrimSpace(line[cursor:start])
			if text != "" {
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
		text := strings.TrimSpace(line[cursor:])
		if text != "" {
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
