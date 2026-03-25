package runtimeapi

import (
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/mcpbridge"
)

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(s.token) == "" {
			c.Next()
			return
		}
		if s.authLimiter != nil && !s.authLimiter.Allow(authRateKey(c), time.Now()) {
			c.AbortWithStatusJSON(429, gin.H{"error": "too many requests"})
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

type authRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	items  map[string]authRateWindow
}

type authRateWindow struct {
	start time.Time
	count int
}

func newAuthRateLimiter(limit int, window time.Duration) *authRateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	return &authRateLimiter{
		limit:  limit,
		window: window,
		items:  make(map[string]authRateWindow),
	}
}

func (l *authRateLimiter) Allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	now = now.Local()
	l.mu.Lock()
	defer l.mu.Unlock()
	for itemKey, item := range l.items {
		if now.Sub(item.start) >= l.window {
			delete(l.items, itemKey)
		}
	}
	item := l.items[key]
	if item.start.IsZero() || now.Sub(item.start) >= l.window {
		item = authRateWindow{start: now}
	}
	item.count++
	l.items[key] = item
	return item.count <= l.limit
}

func authRateKey(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if clientIP := strings.TrimSpace(c.ClientIP()); clientIP != "" {
		return clientIP
	}
	if c.Request == nil {
		return ""
	}
	return strings.TrimSpace(c.Request.RemoteAddr)
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
