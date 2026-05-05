package connector

import (
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

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
		return strings.TrimSpace(sessionKey[:idx])
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

	var candidates []string

	if message != nil {
		threadID := strings.TrimSpace(deref(message.ThreadId))
		rootID := strings.TrimSpace(deref(message.RootId))
		parentID := strings.TrimSpace(deref(message.ParentId))
		sourceMessageID := strings.TrimSpace(deref(message.MessageId))

		if threadID != "" {
			appendCandidate(&candidates, base+threadBindingToken+threadID)
		}
		if rootID != "" {
			appendCandidate(&candidates, buildWorkSessionKey(receiveIDType, receiveID, rootID))
		}
		if parentID != "" && parentID != rootID {
			appendCandidate(&candidates, buildWorkSessionKey(receiveIDType, receiveID, parentID))
		}
		if threadID != "" || rootID != "" || parentID != "" {
		} else if sourceMessageID != "" {
			appendCandidate(&candidates, buildSessionKey(receiveIDType, receiveID)+messageBindingToken+sourceMessageID)
		}
	}

	appendCandidate(&candidates, base)

	return candidates
}

func appendCandidate(candidates *[]string, candidate string) {
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
