package runtimeapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/messaging"
)

type Sender = messaging.RuntimeSender
type replyTextSender = messaging.ReplyTextSender
type replyTextDirectSender = messaging.ReplyTextDirectSender
type replyImageSender = messaging.ReplyImageSender
type replyImageDirectSender = messaging.ReplyImageDirectSender
type replyFileSender = messaging.ReplyFileSender
type replyFileDirectSender = messaging.ReplyFileDirectSender

const runtimeAPIRequestBodyLimitBytes int64 = 1 << 20
const runtimeAPIMaxListLimit = 200
const runtimeAPIAuthRateLimit = 120

type Server struct {
	addr        string
	token       string
	sender      Sender
	automation  *automation.Store
	campaigns   *campaign.Store
	runtimeMu   sync.RWMutex
	runtime     automationRuntimeConfig
	engine      *gin.Engine
	httpSrv     *http.Server
	authLimiter *authRateLimiter
}

type automationRuntimeConfig struct {
	llmProvider string
	llmProfiles map[string]config.LLMProfileConfig
	groupScenes config.GroupScenesConfig
	permissions config.BotPermissionsConfig
}

func NewServer(
	addr, token string,
	sender Sender,
	automationStore *automation.Store,
	campaignStore *campaign.Store,
	cfg config.Config,
) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	srv := &Server{
		addr:        strings.TrimSpace(addr),
		token:       strings.TrimSpace(token),
		sender:      sender,
		automation:  automationStore,
		campaigns:   campaignStore,
		runtime:     newAutomationRuntimeConfig(cfg),
		engine:      engine,
		authLimiter: newAuthRateLimiter(runtimeAPIAuthRateLimit, time.Minute),
	}
	engine.Use(srv.requestBodyLimitMiddleware(runtimeAPIRequestBodyLimitBytes))
	engine.Use(srv.authMiddleware())
	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := engine.Group("/api/v1")
	api.POST("/messages/image", srv.handleSendImage)
	api.POST("/messages/file", srv.handleSendFile)
	api.GET("/automation/tasks", srv.handleAutomationTaskList)
	api.POST("/automation/tasks", srv.handleAutomationTaskCreate)
	api.GET("/automation/tasks/:taskID", srv.handleAutomationTaskGet)
	api.PATCH("/automation/tasks/:taskID", srv.handleAutomationTaskPatch)
	api.DELETE("/automation/tasks/:taskID", srv.handleAutomationTaskDelete)
	api.GET("/campaigns", srv.handleCampaignList)
	api.POST("/campaigns", srv.handleCampaignCreate)
	api.GET("/campaigns/:campaignID", srv.handleCampaignGet)
	api.PATCH("/campaigns/:campaignID", srv.handleCampaignPatch)
	api.DELETE("/campaigns/:campaignID", srv.handleCampaignDelete)
	api.POST("/campaigns/:campaignID/trials", srv.handleCampaignTrialUpsert)
	api.POST("/campaigns/:campaignID/guidance", srv.handleCampaignGuidanceAdd)
	api.POST("/campaigns/:campaignID/reviews", srv.handleCampaignReviewAdd)
	api.POST("/campaigns/:campaignID/pitfalls", srv.handleCampaignPitfallAdd)
	return srv
}

func (s *Server) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("runtime api server is nil")
	}
	s.httpSrv = &http.Server{
		Addr:    s.addr,
		Handler: s.engine,
	}
	errCh := make(chan error, 1)
	go func() {
		err := s.httpSrv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) requestBodyLimitMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limit > 0 && c.Request != nil && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

func parseListLimit(raw string, defaultLimit int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid limit")
	}
	if limit <= 0 || limit > runtimeAPIMaxListLimit {
		return 0, errors.New("limit must be between 1 and 200")
	}
	return limit, nil
}
