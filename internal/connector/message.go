package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"gitee.com/alicespace/alice/internal/logging"
)

func BuildJob(event *larkim.P2MessageReceiveV1) (*Job, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil, ErrIgnoreMessage
	}

	message := event.Event.Message
	if strings.ToLower(deref(message.MessageType)) != "text" {
		return nil, ErrIgnoreMessage
	}

	text, err := extractText(message.Content)
	if err != nil {
		return nil, err
	}
	if text == "" {
		return nil, ErrIgnoreMessage
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
		SourceMessageID:      strings.TrimSpace(deref(message.MessageId)),
		ReplyParentMessageID: extractReplyParentMessageID(message),
		Text:                 text,
		EventID:              eventID(event),
		ReceivedAt:           time.Now(),
		SessionKey:           buildSessionKey(receiveIDType, receiveID),
	}, nil
}

func shouldProcessIncomingMessage(event *larkim.P2MessageReceiveV1, botOpenID, botUserID string) bool {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return true
	}
	message := event.Event.Message
	if !isGroupChatType(deref(message.ChatType)) {
		return true
	}
	return isGroupMentionAccepted(message, botOpenID, botUserID)
}

func isGroupChatType(chatType string) bool {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case "group", "topic_group":
		return true
	default:
		return false
	}
}

func isGroupMentionAccepted(message *larkim.EventMessage, botOpenID, botUserID string) bool {
	if message == nil {
		return false
	}

	normalizedBotOpenID := strings.TrimSpace(botOpenID)
	normalizedBotUserID := strings.TrimSpace(botUserID)
	hasConfiguredBotID := normalizedBotOpenID != "" || normalizedBotUserID != ""

	if hasConfiguredBotID {
		return isBotMentioned(message, normalizedBotOpenID, normalizedBotUserID)
	}
	return hasAnyUserMention(message)
}

func isBotMentioned(message *larkim.EventMessage, botOpenID, botUserID string) bool {
	if message == nil {
		return false
	}

	for _, mention := range message.Mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		openID := strings.TrimSpace(deref(mention.Id.OpenId))
		userID := strings.TrimSpace(deref(mention.Id.UserId))
		if botOpenID != "" && openID == botOpenID {
			return true
		}
		if botUserID != "" && userID == botUserID {
			return true
		}
	}

	for _, mentionedUserID := range extractMentionUserIDs(message.Content) {
		if botOpenID != "" && mentionedUserID == botOpenID {
			return true
		}
		if botUserID != "" && mentionedUserID == botUserID {
			return true
		}
	}
	return false
}

func hasAnyUserMention(message *larkim.EventMessage) bool {
	if message == nil {
		return false
	}

	for _, mentionedUserID := range extractMentionUserIDs(message.Content) {
		if !isMentionAll(mentionedUserID) {
			return true
		}
	}
	for _, mention := range message.Mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		if mentionID := strings.TrimSpace(deref(mention.Id.OpenId)); mentionID != "" && !isMentionAll(mentionID) {
			return true
		}
		if mentionID := strings.TrimSpace(deref(mention.Id.UserId)); mentionID != "" && !isMentionAll(mentionID) {
			return true
		}
	}
	return false
}

func extractMentionUserIDs(content *string) []string {
	rawContent := strings.TrimSpace(deref(content))
	if rawContent == "" {
		return nil
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(rawContent), &payload); err != nil {
		return nil
	}
	if strings.TrimSpace(payload.Text) == "" {
		return nil
	}

	matches := mentionUserIDPattern.FindAllStringSubmatch(payload.Text, -1)
	if len(matches) == 0 {
		return nil
	}

	userIDs := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		userID := strings.TrimSpace(match[1])
		if userID == "" {
			continue
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs
}

func isMentionAll(userID string) bool {
	return strings.EqualFold(strings.TrimSpace(userID), "all")
}

func logIncomingEventDebug(event *larkim.P2MessageReceiveV1) {
	if !logging.IsDebugEnabled() {
		return
	}
	if event == nil || event.Event == nil || event.Event.Message == nil {
		logging.Debugf("incoming message source=feishu_im event=<nil>")
		return
	}

	message := event.Event.Message
	logging.Debugf(
		"incoming message source=feishu_im event_id=%s message_id=%s message_type=%s chat_id=%s raw_content=%s",
		eventID(event),
		strings.TrimSpace(deref(message.MessageId)),
		strings.TrimSpace(deref(message.MessageType)),
		strings.TrimSpace(deref(message.ChatId)),
		deref(message.Content),
	)
}

func buildSessionKey(receiveIDType, receiveID string) string {
	idType := strings.TrimSpace(receiveIDType)
	if idType == "" {
		idType = "unknown"
	}

	id := strings.TrimSpace(receiveID)
	if id == "" {
		return ""
	}
	return idType + ":" + id
}

func extractText(content *string) (string, error) {
	if strings.TrimSpace(deref(content)) == "" {
		return "", ErrIgnoreMessage
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(deref(content)), &payload); err != nil {
		return "", fmt.Errorf("invalid text content json: %w", err)
	}

	text := mentionPattern.ReplaceAllString(payload.Text, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrIgnoreMessage
	}
	return text, nil
}

func extractOpenID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	return deref(event.Event.Sender.SenderId.OpenId)
}

func extractReplyParentMessageID(message *larkim.EventMessage) string {
	if message == nil {
		return ""
	}
	parentID := strings.TrimSpace(deref(message.ParentId))
	if parentID != "" {
		return parentID
	}
	return strings.TrimSpace(deref(message.RootId))
}

func eventID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil {
		return ""
	}
	return event.EventV2Base.Header.EventID
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
