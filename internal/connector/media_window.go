package connector

import (
	"context"
	"fmt"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

type mediaWindowEntry struct {
	SourceMessageID string       `json:"source_message_id"`
	MessageType     string       `json:"message_type"`
	Speaker         string       `json:"speaker,omitempty"`
	Text            string       `json:"text"`
	Attachments     []Attachment `json:"attachments"`
	RawContent      string       `json:"raw_content,omitempty"`
	ReceivedAt      time.Time    `json:"received_at"`
}

func (a *App) groupContextWindowTTL() time.Duration {
	if a.cfg.GroupContextWindowTTL > 0 {
		return a.cfg.GroupContextWindowTTL
	}
	return defaultGroupContextWindow
}

func (a *App) cacheGroupContextWindow(ctx context.Context, event *larkim.P2MessageReceiveV1, accepted bool) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	triggerMode := normalizedTriggerMode(a.cfg.TriggerMode)
	if !shouldApplyGroupContextWindow(triggerMode) {
		return
	}
	message := event.Event.Message
	if !isGroupChatType(deref(message.ChatType)) {
		return
	}
	if accepted {
		return
	}
	if triggerMode == config.TriggerModeAt &&
		strings.TrimSpace(a.cfg.FeishuBotOpenID) == "" &&
		strings.TrimSpace(a.cfg.FeishuBotUserID) == "" {
		return
	}
	if !isSupportedIncomingMessageType(deref(message.MessageType)) {
		return
	}

	job, err := BuildJob(event)
	if err != nil || job == nil || !hasMediaWindowEntryContent(mediaWindowEntry{
		Text:        job.Text,
		Attachments: job.Attachments,
	}) {
		return
	}

	windowKey := buildMediaWindowKeyForJob(*job)
	if windowKey == "" {
		return
	}

	at := a.now()
	if at.IsZero() {
		at = job.ReceivedAt
	}
	if at.IsZero() {
		at = time.Now()
	}
	speakerName := strings.TrimSpace(job.SenderName)
	if speakerName == "" {
		speakerName = a.resolveMediaWindowSpeakerName(ctx, *job)
	}
	entry := mediaWindowEntry{
		SourceMessageID: strings.TrimSpace(job.SourceMessageID),
		MessageType:     strings.TrimSpace(job.MessageType),
		Speaker:         mediaWindowSpeakerLabel(speakerName, job.SenderOpenID, job.SenderUserID, job.SenderUnionID),
		Text:            strings.TrimSpace(job.Text),
		Attachments:     cloneAttachments(job.Attachments),
		RawContent:      strings.TrimSpace(job.RawContent),
		ReceivedAt:      at,
	}

	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	a.pruneExpiredMediaWindowLocked(at)
	entries := append(a.state.mediaWindow[windowKey], entry)
	if len(entries) > maxMediaWindowEntries {
		entries = append([]mediaWindowEntry(nil), entries[len(entries)-maxMediaWindowEntries:]...)
	}
	a.state.mediaWindow[windowKey] = entries
	a.markRuntimeStateChangedLocked()
	logging.Debugf(
		"group context cached event_id=%s window_key=%s message_type=%s text=%t attachments=%d",
		job.EventID,
		windowKey,
		job.MessageType,
		strings.TrimSpace(job.Text) != "",
		len(job.Attachments),
	)
}

