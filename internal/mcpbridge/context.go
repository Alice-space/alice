package mcpbridge

import (
	"errors"
	"strings"
)

const (
	EnvReceiveIDType   = "ALICE_MCP_RECEIVE_ID_TYPE"
	EnvReceiveID       = "ALICE_MCP_RECEIVE_ID"
	EnvResourceRoot    = "ALICE_MCP_RESOURCE_ROOT"
	EnvSourceMessageID = "ALICE_MCP_SOURCE_MESSAGE_ID"
	EnvActorUserID     = "ALICE_MCP_ACTOR_USER_ID"
	EnvActorOpenID     = "ALICE_MCP_ACTOR_OPEN_ID"
	EnvChatType        = "ALICE_MCP_CHAT_TYPE"
	EnvSessionKey      = "ALICE_MCP_SESSION_KEY"
)

type SessionContext struct {
	ReceiveIDType   string
	ReceiveID       string
	ResourceRoot    string
	SourceMessageID string
	ActorUserID     string
	ActorOpenID     string
	ChatType        string
	SessionKey      string
}

func (c SessionContext) Validate() error {
	if strings.TrimSpace(c.ReceiveIDType) == "" {
		return errors.New("missing receive id type")
	}
	if strings.TrimSpace(c.ReceiveID) == "" {
		return errors.New("missing receive id")
	}
	return nil
}

func (c SessionContext) ToEnv() map[string]string {
	env := make(map[string]string, 8)
	env[EnvReceiveIDType] = strings.TrimSpace(c.ReceiveIDType)
	env[EnvReceiveID] = strings.TrimSpace(c.ReceiveID)
	if root := strings.TrimSpace(c.ResourceRoot); root != "" {
		env[EnvResourceRoot] = root
	}
	if sourceMessageID := strings.TrimSpace(c.SourceMessageID); sourceMessageID != "" {
		env[EnvSourceMessageID] = sourceMessageID
	}
	if actorUserID := strings.TrimSpace(c.ActorUserID); actorUserID != "" {
		env[EnvActorUserID] = actorUserID
	}
	if actorOpenID := strings.TrimSpace(c.ActorOpenID); actorOpenID != "" {
		env[EnvActorOpenID] = actorOpenID
	}
	if chatType := strings.TrimSpace(c.ChatType); chatType != "" {
		env[EnvChatType] = chatType
	}
	if sessionKey := strings.TrimSpace(c.SessionKey); sessionKey != "" {
		env[EnvSessionKey] = sessionKey
	}
	return env
}

func SessionContextFromEnv(getenv func(key string) string) SessionContext {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return SessionContext{
		ReceiveIDType:   strings.TrimSpace(getenv(EnvReceiveIDType)),
		ReceiveID:       strings.TrimSpace(getenv(EnvReceiveID)),
		ResourceRoot:    strings.TrimSpace(getenv(EnvResourceRoot)),
		SourceMessageID: strings.TrimSpace(getenv(EnvSourceMessageID)),
		ActorUserID:     strings.TrimSpace(getenv(EnvActorUserID)),
		ActorOpenID:     strings.TrimSpace(getenv(EnvActorOpenID)),
		ChatType:        strings.TrimSpace(getenv(EnvChatType)),
		SessionKey:      strings.TrimSpace(getenv(EnvSessionKey)),
	}
}
