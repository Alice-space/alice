package sessionctx

import (
	"errors"
	"strings"
)

const (
	EnvReceiveIDType   = "ALICE_RECEIVE_ID_TYPE"
	EnvReceiveID       = "ALICE_RECEIVE_ID"
	EnvSourceMessageID = "ALICE_SOURCE_MESSAGE_ID"
	EnvActorUserID     = "ALICE_ACTOR_USER_ID"
	EnvActorOpenID     = "ALICE_ACTOR_OPEN_ID"
	EnvChatType        = "ALICE_CHAT_TYPE"
	EnvSessionKey      = "ALICE_SESSION_KEY"
	EnvResumeThreadID  = "ALICE_RESUME_THREAD_ID"
)

type SessionContext struct {
	ReceiveIDType   string
	ReceiveID       string
	SourceMessageID string
	ActorUserID     string
	ActorOpenID     string
	ChatType        string
	SessionKey      string
	// ResumeThreadID is the Claude Code session UUID of the current session.
	// Skills can read ALICE_RESUME_THREAD_ID to use it as
	// action.resume_thread_id when creating resume-mode scheduled tasks.
	ResumeThreadID string
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
	if resumeThreadID := strings.TrimSpace(c.ResumeThreadID); resumeThreadID != "" {
		env[EnvResumeThreadID] = resumeThreadID
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
		SourceMessageID: strings.TrimSpace(getenv(EnvSourceMessageID)),
		ActorUserID:     strings.TrimSpace(getenv(EnvActorUserID)),
		ActorOpenID:     strings.TrimSpace(getenv(EnvActorOpenID)),
		ChatType:        strings.TrimSpace(getenv(EnvChatType)),
		SessionKey:      strings.TrimSpace(getenv(EnvSessionKey)),
		ResumeThreadID:  strings.TrimSpace(getenv(EnvResumeThreadID)),
	}
}
