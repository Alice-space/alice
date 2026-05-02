package connector

import (
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

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
	return isGroupMessageTriggered(event, triggerMode, triggerPrefix, botOpenID, botUserID)
}

func isGroupMessageTriggered(
	event *larkim.P2MessageReceiveV1,
	triggerMode string,
	triggerPrefix string,
	botOpenID string,
	botUserID string,
) bool {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return false
	}
	message := event.Event.Message
	if !isGroupChatType(deref(message.ChatType)) {
		return true
	}
	switch normalizedTriggerMode(triggerMode) {
	case config.TriggerModeAll:
		return true
	case config.TriggerModePrefix:
		return isGroupTriggerPrefixMatched(event, triggerPrefix)
	default:
		return isGroupMentionAccepted(message, botOpenID, botUserID)
	}
}

func normalizedTriggerMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case config.TriggerModePrefix, config.TriggerModeAll:
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
