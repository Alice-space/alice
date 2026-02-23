package connector

import (
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func extractUnionID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	return deref(event.Event.Sender.SenderId.UnionId)
}

func extractMentionedUsers(message *larkim.EventMessage) []MentionedUser {
	if message == nil {
		return nil
	}

	mentioned := make([]MentionedUser, 0, len(message.Mentions))
	seen := make(map[string]struct{}, len(message.Mentions))
	for _, mention := range message.Mentions {
		if mention == nil {
			continue
		}
		candidate := MentionedUser{
			Key:  strings.TrimSpace(deref(mention.Key)),
			Name: strings.TrimSpace(deref(mention.Name)),
		}
		if mention.Id != nil {
			candidate.OpenID = strings.TrimSpace(deref(mention.Id.OpenId))
			candidate.UserID = strings.TrimSpace(deref(mention.Id.UserId))
			candidate.UnionID = strings.TrimSpace(deref(mention.Id.UnionId))
		}
		appendMentionedUser(&mentioned, seen, candidate)
	}

	for _, rawID := range extractMentionUserIDs(message.Content) {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		candidate := MentionedUser{}
		switch {
		case strings.HasPrefix(id, "ou_"):
			candidate.OpenID = id
		case strings.HasPrefix(id, "on_"):
			candidate.UnionID = id
		default:
			candidate.UserID = id
		}
		appendMentionedUser(&mentioned, seen, candidate)
	}
	return mentioned
}

func appendMentionedUser(target *[]MentionedUser, seen map[string]struct{}, candidate MentionedUser) {
	candidate.Key = strings.TrimSpace(candidate.Key)
	candidate.Name = strings.TrimSpace(candidate.Name)
	candidate.OpenID = strings.TrimSpace(candidate.OpenID)
	candidate.UserID = strings.TrimSpace(candidate.UserID)
	candidate.UnionID = strings.TrimSpace(candidate.UnionID)
	if candidate.Key == "" && candidate.Name == "" && candidate.OpenID == "" && candidate.UserID == "" && candidate.UnionID == "" {
		return
	}

	dedupeKey := strings.Join([]string{
		candidate.OpenID,
		candidate.UserID,
		candidate.UnionID,
		candidate.Key,
	}, "|")
	if dedupeKey == "|||" {
		return
	}
	if _, ok := seen[dedupeKey]; ok {
		return
	}
	seen[dedupeKey] = struct{}{}
	*target = append(*target, candidate)
}
