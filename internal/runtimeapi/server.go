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
	addr            string
	token           string
	shutdownTimeout time.Duration
	sender          Sender
	automation      *automation.Store
	runtimeMu       sync.RWMutex
	runtime         automationRuntimeConfig
	engine          *gin.Engine
	httpSrv         *http.Server
	authLimiter     *authRateLimiter
	goalExecutor    GoalExecutor
}

type automationRuntimeConfig struct {
	llmProvider string
	llmProfiles map[string]config.LLMProfileConfig
	groupScenes config.GroupScenesConfig
	permissions config.BotPermissionsConfig
}

type GoalExecutor interface {
	ExecuteGoal(ctx context.Context, scope automation.Scope) error
}

func NewServer(
	addr, token string,
	sender Sender,
	automationStore *automation.Store,
	cfg config.Config,
) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)
	engine.Use(gin.Recovery())

	srv := &Server{
		addr:            strings.TrimSpace(addr),
		token:           strings.TrimSpace(token),
		shutdownTimeout: runtimeAPIShutdownTimeout(cfg),
		sender:          sender,
		automation:      automationStore,
		runtime:         newAutomationRuntimeConfig(cfg),
		engine:          engine,
		authLimiter:     newAuthRateLimiter(runtimeAPIAuthRateLimit, time.Minute),
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

	goal := api.Group("/goal")
	goal.GET("", srv.handleGoalGet)
	goal.POST("", srv.handleGoalCreate)
	goal.POST("/pause", srv.handleGoalPause)
	goal.POST("/resume", srv.handleGoalResume)
	goal.POST("/complete", srv.handleGoalComplete)
	goal.DELETE("", srv.handleGoalDelete)
	return srv
}

func (s *Server) SetGoalExecutor(executor GoalExecutor) {
	if s != nil {
		s.goalExecutor = executor
	}
}

func (s *Server) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("runtime api server is nil")
	}
	go s.authLimiter.RunCleanup(ctx, time.Minute)

	s.httpSrv = &http.Server{
		Addr:              s.addr,
		Handler:           s.engine,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20,
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func runtimeAPIShutdownTimeout(cfg config.Config) time.Duration {
	timeout := cfg.RuntimeAPIShutdownTimeout
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultRuntimeAPIShutdownTimeoutSecs) * time.Second
	}
	return timeout
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
