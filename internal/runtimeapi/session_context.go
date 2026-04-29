package runtimeapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/sessionctx"
)

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(s.token) == "" {
			logging.Warnf("runtime api request rejected: token is not configured")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "runtime api token is not configured"})
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

func (l *authRateLimiter) RunCleanup(ctx context.Context, interval time.Duration) {
	if l == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanExpired(time.Now())
		}
	}
}

func (l *authRateLimiter) cleanExpired(now time.Time) {
	if l == nil {
		return
	}
	now = now.Local()
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, item := range l.items {
		if now.Sub(item.start) >= l.window {
			delete(l.items, key)
		}
	}
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

func sessionContextFromHeaders(c *gin.Context) (sessionctx.SessionContext, error) {
	session := sessionContextFromHeadersNoError(c)
	if err := session.Validate(); err != nil {
		return sessionctx.SessionContext{}, err
	}
	return session, nil
}

func sessionContextFromHeadersNoError(c *gin.Context) sessionctx.SessionContext {
	if c == nil {
		return sessionctx.SessionContext{}
	}
	return sessionctx.SessionContext{
		ReceiveIDType:   strings.TrimSpace(c.GetHeader(HeaderReceiveIDType)),
		ReceiveID:       strings.TrimSpace(c.GetHeader(HeaderReceiveID)),
		SourceMessageID: strings.TrimSpace(c.GetHeader(HeaderSourceMessageID)),
		ActorUserID:     strings.TrimSpace(c.GetHeader(HeaderActorUserID)),
		ActorOpenID:     strings.TrimSpace(c.GetHeader(HeaderActorOpenID)),
		ChatType:        strings.TrimSpace(c.GetHeader(HeaderChatType)),
		SessionKey:      strings.TrimSpace(c.GetHeader(HeaderSessionKey)),
	}
}

func defaultSessionKey(session sessionctx.SessionContext) string {
	if sessionKey := strings.TrimSpace(session.SessionKey); sessionKey != "" {
		return sessionKey
	}
	return scopeSessionKey(session)
}
