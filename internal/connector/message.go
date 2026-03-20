package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
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
	return Attachment{
		Kind:     "image",
		ImageKey: imageKey,
		FileKey:  fileKey,
	}, nil
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
	return Attachment{
		Kind:     "sticker",
		FileKey:  fileKey,
		ImageKey: imageKey,
	}, nil
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
	return Attachment{
		Kind:    "audio",
		FileKey: fileKey,
	}, nil
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
	return text, Attachment{
		Kind:     "file",
		FileKey:  fileKey,
		FileName: fileName,
	}, nil
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

func shouldProcessIncomingMessage(
	event *larkim.P2MessageReceiveV1,
	triggerMode string,
	triggerPrefix string,
	botOpenID string,
	botUserID string,
) bool {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return true
	}
	message := event.Event.Message
	if !isGroupChatType(deref(message.ChatType)) {
		return true
	}
	if isBuiltinCommandEvent(event) {
		return true
	}
	mentionAccepted := isGroupMentionAccepted(message, botOpenID, botUserID)

	switch normalizedTriggerMode(triggerMode) {
	case config.TriggerModePrefix:
		return isGroupTriggerPrefixMatched(event, triggerPrefix)
	default:
		return mentionAccepted
	}
}

func normalizedTriggerMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case config.TriggerModePrefix:
		return normalized
	default:
		return config.TriggerModeAt
	}
}

func isGroupTriggerPrefixMatched(event *larkim.P2MessageReceiveV1, triggerPrefix string) bool {
	prefix := strings.TrimSpace(triggerPrefix)
	if prefix == "" {
		return false
	}
	job, err := BuildJob(event)
	if err != nil || job == nil {
		return false
	}
	text := strings.TrimSpace(job.Text)
	return strings.HasPrefix(text, prefix)
}

func normalizeIncomingGroupJobTextForTriggerMode(job *Job, triggerMode, triggerPrefix string) {
	if job == nil || !isGroupChatType(job.ChatType) {
		return
	}
	if isBuiltinCommandText(job.Text) {
		return
	}
	if normalizedTriggerMode(triggerMode) != config.TriggerModePrefix {
		return
	}
	job.Text = trimGroupTriggerPrefix(job.Text, triggerPrefix)
}

func isBuiltinCommandEvent(event *larkim.P2MessageReceiveV1) bool {
	job, err := BuildJob(event)
	if err != nil || job == nil {
		return false
	}
	return isBuiltinCommandText(job.Text)
}

func trimGroupTriggerPrefix(text, triggerPrefix string) string {
	trimmedText := strings.TrimSpace(text)
	prefix := strings.TrimSpace(triggerPrefix)
	if prefix == "" {
		return trimmedText
	}
	if !strings.HasPrefix(trimmedText, prefix) {
		return trimmedText
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmedText, prefix))
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

	if !hasConfiguredBotID {
		return false
	}
	return isBotMentioned(message, normalizedBotOpenID, normalizedBotUserID)
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
		"incoming message source=feishu_im event_id=%s message_id=%s parent_id=%s root_id=%s thread_id=%s message_type=%s chat_id=%s raw_content=%s",
		eventID(event),
		strings.TrimSpace(deref(message.MessageId)),
		strings.TrimSpace(deref(message.ParentId)),
		strings.TrimSpace(deref(message.RootId)),
		strings.TrimSpace(deref(message.ThreadId)),
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

func buildResourceScopeKey(receiveIDType, receiveID string) string {
	return buildSessionKey(receiveIDType, receiveID)
}

func resourceScopeKeyForJob(job Job) string {
	scopeKey := strings.TrimSpace(job.ResourceScopeKey)
	if scopeKey != "" {
		return scopeKey
	}
	scopeKey = buildResourceScopeKey(job.ReceiveIDType, job.ReceiveID)
	if scopeKey != "" {
		return scopeKey
	}
	return resourceScopeKeyFromSessionKey(job.SessionKey)
}

func resourceScopeKeyFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, "|"); idx >= 0 {
		sessionKey = strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}

func buildSessionKeyForMessage(receiveIDType, receiveID string, message *larkim.EventMessage) string {
	candidates := buildSessionKeyCandidatesForMessage(receiveIDType, receiveID, message)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func buildSessionKeyCandidatesForMessage(receiveIDType, receiveID string, message *larkim.EventMessage) []string {
	base := buildSessionKey(receiveIDType, receiveID)
	if base == "" {
		return nil
	}

	candidates := make([]string, 0, 4)
	isGroupMessage := message != nil && isGroupChatType(deref(message.ChatType))
	if !isGroupMessage {
		appendSessionKeyCandidate(&candidates, base)
	}
	if message != nil {
		threadID := strings.TrimSpace(deref(message.ThreadId))
		rootID := strings.TrimSpace(deref(message.RootId))
		parentID := strings.TrimSpace(deref(message.ParentId))
		sourceMessageID := strings.TrimSpace(deref(message.MessageId))

		if threadID != "" {
			appendSessionKeyCandidate(&candidates, base+"|thread:"+threadID)
		} else if rootID != "" {
			appendSessionKeyCandidate(&candidates, base+"|thread:"+rootID)
		}
		if parentID != "" {
			appendSessionKeyCandidate(&candidates, base+"|message:"+parentID)
		}
		if threadID != "" || rootID != "" {
			if rootID != "" {
				appendSessionKeyCandidate(&candidates, base+"|message:"+rootID)
			}
		}
		if isGroupMessage && sourceMessageID != "" {
			appendSessionKeyCandidate(&candidates, base+"|message:"+sourceMessageID)
		}
	}

	if len(candidates) == 0 {
		appendSessionKeyCandidate(&candidates, base)
	}
	return candidates
}

func appendSessionKeyCandidate(candidates *[]string, candidate string) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return
	}
	for _, existing := range *candidates {
		if existing == candidate {
			return
		}
	}
	*candidates = append(*candidates, candidate)
}

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

	text := mentionPattern.ReplaceAllString(payload.Text, "")
	text = stripMentionKeys(text, mentions)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrIgnoreMessage
	}
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

func extractOpenID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	return deref(event.Event.Sender.SenderId.OpenId)
}

func extractUserID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	return deref(event.Event.Sender.SenderId.UserId)
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
