package connector

import "strings"

func (p *Processor) resolveCanonicalSessionKeyLocked(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if _, ok := p.sessions[sessionKey]; ok {
		return sessionKey
	}
	for canonicalKey, state := range p.sessions {
		if stateMatchesSessionKey(canonicalKey, state, sessionKey) {
			return canonicalKey
		}
	}
	return ""
}

func normalizeSessionAliases(aliases []string, canonicalKey string) []string {
	normalized := make([]string, 0, len(aliases))
	for _, rawAlias := range aliases {
		alias := strings.TrimSpace(rawAlias)
		if alias == "" || alias == strings.TrimSpace(canonicalKey) || isThreadSessionAlias(alias) || containsSessionAlias(normalized, alias) {
			continue
		}
		normalized = append(normalized, alias)
		if len(normalized) >= maxSessionAliases {
			break
		}
	}
	return normalized
}

func appendSessionAlias(aliases []string, alias string) []string {
	return appendSessionAliasWithLimit(aliases, alias, maxSessionAliases)
}

func appendSessionAliasWithLimit(aliases []string, alias string, limit int) []string {
	aliases = normalizeSessionAliases(aliases, "")
	alias = strings.TrimSpace(alias)
	if alias == "" || containsSessionAlias(aliases, alias) {
		return aliases
	}
	aliases = append(aliases, alias)
	if limit <= 0 || len(aliases) <= limit {
		return aliases
	}
	return append([]string(nil), aliases[len(aliases)-limit:]...)
}

func removeSessionAlias(aliases []string, alias string) []string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return normalizeSessionAliases(aliases, "")
	}
	filtered := make([]string, 0, len(aliases))
	for _, rawAlias := range aliases {
		existing := strings.TrimSpace(rawAlias)
		if existing == "" || existing == alias {
			continue
		}
		filtered = append(filtered, existing)
	}
	return normalizeSessionAliases(filtered, "")
}

func containsSessionAlias(aliases []string, alias string) bool {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return false
	}
	for _, existing := range aliases {
		if strings.TrimSpace(existing) == alias {
			return true
		}
	}
	return false
}

func migrateWorkThreadID(aliases []string, workThreadID string) ([]string, string) {
	regular := make([]string, 0, len(aliases))
	workThreadID = strings.TrimSpace(workThreadID)
	for _, rawAlias := range aliases {
		alias := strings.TrimSpace(rawAlias)
		if alias == "" {
			continue
		}
		if isThreadSessionAlias(alias) {
			if workThreadID == "" {
				workThreadID = extractThreadIDFromAlias(alias)
			}
			continue
		}
		regular = appendSessionAliasWithLimit(regular, alias, maxSessionAliases)
	}
	return regular, workThreadID
}

func stateMatchesSessionKey(canonicalKey string, state sessionState, sessionKey string) bool {
	if containsSessionAlias(state.Aliases, sessionKey) {
		return true
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if buildWorkSceneSeedAlias(canonicalKey) == sessionKey {
		return true
	}
	return buildWorkSceneThreadAlias(canonicalKey, state.WorkThreadID) == sessionKey
}

func buildWorkSceneSeedAlias(canonicalKey string) string {
	canonicalKey = strings.TrimSpace(canonicalKey)
	idx := strings.Index(canonicalKey, workSceneSeedKeyToken)
	if idx <= 0 {
		return ""
	}
	base := strings.TrimSpace(canonicalKey[:idx])
	seedMessageID := strings.TrimSpace(canonicalKey[idx+len(workSceneSeedKeyToken):])
	if base == "" || seedMessageID == "" {
		return ""
	}
	return base + messageAliasToken + seedMessageID
}

func isThreadSessionAlias(alias string) bool {
	return strings.Contains(strings.TrimSpace(alias), threadAliasToken)
}

func buildWorkSceneThreadAlias(canonicalKey, workThreadID string) string {
	canonicalKey = strings.TrimSpace(canonicalKey)
	workThreadID = strings.TrimSpace(workThreadID)
	idx := strings.Index(canonicalKey, workSceneSeedKeyToken)
	if idx <= 0 || workThreadID == "" {
		return ""
	}
	base := strings.TrimSpace(canonicalKey[:idx])
	if base == "" {
		return ""
	}
	return base + threadAliasToken + workThreadID
}

func extractThreadIDFromAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	idx := strings.Index(alias, threadAliasToken)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(alias[idx+len(threadAliasToken):])
}

func scopeKeyFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, "|"); idx >= 0 {
		sessionKey = strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}
