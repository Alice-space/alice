package runtimeapi

import (
	"errors"
	"strings"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/sessionkey"
)

type runtimeSessionContext struct {
	actorUserID   string
	actorOpenID   string
	actorID       string
	receiveIDType string
	receiveID     string
	chatType      string
	scopeKey      string
	isGroup       bool
}

func resolveRuntimeSessionContext(session mcpbridge.SessionContext) (runtimeSessionContext, error) {
	if err := session.Validate(); err != nil {
		return runtimeSessionContext{}, err
	}
	actorUserID := strings.TrimSpace(session.ActorUserID)
	actorOpenID := strings.TrimSpace(session.ActorOpenID)
	actorID := actorUserID
	if actorID == "" {
		actorID = actorOpenID
	}
	if actorID == "" {
		return runtimeSessionContext{}, errors.New("missing actor id in runtime context")
	}
	chatType := strings.ToLower(strings.TrimSpace(session.ChatType))
	return runtimeSessionContext{
		actorUserID:   actorUserID,
		actorOpenID:   actorOpenID,
		actorID:       actorID,
		receiveIDType: strings.TrimSpace(session.ReceiveIDType),
		receiveID:     strings.TrimSpace(session.ReceiveID),
		chatType:      chatType,
		scopeKey:      scopeSessionKey(session),
		isGroup:       chatType == "group" || chatType == "topic_group",
	}, nil
}

func scopeSessionKey(session mcpbridge.SessionContext) string {
	if sessionKey := sessionkey.WithoutMessage(session.SessionKey); sessionKey != "" {
		return sessionKey
	}
	return sessionkey.Build(session.ReceiveIDType, session.ReceiveID)
}
