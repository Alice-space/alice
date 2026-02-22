package connector

import (
	"fmt"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"gitee.com/alicespace/alice/internal/logging"
)

type mediaWindowEntry struct {
	SourceMessageID string       `json:"source_message_id"`
	MessageType     string       `json:"message_type"`
	Text            string       `json:"text"`
	Attachments     []Attachment `json:"attachments"`
	RawContent      string       `json:"raw_content,omitempty"`
	ReceivedAt      time.Time    `json:"received_at"`
}

func (a *App) cacheGroupMediaWindow(event *larkim.P2MessageReceiveV1) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	message := event.Event.Message
	if !isGroupChatType(deref(message.ChatType)) {
		return
	}
	if strings.TrimSpace(a.cfg.FeishuBotOpenID) == "" && strings.TrimSpace(a.cfg.FeishuBotUserID) == "" {
		return
	}
	if !isMediaMessageType(deref(message.MessageType)) {
		return
	}

	job, err := BuildJob(event)
	if err != nil || job == nil || len(job.Attachments) == 0 {
		return
	}

	senderIdentity := senderIdentityForJob(*job)
	if senderIdentity == "" {
		return
	}
	windowKey := buildMediaWindowKey(job.ReceiveID, senderIdentity)
	if windowKey == "" {
		return
	}

	at := job.ReceivedAt
	if at.IsZero() {
		at = a.now()
	}
	entry := mediaWindowEntry{
		SourceMessageID: strings.TrimSpace(job.SourceMessageID),
		MessageType:     strings.TrimSpace(job.MessageType),
		Text:            strings.TrimSpace(job.Text),
		Attachments:     cloneAttachments(job.Attachments),
		RawContent:      strings.TrimSpace(job.RawContent),
		ReceivedAt:      at,
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneExpiredMediaWindowLocked(at)
	entries := append(a.mediaWindow[windowKey], entry)
	if len(entries) > maxMediaWindowEntries {
		entries = append([]mediaWindowEntry(nil), entries[len(entries)-maxMediaWindowEntries:]...)
	}
	a.mediaWindow[windowKey] = entries
	a.markRuntimeStateChangedLocked()
	logging.Debugf(
		"group media cached event_id=%s window_key=%s message_type=%s attachments=%d",
		job.EventID,
		windowKey,
		job.MessageType,
		len(job.Attachments),
	)
}

func (a *App) mergeRecentGroupMediaWindow(job *Job) {
	if job == nil {
		return
	}
	if !isGroupChatType(job.ChatType) {
		return
	}

	senderIdentity := senderIdentityForJob(*job)
	if senderIdentity == "" {
		return
	}
	windowKey := buildMediaWindowKey(job.ReceiveID, senderIdentity)
	if windowKey == "" {
		return
	}

	now := job.ReceivedAt
	if now.IsZero() {
		now = a.now()
	}
	cutoff := now.Add(-groupMediaWindowTTL)
	sourceMessageID := strings.TrimSpace(job.SourceMessageID)

	a.mu.Lock()
	a.pruneExpiredMediaWindowLocked(now)
	entries := a.mediaWindow[windowKey]
	if len(entries) == 0 {
		a.mu.Unlock()
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
			remaining = append(remaining, entry)
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
		delete(a.mediaWindow, windowKey)
	} else {
		a.mediaWindow[windowKey] = remaining
	}
	if len(selected) > 0 {
		a.markRuntimeStateChangedLocked()
	}
	a.mu.Unlock()

	if len(selected) == 0 {
		return
	}

	mergedAttachments := 0
	for _, entry := range selected {
		mergedAttachments += len(entry.Attachments)
		job.Attachments = append(job.Attachments, cloneAttachments(entry.Attachments)...)
	}

	hint := fmt.Sprintf(
		"系统补充：已自动合并你在过去5分钟发送的%d条多媒体消息（共%d个附件）。",
		len(selected),
		mergedAttachments,
	)
	baseText := strings.TrimSpace(job.Text)
	if baseText == "" {
		job.Text = hint
	} else {
		job.Text = baseText + "\n\n" + hint
	}

	logging.Debugf(
		"group media merged event_id=%s window_key=%s merged_messages=%d merged_attachments=%d",
		job.EventID,
		windowKey,
		len(selected),
		mergedAttachments,
	)
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

	return &Job{
		ReceiveID:            receiveID,
		ReceiveIDType:        receiveIDType,
		ChatType:             strings.TrimSpace(deref(message.ChatType)),
		SenderOpenID:         strings.TrimSpace(extractOpenID(event)),
		SenderUserID:         strings.TrimSpace(extractUserID(event)),
		SourceMessageID:      strings.TrimSpace(deref(message.MessageId)),
		ReplyParentMessageID: extractReplyParentMessageID(message),
		MessageType:          "text",
		Text:                 "用户@了你，请结合其最近发送的多媒体继续处理。",
		RawContent:           strings.TrimSpace(deref(message.Content)),
		EventID:              eventID(event),
		ReceivedAt:           a.now(),
		SessionKey:           buildSessionKey(receiveIDType, receiveID),
	}, true
}

func (a *App) pruneExpiredMediaWindowLocked(now time.Time) {
	if len(a.mediaWindow) == 0 {
		return
	}
	if now.IsZero() {
		now = a.now()
	}
	cutoff := now.Add(-groupMediaWindowTTL)
	changed := false

	for key, entries := range a.mediaWindow {
		filtered := make([]mediaWindowEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.ReceivedAt.IsZero() {
				continue
			}
			if entry.ReceivedAt.Before(cutoff) {
				continue
			}
			if len(entry.Attachments) == 0 {
				continue
			}
			filtered = append(filtered, entry)
		}
		if len(filtered) == 0 {
			delete(a.mediaWindow, key)
			changed = true
			continue
		}
		if len(filtered) != len(entries) {
			a.mediaWindow[key] = filtered
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

func cloneAttachments(in []Attachment) []Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]Attachment, len(in))
	copy(out, in)
	return out
}
