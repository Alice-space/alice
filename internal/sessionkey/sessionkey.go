package sessionkey

import "strings"

const messageToken = "|message:"

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
