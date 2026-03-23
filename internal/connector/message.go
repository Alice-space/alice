package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func BuildJob(event *larkim.P2MessageReceiveV1) (*Job, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil, ErrIgnoreMessage
	}

	message := event.Event.Message
	messageType := strings.ToLower(strings.TrimSpace(deref(message.MessageType)))
	if !isSupportedIncomingMessageType(messageType) {
		return nil, ErrIgnoreMessage
	}
	sourceMessageID := strings.TrimSpace(deref(message.MessageId))

	text, attachments, err := extractIncomingMessageContent(messageType, message.Content, message.Mentions)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return nil, ErrIgnoreMessage
	}
	for i := range attachments {
		if strings.TrimSpace(attachments[i].SourceMessageID) == "" {
			attachments[i].SourceMessageID = sourceMessageID
		}
	}

	receiveID := strings.TrimSpace(deref(message.ChatId))
	receiveIDType := "chat_id"
	if receiveID == "" {
		receiveID = strings.TrimSpace(extractOpenID(event))
		receiveIDType = "open_id"
	}
	if receiveID == "" {
		return nil, errors.New("missing receive target")
	}

	return &Job{
		ReceiveID:            receiveID,
		ReceiveIDType:        receiveIDType,
		ChatType:             strings.TrimSpace(deref(message.ChatType)),
		SenderName:           "",
		SenderOpenID:         strings.TrimSpace(extractOpenID(event)),
		SenderUserID:         strings.TrimSpace(extractUserID(event)),
		SenderUnionID:        strings.TrimSpace(extractUnionID(event)),
		MentionedUsers:       extractMentionedUsers(message),
		SourceMessageID:      sourceMessageID,
		ReplyParentMessageID: extractReplyParentMessageID(message),
		ThreadID:             strings.TrimSpace(deref(message.ThreadId)),
		RootID:               strings.TrimSpace(deref(message.RootId)),
		MessageType:          messageType,
		Text:                 text,
		Attachments:          attachments,
		RawContent:           strings.TrimSpace(deref(message.Content)),
		EventID:              eventID(event),
		ReceivedAt:           time.Now(),
		ResourceScopeKey:     buildResourceScopeKey(receiveIDType, receiveID),
		SessionKey:           buildSessionKeyForMessage(receiveIDType, receiveID, message),
	}, nil
}

func isSupportedIncomingMessageType(messageType string) bool {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "text", "image", "sticker", "audio", "file", "post":
		return true
	default:
		return false
	}
}

func isMediaMessageType(messageType string) bool {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "image", "sticker", "audio", "file", "post":
		return true
	default:
		return false
	}
}

func extractIncomingMessageContent(messageType string, content *string, mentions []*larkim.MentionEvent) (string, []Attachment, error) {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "text":
		text, err := extractTextWithMentions(content, mentions)
		return text, nil, err
	case "image":
		text, err := extractOptionalTextWithMentions(content, mentions)
		if err != nil {
			return "", nil, err
		}
		attachment, err := extractImageAttachment(content)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(text) == "" {
			text = "用户发送了一张图片。"
		}
		return text, []Attachment{attachment}, nil
	case "sticker":
		text, err := extractOptionalTextWithMentions(content, mentions)
		if err != nil {
			return "", nil, err
		}
		attachment, err := extractStickerAttachment(content)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(text) == "" {
			text = "用户发送了一个表情包。"
		}
		return text, []Attachment{attachment}, nil
	case "audio":
		text, err := extractOptionalTextWithMentions(content, mentions)
		if err != nil {
			return "", nil, err
		}
		attachment, err := extractAudioAttachment(content)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(text) == "" {
			text = "用户发送了一段语音。"
		}
		return text, []Attachment{attachment}, nil
	case "file":
		defaultText, attachment, err := extractFileAttachment(content)
		if err != nil {
			return "", nil, err
		}
		text, err := extractOptionalTextWithMentions(content, mentions)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(text) == "" {
			text = defaultText
		}
		return text, []Attachment{attachment}, nil
	case "post":
		return extractPostContentWithMentions(content, mentions)
	default:
		return "", nil, ErrIgnoreMessage
	}
}

func extractImageAttachment(content *string) (Attachment, error) {
	var payload struct {
		ImageKey string `json:"image_key"`
		FileKey  string `json:"file_key"`
	}
	if err := decodeIncomingContent(content, &payload); err != nil {
		return Attachment{}, err
	}

	imageKey := strings.TrimSpace(payload.ImageKey)
	fileKey := strings.TrimSpace(payload.FileKey)
	if imageKey == "" && fileKey == "" {
		return Attachment{}, ErrIgnoreMessage
	}
	return Attachment{Kind: "image", ImageKey: imageKey, FileKey: fileKey}, nil
}

func extractStickerAttachment(content *string) (Attachment, error) {
	var payload struct {
		FileKey  string `json:"file_key"`
		ImageKey string `json:"image_key"`
	}
	if err := decodeIncomingContent(content, &payload); err != nil {
		return Attachment{}, err
	}

	fileKey := strings.TrimSpace(payload.FileKey)
	imageKey := strings.TrimSpace(payload.ImageKey)
	if fileKey == "" && imageKey == "" {
		return Attachment{}, ErrIgnoreMessage
	}
	return Attachment{Kind: "sticker", FileKey: fileKey, ImageKey: imageKey}, nil
}

func extractAudioAttachment(content *string) (Attachment, error) {
	var payload struct {
		FileKey string `json:"file_key"`
	}
	if err := decodeIncomingContent(content, &payload); err != nil {
		return Attachment{}, err
	}

	fileKey := strings.TrimSpace(payload.FileKey)
	if fileKey == "" {
		return Attachment{}, ErrIgnoreMessage
	}
	return Attachment{Kind: "audio", FileKey: fileKey}, nil
}

func extractFileAttachment(content *string) (string, Attachment, error) {
	var payload struct {
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
	}
	if err := decodeIncomingContent(content, &payload); err != nil {
		return "", Attachment{}, err
	}

	fileKey := strings.TrimSpace(payload.FileKey)
	fileName := strings.TrimSpace(payload.FileName)
	if fileKey == "" {
		return "", Attachment{}, ErrIgnoreMessage
	}

	text := "用户发送了一个文件。"
	if fileName != "" {
		text = "用户发送了一个文件：" + fileName
	}
	return text, Attachment{Kind: "file", FileKey: fileKey, FileName: fileName}, nil
}

func decodeIncomingContent(content *string, out any) error {
	trimmed := strings.TrimSpace(deref(content))
	if trimmed == "" {
		return ErrIgnoreMessage
	}
	if err := json.Unmarshal([]byte(trimmed), out); err != nil {
		return fmt.Errorf("invalid message content json: %w", err)
	}
	return nil
}
