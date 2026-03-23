package runtimeapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/mcpbridge"
)

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(s.token) == "" {
			c.Next()
			return
		}
		auth := strings.TrimSpace(c.GetHeader("Authorization"))
		if auth != "Bearer "+s.token {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func sessionContextFromHeaders(c *gin.Context) (mcpbridge.SessionContext, error) {
	session := sessionContextFromHeadersNoError(c)
	if err := session.Validate(); err != nil {
		return mcpbridge.SessionContext{}, err
	}
	return session, nil
}

func sessionContextFromHeadersNoError(c *gin.Context) mcpbridge.SessionContext {
	if c == nil {
		return mcpbridge.SessionContext{}
	}
	return mcpbridge.SessionContext{
		ReceiveIDType:   strings.TrimSpace(c.GetHeader(HeaderReceiveIDType)),
		ReceiveID:       strings.TrimSpace(c.GetHeader(HeaderReceiveID)),
		ResourceRoot:    strings.TrimSpace(c.GetHeader(HeaderResourceRoot)),
		SourceMessageID: strings.TrimSpace(c.GetHeader(HeaderSourceMessageID)),
		ActorUserID:     strings.TrimSpace(c.GetHeader(HeaderActorUserID)),
		ActorOpenID:     strings.TrimSpace(c.GetHeader(HeaderActorOpenID)),
		ChatType:        strings.TrimSpace(c.GetHeader(HeaderChatType)),
		SessionKey:      strings.TrimSpace(c.GetHeader(HeaderSessionKey)),
	}
}

func defaultSessionKey(session mcpbridge.SessionContext) string {
	if sessionKey := strings.TrimSpace(session.SessionKey); sessionKey != "" {
		return sessionKey
	}
	return scopeSessionKey(session)
}
