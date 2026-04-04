package sessionkey

import "strings"

const messageToken = "|message:"
const threadToken = "|thread:"
const seedToken = "|seed:"

func Build(receiveIDType, receiveID string) string {
	receiveIDType = strings.TrimSpace(receiveIDType)
	receiveID = strings.TrimSpace(receiveID)
	if receiveIDType == "" || receiveID == "" {
		return ""
	}
	return receiveIDType + ":" + receiveID
}

func VisibilityKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, "|"); idx >= 0 {
		sessionKey = strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}

func WithoutMessage(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, messageToken); idx >= 0 {
		sessionKey = strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}

// ExtractThreadID returns the Feishu thread ID (omt_xxx) embedded in a session
// key as a "|thread:omt_xxx" token, or empty string if none is present.
func ExtractThreadID(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	idx := strings.Index(sessionKey, threadToken)
	if idx < 0 {
		return ""
	}
	rest := sessionKey[idx+len(threadToken):]
	if pipe := strings.Index(rest, "|"); pipe >= 0 {
		rest = rest[:pipe]
	}
	return strings.TrimSpace(rest)
}

// ExtractSeedMessageID returns the seed message ID (om_xxx) embedded in a
// session key as a "|seed:om_xxx" token, or empty string if none is present.
// The seed is the root/anchor message of a Feishu work-thread session; it can
// be used with the Reply API (reply_in_thread=true) to continue the thread.
func ExtractSeedMessageID(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	idx := strings.Index(sessionKey, seedToken)
	if idx < 0 {
		return ""
	}
	rest := sessionKey[idx+len(seedToken):]
	if pipe := strings.Index(rest, "|"); pipe >= 0 {
		rest = rest[:pipe]
	}
	return strings.TrimSpace(rest)
}
