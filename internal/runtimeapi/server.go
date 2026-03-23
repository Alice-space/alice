package runtimeapi

import (
	"context"
	"errors"
	"net/http"
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

type Server struct {
	addr       string
	token      string
	sender     Sender
	automation *automation.Store
	campaigns  *campaign.Store
	runtimeMu  sync.RWMutex
	runtime    automationRuntimeConfig
	engine     *gin.Engine
	httpSrv    *http.Server
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
		addr:       strings.TrimSpace(addr),
		token:      strings.TrimSpace(token),
		sender:     sender,
		automation: automationStore,
		campaigns:  campaignStore,
		runtime:    newAutomationRuntimeConfig(cfg),
		engine:     engine,
	}
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