func (a *App) mergeRecentGroupContextWindow(job *Job) {
	if job == nil {
		return
	}
	if !shouldApplyGroupContextWindow(normalizedTriggerMode(a.cfg.TriggerMode)) {
		return
	}
	if !isGroupChatType(job.ChatType) {
		return
	}
	windowKey := buildMediaWindowKeyForJob(*job)
	if windowKey == "" {
		return
	}

	now := a.now()
	if now.IsZero() {
		now = job.ReceivedAt
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-a.groupContextWindowTTL())
	sourceMessageID := strings.TrimSpace(job.SourceMessageID)

	a.state.mu.Lock()
	a.pruneExpiredMediaWindowLocked(now)
	entries := a.state.mediaWindow[windowKey]
	if len(entries) == 0 {
		a.state.mu.Unlock()
		return
	}

	selected := make([]mediaWindowEntry, 0, len(entries))
	remaining := make([]mediaWindowEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ReceivedAt.IsZero() {
			entry.ReceivedAt = now
		}
		if entry.ReceivedAt.Before(cutoff) {
			continue
		}
		// Avoid reusing the same message as both trigger and window content.
		if sourceMessageID != "" && strings.TrimSpace(entry.SourceMessageID) == sourceMessageID {
			continue
		}
		// Keep future-dated entries (clock skew) for next trigger.
		if entry.ReceivedAt.After(now) {
			remaining = append(remaining, entry)
			continue
		}
		selected = append(selected, entry)
	}

	if len(remaining) == 0 {
		delete(a.state.mediaWindow, windowKey)
	} else {
		a.state.mediaWindow[windowKey] = remaining
	}
	if len(selected) > 0 {
		a.markRuntimeStateChangedLocked()
	}
	a.state.mu.Unlock()

	if len(selected) == 0 {
		return
	}

	mergedTexts := make([]string, 0, len(selected))
	mediaMessageCount := 0
	textMessageCount := 0
	mergedAttachments := 0
	fallbackSpeaker := mediaWindowSpeakerLabel(job.SenderName, job.SenderOpenID, job.SenderUserID, job.SenderUnionID)
	for _, entry := range selected {
		if strings.EqualFold(strings.TrimSpace(entry.MessageType), "text") {
			textMessageCount++
		}
		if isMediaMessageType(entry.MessageType) {
			mediaMessageCount++
		}
		if text := strings.TrimSpace(entry.Text); text != "" {
			speaker := strings.TrimSpace(entry.Speaker)
			if speaker == "" || (isGenericMediaWindowSpeaker(speaker) && !isGenericMediaWindowSpeaker(fallbackSpeaker)) {
				speaker = fallbackSpeaker
			}
			mergedTexts = append(
				mergedTexts,
				fmt.Sprintf("说话者：%s；内容：%s", speaker, clipText(text, 200)),
			)
		}
		mergedAttachments += len(entry.Attachments)
		job.Attachments = append(job.Attachments, cloneAttachments(entry.Attachments)...)
	}

	hint := buildGroupContextMergeHint(
		formatGroupContextWindowLabel(a.groupContextWindowTTL()),
		textMessageCount,
		mediaMessageCount,
		mergedAttachments,
		mergedTexts,
	)
	baseText := strings.TrimSpace(job.Text)
	if baseText == "" {
		job.Text = hint
	} else {
		job.Text = baseText + "\n\n" + hint
	}

	logging.Debugf(
		"group context merged event_id=%s window_key=%s merged_messages=%d merged_attachments=%d",
		job.EventID,
		windowKey,
		len(selected),
		mergedAttachments,
	)
}

func shouldApplyGroupContextWindow(triggerMode string) bool {
	switch normalizedTriggerMode(triggerMode) {
	case config.TriggerModeAt, config.TriggerModePrefix:
		return true
	default:
		return false
	}
}

func (a *App) buildSyntheticMentionJob(event *larkim.P2MessageReceiveV1) (*Job, bool) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil, false
	}
	message := event.Event.Message
	if !isGroupChatType(deref(message.ChatType)) {
		return nil, false
	}
	if strings.ToLower(strings.TrimSpace(deref(message.MessageType))) != "text" {
		return nil, false
	}

	receiveID := strings.TrimSpace(deref(message.ChatId))
	receiveIDType := "chat_id"
	if receiveID == "" {
		receiveID = strings.TrimSpace(extractOpenID(event))
		receiveIDType = "open_id"
	}
	if receiveID == "" {
		return nil, false
	}

	promptText, err := a.renderPromptFile(connectorPromptSyntheticMention, nil)
	if err != nil {
		logging.Warnf("render synthetic mention prompt failed event_id=%s: %v", eventID(event), err)
		return nil, false
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
		SourceMessageID:      strings.TrimSpace(deref(message.MessageId)),
		ReplyParentMessageID: extractReplyParentMessageID(message),
		ThreadID:             strings.TrimSpace(deref(message.ThreadId)),
		RootID:               strings.TrimSpace(deref(message.RootId)),
		MessageType:          "text",
		Text:                 promptText,
		RawContent:           strings.TrimSpace(deref(message.Content)),
		EventID:              eventID(event),
		ReceivedAt:           a.now(),
		MemoryScopeKey:       buildMemoryScopeKey(receiveIDType, receiveID),
		SessionKey:           buildSessionKeyForMessage(receiveIDType, receiveID, message),
	}, true
}

func (a *App) pruneExpiredMediaWindowLocked(now time.Time) {
	if len(a.state.mediaWindow) == 0 {
		return
	}
	if now.IsZero() {
		now = a.now()
	}
	cutoff := now.Add(-a.groupContextWindowTTL())
	changed := false

	for key, entries := range a.state.mediaWindow {
		filtered := make([]mediaWindowEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.ReceivedAt.IsZero() {
				continue
			}
			if entry.ReceivedAt.Before(cutoff) {
				continue
			}
			if !hasMediaWindowEntryContent(entry) {
				continue
			}
			filtered = append(filtered, entry)
		}
		if len(filtered) == 0 {
			delete(a.state.mediaWindow, key)
			changed = true
			continue
		}
		if len(filtered) != len(entries) {
			a.state.mediaWindow[key] = filtered
			changed = true
		}
	}

	if changed {
		a.markRuntimeStateChangedLocked()
	}
}

func senderIdentityForJob(job Job) string {
	openID := strings.TrimSpace(job.SenderOpenID)
	if openID != "" {
		return "open_id:" + openID
	}
	userID := strings.TrimSpace(job.SenderUserID)
	if userID != "" {
		return "user_id:" + userID
	}
	return ""
}

func buildMediaWindowKey(receiveID, senderIdentity string) string {
	chatID := strings.TrimSpace(receiveID)
	senderIdentity = strings.TrimSpace(senderIdentity)
	if chatID == "" || senderIdentity == "" {
		return ""
	}
	return chatID + "|" + senderIdentity
}

