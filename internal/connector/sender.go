package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/memory"
)

type LarkSender struct {
	client      *lark.Client
	resourceDir string
}

func NewLarkSender(client *lark.Client, resourceDir string) *LarkSender {
	return &LarkSender{
		client:      client,
		resourceDir: strings.TrimSpace(resourceDir),
	}
}

func (s *LarkSender) ResourceRootForScope(memoryScopeKey string) string {
	if s == nil {
		return ""
	}
	return memory.ResolveScopedResourceRoot(strings.TrimSpace(s.resourceDir), memoryScopeKey)
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

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		return nil
	})
}

func (s *LarkSender) AddReaction(ctx context.Context, messageID, emojiType string) error {
	messageID = strings.TrimSpace(messageID)
	emojiType = strings.ToUpper(strings.TrimSpace(emojiType))
	if messageID == "" {
		return errors.New("message id is empty")
	}
	if emojiType == "" {
		return errors.New("emoji type is empty")
	}

	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.MessageReaction.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		return nil
	})
}

func (s *LarkSender) SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("interactive").
			Content(cardContent).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		return nil
	})
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

func (s *LarkSender) ReplyTextDirect(ctx context.Context, sourceMessageID, text string) (string, error) {
	return s.replyMessage(
		ctx,
		sourceMessageID,
		"text",
		textMessageContent(text),
		false,
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

func (s *LarkSender) ReplyRichTextMarkdownDirect(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	return s.replyMessage(
		ctx,
		sourceMessageID,
		"post",
		richTextMarkdownMessageContent(markdown),
		false,
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

func (s *LarkSender) ReplyCardDirect(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	return s.replyMessage(
		ctx,
		sourceMessageID,
		"interactive",
		cardContent,
		false,
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

	var messageID string
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Reply(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		if resp.Data == nil || resp.Data.MessageId == nil {
			return errors.New(emptyMessageIDErr)
		}
		messageID = strings.TrimSpace(*resp.Data.MessageId)
		if messageID == "" {
			return errors.New(emptyMessageIDErr)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return messageID, nil
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

func (s *LarkSender) withFeishuRetry(ctx context.Context, run func() error) error {
	if run == nil {
		return errors.New("feishu operation is nil")
	}
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 200 * time.Millisecond
	bo.MaxInterval = 1 * time.Second
	bo.MaxElapsedTime = 3 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0.1
	boCtx := backoff.WithContext(bo, ctx)
	return backoff.Retry(func() error {
		err := run()
		if err == nil {
			return nil
		}
		if !isRetryableFeishuError(err) {
			return backoff.Permanent(err)
		}
		return err
	}, boCtx)
}

func isRetryableFeishuError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *feishuAPIError
	return !errors.As(err, &apiErr)
}

func (s *LarkSender) GetMessageText(ctx context.Context, messageID string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", errors.New("message id is empty")
	}

	req := larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		Build()
	var msgType string
	var content string
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Get(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		if resp.Data == nil || len(resp.Data.Items) == 0 || resp.Data.Items[0] == nil {
			return errors.New("get message success but items is empty")
		}
		item := resp.Data.Items[0]
		msgType = strings.ToLower(strings.TrimSpace(deref(item.MsgType)))
		content = ""
		if item.Body != nil {
			content = deref(item.Body.Content)
		}
		content = strings.TrimSpace(content)
		if content == "" {
			return errors.New("get message success but content is empty")
		}
		return nil
	})
	if err != nil {
		return "", err
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

func (s *LarkSender) DownloadAttachment(ctx context.Context, memoryScopeKey, sourceMessageID string, attachment *Attachment) error {
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
	resourceRoot := strings.TrimSpace(s.ResourceRootForScope(memoryScopeKey))
	if resourceRoot == "" {
		return errors.New("resource root is empty")
	}

	kind := strings.ToLower(strings.TrimSpace(attachment.Kind))
	switch kind {
	case "image":
		imageKey := strings.TrimSpace(attachment.ImageKey)
		fileKey := strings.TrimSpace(attachment.FileKey)
		if imageKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, imageKey, "image")
			if err == nil {
				return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, imageKey, fileName, fileReader, attachment)
			}
			if fallbackName, fallbackReader, fallbackErr := s.downloadImage(ctx, imageKey); fallbackErr == nil {
				return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, imageKey, fallbackName, fallbackReader, attachment)
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
			return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
		}
		return errors.New("image attachment missing image_key and file_key")
	case "sticker":
		fileKey := strings.TrimSpace(attachment.FileKey)
		imageKey := strings.TrimSpace(attachment.ImageKey)
		if fileKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
			if err == nil {
				return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
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
			return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, imageKey, fileName, fileReader, attachment)
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
		return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
	default:
		return fmt.Errorf("unsupported attachment kind: %s", kind)
	}
}

func (s *LarkSender) downloadImage(ctx context.Context, imageKey string) (string, io.Reader, error) {
	req := larkim.NewGetImageReqBuilder().
		ImageKey(imageKey).
		Build()
	var fileName string
	var fileReader io.Reader
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Image.Get(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		if resp.File == nil {
			return errors.New("download image success but file body is empty")
		}
		fileName = strings.TrimSpace(resp.FileName)
		fileReader = resp.File
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return fileName, fileReader, nil
}

func (s *LarkSender) downloadMessageResource(ctx context.Context, messageID, resourceKey, resourceType string) (string, io.Reader, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(resourceKey).
		Type(resourceType).
		Build()
	var fileName string
	var fileReader io.Reader
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.MessageResource.Get(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		if resp.File == nil {
			return errors.New("download message resource success but file body is empty")
		}
		fileName = strings.TrimSpace(resp.FileName)
		fileReader = resp.File
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return fileName, fileReader, nil
}

func (s *LarkSender) writeAttachmentFile(
	resourceRoot string,
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

	subDir := filepath.Join(
		strings.TrimSpace(resourceRoot),
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
