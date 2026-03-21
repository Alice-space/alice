package connector

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type outgoingMentionCandidate struct {
	Alias   string
	UserID  string
	Display string
}

func normalizeOutgoingReplyWithMentions(reply string, job Job) (string, bool) {
	normalized := stripHiddenReplyMetadata(reply)
	if normalized == "" || !strings.Contains(normalized, "@") {
		return normalized, false
	}

	candidates := buildOutgoingMentionCandidates(job)
	if len(candidates) == 0 {
		return normalized, false
	}
	return replaceOutgoingMentions(normalized, candidates)
}

func buildOutgoingMentionCandidates(job Job) []outgoingMentionCandidate {
	candidates := make([]outgoingMentionCandidate, 0, len(job.MentionedUsers)*3+3)
	seenAliases := make(map[string]struct{}, cap(candidates))

	botOpenID := strings.TrimSpace(job.BotOpenID)
	botUserID := strings.TrimSpace(job.BotUserID)

	appendIdentity := func(name, key, openID, userID, unionID string) {
		resolvedID := preferredID(openID, userID, unionID)
		if resolvedID == "" || isBotIdentity(openID, userID, resolvedID, botOpenID, botUserID) {
			return
		}

		displayName := normalizeUserDisplayName(name, "")
		appendOutgoingMentionCandidate(&candidates, seenAliases, displayName, resolvedID, displayName)
		appendOutgoingMentionCandidate(&candidates, seenAliases, key, resolvedID, displayName)
		appendOutgoingMentionCandidate(
			&candidates,
			seenAliases,
			resolvedID,
			resolvedID,
			defaultIfEmpty(displayName, resolvedID),
		)
	}

	for _, mentioned := range job.MentionedUsers {
		appendIdentity(mentioned.Name, mentioned.Key, mentioned.OpenID, mentioned.UserID, mentioned.UnionID)
	}
	appendIdentity(job.SenderName, "", job.SenderOpenID, job.SenderUserID, job.SenderUnionID)

	sort.Slice(candidates, func(i, j int) bool {
		leftLen := utf8.RuneCountInString(candidates[i].Alias)
		rightLen := utf8.RuneCountInString(candidates[j].Alias)
		if leftLen != rightLen {
			return leftLen > rightLen
		}
		return len(candidates[i].Alias) > len(candidates[j].Alias)
	})
	return candidates
}

func appendOutgoingMentionCandidate(
	target *[]outgoingMentionCandidate,
	seenAliases map[string]struct{},
	alias, userID, display string,
) {
	alias = normalizeOutgoingMentionAlias(alias)
	userID = strings.TrimSpace(userID)
	display = strings.TrimSpace(display)
	if alias == "" || userID == "" {
		return
	}
	if display == "" {
		display = alias
	}

	aliasKey := strings.ToLower(alias)
	if _, exists := seenAliases[aliasKey]; exists {
		return
	}
	seenAliases[aliasKey] = struct{}{}
	*target = append(*target, outgoingMentionCandidate{
		Alias:   alias,
		UserID:  userID,
		Display: display,
	})
}

func normalizeOutgoingMentionAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	alias = strings.TrimPrefix(alias, "@")
	return strings.TrimSpace(alias)
}

func replaceOutgoingMentions(text string, candidates []outgoingMentionCandidate) (string, bool) {
	if strings.TrimSpace(text) == "" || len(candidates) == 0 || !strings.Contains(text, "@") {
		return text, false
	}

	var builder strings.Builder
	builder.Grow(len(text) + 32)
	changed := false

	for idx := 0; idx < len(text); {
		if text[idx] != '@' {
			_, size := utf8.DecodeRuneInString(text[idx:])
			if size <= 0 {
				size = 1
			}
			builder.WriteString(text[idx : idx+size])
			idx += size
			continue
		}

		if !isMentionTokenBoundaryBefore(text, idx) {
			builder.WriteByte('@')
			idx++
			continue
		}

		rest := text[idx+1:]
		matched := false
		for _, candidate := range candidates {
			if !hasFoldPrefix(rest, candidate.Alias) {
				continue
			}
			end := idx + 1 + len(candidate.Alias)
			if !isMentionTokenBoundaryAfter(text, end) {
				continue
			}

			builder.WriteString(`<at user_id="`)
			builder.WriteString(candidate.UserID)
			builder.WriteString(`">`)
			builder.WriteString(candidate.Display)
			builder.WriteString(`</at>`)

			idx = end
			changed = true
			matched = true
			break
		}
		if matched {
			continue
		}

		builder.WriteByte('@')
		idx++
	}

	if !changed {
		return text, false
	}
	return builder.String(), true
}

func hasFoldPrefix(text, prefix string) bool {
	if len(text) < len(prefix) {
		return false
	}
	return strings.EqualFold(text[:len(prefix)], prefix)
}

func isMentionTokenBoundaryBefore(text string, index int) bool {
	if index <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func isMentionTokenBoundaryAfter(text string, index int) bool {
	if index >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[index:])
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}