func mediaWindowSpeakerLabel(name, openID, userID, unionID string) string {
	normalizedName := strings.TrimSpace(name)
	id := preferredID(openID, userID, unionID)
	switch {
	case normalizedName != "" && id != "":
		return normalizedName + "(" + id + ")"
	case normalizedName != "":
		return normalizedName
	case id != "":
		return "该用户(" + id + ")"
	default:
		return "该用户"
	}
}

func isGenericMediaWindowSpeaker(speaker string) bool {
	speaker = strings.TrimSpace(speaker)
	return speaker == "该用户" || strings.HasPrefix(speaker, "该用户(")
}

func (a *App) resolveMediaWindowSpeakerName(ctx context.Context, job Job) string {
	if a == nil || a.processor == nil || a.processor.sender == nil {
		return ""
	}
	openID := strings.TrimSpace(job.SenderOpenID)
	userID := strings.TrimSpace(job.SenderUserID)
	if openID == "" && userID == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	chatID := strings.TrimSpace(job.ReceiveID)
	if strings.EqualFold(strings.TrimSpace(job.ReceiveIDType), "chat_id") && chatID != "" {
		if resolver, ok := a.processor.sender.(ChatMemberNameResolver); ok {
			name, err := resolver.ResolveChatMemberName(ctx, chatID, openID, userID)
			name = strings.TrimSpace(name)
			if name != "" {
				return name
			}
			if err != nil {
				logging.Debugf(
					"group context speaker resolve via chat_member failed event_id=%s chat_id=%s open_id=%s user_id=%s err=%v",
					job.EventID,
					chatID,
					openID,
					userID,
					err,
				)
			}
		}
	}

	if resolver, ok := a.processor.sender.(UserNameResolver); ok {
		name, err := resolver.ResolveUserName(ctx, openID, userID)
		name = strings.TrimSpace(name)
		if name != "" {
			return name
		}
		if err != nil {
			logging.Debugf(
				"group context speaker resolve via user failed event_id=%s open_id=%s user_id=%s err=%v",
				job.EventID,
				openID,
				userID,
				err,
			)
		}
	}
	return ""
}

func buildMediaWindowKeyForJob(job Job) string {
	base := buildMediaWindowKey(job.ReceiveID, senderIdentityForJob(job))
	if base == "" {
		return ""
	}
	scope := mediaWindowThreadScopeForJob(job)
	if scope == "" {
		return base
	}
	return base + "|" + scope
}

func mediaWindowThreadScopeForJob(job Job) string {
	if threadID := strings.TrimSpace(job.ThreadID); threadID != "" {
		return "thread:" + threadID
	}
	if rootID := strings.TrimSpace(job.RootID); rootID != "" {
		return "root:" + rootID
	}
	return ""
}

func hasMediaWindowEntryContent(entry mediaWindowEntry) bool {
	return strings.TrimSpace(entry.Text) != "" || len(entry.Attachments) > 0
}

func formatGroupContextWindowLabel(ttl time.Duration) string {
	if ttl <= 0 {
		ttl = defaultGroupContextWindow
	}
	if ttl%time.Minute == 0 {
		return fmt.Sprintf("%d分钟", int(ttl/time.Minute))
	}
	if ttl%time.Second == 0 {
		return fmt.Sprintf("%d秒", int(ttl/time.Second))
	}
	return ttl.String()
}

func buildGroupContextMergeHint(
	windowLabel string,
	textMessageCount int,
	mediaMessageCount int,
	mergedAttachments int,
	mergedTexts []string,
) string {
	var summary string
	switch {
	case textMessageCount > 0 && mediaMessageCount > 0:
		summary = fmt.Sprintf(
			"系统补充：已自动合并你在过去%s发送的%d条文本消息和%d条多媒体消息（共%d个附件）。",
			windowLabel,
			textMessageCount,
			mediaMessageCount,
			mergedAttachments,
		)
	case mediaMessageCount > 0:
		summary = fmt.Sprintf(
			"系统补充：已自动合并你在过去%s发送的%d条多媒体消息（共%d个附件）。",
			windowLabel,
			mediaMessageCount,
			mergedAttachments,
		)
	default:
		summary = fmt.Sprintf(
			"系统补充：已自动合并你在过去%s发送的%d条文本消息。",
			windowLabel,
			textMessageCount,
		)
	}

	if len(mergedTexts) == 0 {
		return summary
	}

	maxItems := 8
	if len(mergedTexts) < maxItems {
		maxItems = len(mergedTexts)
	}
	lines := make([]string, 0, maxItems+2)
	lines = append(lines, summary, "最近消息内容：")
	for i := 0; i < maxItems; i++ {
		lines = append(lines, "- "+mergedTexts[i])
	}
	if len(mergedTexts) > maxItems {
		lines = append(lines, fmt.Sprintf("- 其余%d条消息已省略。", len(mergedTexts)-maxItems))
	}
	return strings.Join(lines, "\n")
}

func cloneAttachments(in []Attachment) []Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]Attachment, len(in))
	copy(out, in)
	return out
}
