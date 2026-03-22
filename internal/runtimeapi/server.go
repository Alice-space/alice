package runtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/mcpbridge"
)

type Sender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
	SendImage(ctx context.Context, receiveIDType, receiveID, imageKey string) error
	SendFile(ctx context.Context, receiveIDType, receiveID, fileKey string) error
	UploadImage(ctx context.Context, localPath string) (string, error)
	UploadFile(ctx context.Context, localPath, fileName string) (string, error)
}

type replyTextSender interface {
	ReplyText(ctx context.Context, sourceMessageID, text string) (string, error)
}

type replyTextDirectSender interface {
	ReplyTextDirect(ctx context.Context, sourceMessageID, text string) (string, error)
}

type replyImageSender interface {
	ReplyImage(ctx context.Context, sourceMessageID, imageKey string) (string, error)
}

type replyImageDirectSender interface {
	ReplyImageDirect(ctx context.Context, sourceMessageID, imageKey string) (string, error)
}

type replyFileSender interface {
	ReplyFile(ctx context.Context, sourceMessageID, fileKey string) (string, error)
}

type replyFileDirectSender interface {
	ReplyFileDirect(ctx context.Context, sourceMessageID, fileKey string) (string, error)
}

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

func newAutomationRuntimeConfig(cfg config.Config) automationRuntimeConfig {
	return automationRuntimeConfig{
		llmProvider: cfg.LLMProvider,
		llmProfiles: cloneLLMProfiles(cfg.LLMProfiles),
		groupScenes: cfg.GroupScenes,
		permissions: cfg.Permissions,
	}
}

func cloneLLMProfiles(in map[string]config.LLMProfileConfig) map[string]config.LLMProfileConfig {
	if len(in) == 0 {
		return map[string]config.LLMProfileConfig{}
	}
	out := make(map[string]config.LLMProfileConfig, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	return s.addr
}

func (s *Server) BaseURL() string {
	if s == nil {
		return ""
	}
	return BaseURL(s.addr)
}

func (s *Server) Token() string {
	if s == nil {
		return ""
	}
	return s.token
}

func (s *Server) UpdateRuntimeConfig(cfg config.Config) {
	if s == nil {
		return
	}
	s.runtimeMu.Lock()
	s.runtime = newAutomationRuntimeConfig(cfg)
	s.runtimeMu.Unlock()
}

func (s *Server) runtimeConfig() automationRuntimeConfig {
	if s == nil {
		return automationRuntimeConfig{}
	}
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return automationRuntimeConfig{
		llmProvider: s.runtime.llmProvider,
		llmProfiles: cloneLLMProfiles(s.runtime.llmProfiles),
		groupScenes: s.runtime.groupScenes,
		permissions: s.runtime.permissions,
	}
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

func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(s.token) == "" {
			c.Next()
			return
		}
		auth := strings.TrimSpace(c.GetHeader("Authorization"))
		if auth != "Bearer "+s.token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func (s *Server) handleSendImage(c *gin.Context) {
	if !s.allowRuntimeMessage() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime message is disabled for this bot"})
		return
	}
	if s.sender == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sender is unavailable"})
		return
	}
	session, err := sessionContextFromHeaders(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req ImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	imageKey := strings.TrimSpace(req.ImageKey)
	if imageKey == "" {
		if err := validatePathUnderRoot(req.Path, session.ResourceRoot); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uploaded, err := s.sender.UploadImage(c.Request.Context(), strings.TrimSpace(req.Path))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upload image failed: %v", err)})
			return
		}
		imageKey = strings.TrimSpace(uploaded)
	}
	if imageKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image_key or path is required"})
		return
	}
	if err := s.dispatchImage(c.Request.Context(), session, imageKey); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		if err := s.dispatchText(c.Request.Context(), session, caption); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "type": "image", "image_key": imageKey})
}

func (s *Server) handleSendFile(c *gin.Context) {
	if !s.allowRuntimeMessage() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime message is disabled for this bot"})
		return
	}
	if s.sender == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sender is unavailable"})
		return
	}
	session, err := sessionContextFromHeaders(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req FileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fileKey := strings.TrimSpace(req.FileKey)
	if fileKey == "" {
		if err := validatePathUnderRoot(req.Path, session.ResourceRoot); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uploaded, err := s.sender.UploadFile(c.Request.Context(), strings.TrimSpace(req.Path), strings.TrimSpace(req.FileName))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upload file failed: %v", err)})
			return
		}
		fileKey = strings.TrimSpace(uploaded)
	}
	if fileKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_key or path is required"})
		return
	}
	if err := s.dispatchFile(c.Request.Context(), session, fileKey); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		if err := s.dispatchText(c.Request.Context(), session, caption); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "type": "file", "file_key": fileKey})
}

func (s *Server) handleAutomationTaskList(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	limit := 20
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		if _, err := fmt.Sscanf(rawLimit, "%d", &limit); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
	}
	tasks, err := s.automation.ListTasks(scopeCtx.scope, c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "count": len(tasks), "tasks": tasks})
}

func (s *Server) handleAutomationTaskCreate(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	task, err := s.buildTaskFromRequest(req, scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := s.automation.CreateTask(task)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": created})
}

func (s *Server) handleAutomationTaskGet(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	task, err := s.automation.GetTask(strings.TrimSpace(c.Param("taskID")))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if task.Scope != scopeCtx.scope {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found in current scope"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": task})
}

func (s *Server) handleAutomationTaskPatch(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	taskID := strings.TrimSpace(c.Param("taskID"))
	current, err := s.automation.GetTask(taskID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if current.Scope != scopeCtx.scope {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found in current scope"})
		return
	}
	if !canManageTask(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for task update"})
		return
	}
	patchBytes, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := applyTaskPatch(current, patchBytes, c.ContentType(), scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persisted, err := s.automation.PatchTask(taskID, func(task *automation.Task) error {
		*task = updated
		return nil
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": persisted})
}

func (s *Server) handleAutomationTaskDelete(c *gin.Context) {
	if !s.allowRuntimeAutomation() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime automation is disabled for this bot"})
		return
	}
	if s.automation == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "automation store is unavailable"})
		return
	}
	scopeCtx, err := resolveAutomationScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	taskID := strings.TrimSpace(c.Param("taskID"))
	deleted, err := s.automation.PatchTask(taskID, func(task *automation.Task) error {
		if task.Scope != scopeCtx.scope {
			return errors.New("task not found in current scope")
		}
		if !canManageTask(*task, scopeCtx.actorID) {
			return errors.New("permission denied for task delete")
		}
		task.Status = automation.TaskStatusDeleted
		task.NextRunAt = time.Time{}
		return nil
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, automation.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "task": deleted})
}

func (s *Server) allowRuntimeMessage() bool {
	return runtimePermissionEnabled(s.runtimeConfig().permissions.RuntimeMessage)
}

func (s *Server) allowRuntimeAutomation() bool {
	return runtimePermissionEnabled(s.runtimeConfig().permissions.RuntimeAutomation)
}

func (s *Server) allowRuntimeCampaigns() bool {
	return runtimePermissionEnabled(s.runtimeConfig().permissions.RuntimeCampaigns)
}

func runtimePermissionEnabled(flag *bool) bool {
	return flag == nil || *flag
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

type automationScopeContext struct {
	scope   automation.Scope
	route   automation.Route
	creator automation.Actor
	actorID string
	isGroup bool
	session mcpbridge.SessionContext
}

func resolveAutomationScope(session mcpbridge.SessionContext) (automationScopeContext, error) {
	if err := session.Validate(); err != nil {
		return automationScopeContext{}, err
	}
	actorUserID := strings.TrimSpace(session.ActorUserID)
	actorOpenID := strings.TrimSpace(session.ActorOpenID)
	actorID := actorUserID
	if actorID == "" {
		actorID = actorOpenID
	}
	if actorID == "" {
		return automationScopeContext{}, errors.New("missing actor id in runtime context")
	}
	chatType := strings.ToLower(strings.TrimSpace(session.ChatType))
	isGroup := chatType == "group" || chatType == "topic_group"
	ctx := automationScopeContext{
		creator: automation.Actor{
			UserID: actorUserID,
			OpenID: actorOpenID,
		},
		actorID: actorID,
		isGroup: isGroup,
		session: session,
	}
	if isGroup {
		receiveID := strings.TrimSpace(session.ReceiveID)
		if receiveID == "" {
			return automationScopeContext{}, errors.New("missing chat_id for group automation scope")
		}
		ctx.scope = automation.Scope{Kind: automation.ScopeKindChat, ID: receiveID}
		ctx.route = automation.Route{ReceiveIDType: "chat_id", ReceiveID: receiveID}
		return ctx, nil
	}
	ctx.scope = automation.Scope{Kind: automation.ScopeKindUser, ID: actorID}
	if actorUserID != "" {
		ctx.route = automation.Route{ReceiveIDType: "user_id", ReceiveID: actorUserID}
	} else if actorOpenID != "" {
		ctx.route = automation.Route{ReceiveIDType: "open_id", ReceiveID: actorOpenID}
	} else {
		ctx.route = automation.Route{ReceiveIDType: strings.TrimSpace(session.ReceiveIDType), ReceiveID: strings.TrimSpace(session.ReceiveID)}
	}
	return ctx, nil
}

func (s *Server) buildTaskFromRequest(req CreateTaskRequest, scopeCtx automationScopeContext) (automation.Task, error) {
	task := automation.Task{
		Title:      strings.TrimSpace(req.Title),
		Scope:      scopeCtx.scope,
		Route:      scopeCtx.route,
		Creator:    scopeCtx.creator,
		ManageMode: req.ManageMode,
		Schedule:   req.Schedule,
		Action:     req.Action,
		MaxRuns:    req.MaxRuns,
		Status:     automation.TaskStatusActive,
	}
	if task.ManageMode == "" {
		task.ManageMode = automation.ManageModeCreatorOnly
	}
	switch task.Schedule.Type {
	case "":
		if strings.TrimSpace(task.Schedule.CronExpr) != "" {
			task.Schedule.Type = automation.ScheduleTypeCron
		} else {
			task.Schedule.Type = automation.ScheduleTypeInterval
		}
	}
	if task.Schedule.Type == automation.ScheduleTypeInterval && task.Schedule.EverySeconds < 60 {
		return automation.Task{}, errors.New("every_seconds must be >= 60 for interval schedule")
	}
	if task.Schedule.Type == automation.ScheduleTypeCron && strings.TrimSpace(task.Schedule.CronExpr) == "" {
		return automation.Task{}, errors.New("cron_expr is required for cron schedule")
	}
	if task.Action.Type == "" {
		switch {
		case strings.TrimSpace(task.Action.Workflow) != "":
			task.Action.Type = automation.ActionTypeRunWorkflow
		case strings.TrimSpace(task.Action.Prompt) != "":
			task.Action.Type = automation.ActionTypeRunLLM
		default:
			task.Action.Type = automation.ActionTypeSendText
		}
	}
	applySceneLLMProfileDefaults(&task, scopeCtx, s.runtimeConfig())
	task.Action.SessionKey = scopeSessionKey(scopeCtx.session)
	if err := validateMentionPermission(scopeCtx, task.Action.MentionUserIDs); err != nil {
		return automation.Task{}, err
	}
	if req.Enabled != nil && !*req.Enabled {
		task.Status = automation.TaskStatusPaused
	}
	return automation.NormalizeTask(task), nil
}

func applySceneLLMProfileDefaults(task *automation.Task, scopeCtx automationScopeContext, runtime automationRuntimeConfig) {
	if task == nil {
		return
	}
	if task.Action.Type != automation.ActionTypeRunLLM && task.Action.Type != automation.ActionTypeRunWorkflow {
		return
	}
	profile, ok := resolveSceneLLMProfile(runtime, scopeCtx.session.SessionKey)
	if !ok {
		return
	}
	if strings.TrimSpace(task.Action.Model) == "" {
		task.Action.Model = strings.TrimSpace(profile.Model)
	}
	if strings.TrimSpace(task.Action.Provider) == "" {
		task.Action.Provider = strings.TrimSpace(profile.Provider)
		if task.Action.Provider == "" {
			task.Action.Provider = strings.TrimSpace(runtime.llmProvider)
		}
	}
	if strings.TrimSpace(task.Action.Profile) == "" {
		task.Action.Profile = strings.TrimSpace(profile.Profile)
	}
	if strings.TrimSpace(task.Action.ReasoningEffort) == "" {
		task.Action.ReasoningEffort = strings.TrimSpace(profile.ReasoningEffort)
	}
	if strings.TrimSpace(task.Action.Personality) == "" {
		task.Action.Personality = strings.TrimSpace(profile.Personality)
	}
}

func resolveSceneLLMProfile(runtime automationRuntimeConfig, sessionKey string) (config.LLMProfileConfig, bool) {
	sessionKey = strings.TrimSpace(sessionKey)
	switch {
	case strings.Contains(sessionKey, "|scene:work"):
		name := strings.TrimSpace(runtime.groupScenes.Work.LLMProfile)
		profile, ok := runtime.llmProfiles[name]
		return profile, ok
	case strings.Contains(sessionKey, "|scene:chat"):
		name := strings.TrimSpace(runtime.groupScenes.Chat.LLMProfile)
		profile, ok := runtime.llmProfiles[name]
		return profile, ok
	default:
		return config.LLMProfileConfig{}, false
	}
}

func applyTaskPatch(current automation.Task, patchBytes []byte, contentType string, scopeCtx automationScopeContext) (automation.Task, error) {
	current = automation.NormalizeTask(current)
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return automation.Task{}, err
	}

	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	patchedJSON := patchBytes
	switch contentType {
	case "application/json-patch+json":
		patch, err := jsonpatch.DecodePatch(patchBytes)
		if err != nil {
			return automation.Task{}, err
		}
		patchedJSON, err = patch.Apply(currentJSON)
		if err != nil {
			return automation.Task{}, err
		}
	case "application/merge-patch+json", "application/json", "":
		patchedJSON, err = jsonpatch.MergePatch(currentJSON, patchBytes)
		if err != nil {
			return automation.Task{}, err
		}
	default:
		return automation.Task{}, fmt.Errorf("unsupported patch content type %q", contentType)
	}

	var next automation.Task
	if err := json.Unmarshal(patchedJSON, &next); err != nil {
		return automation.Task{}, err
	}
	next = automation.NormalizeTask(next)
	next.ID = current.ID
	next.Scope = current.Scope
	next.Route = current.Route
	next.Creator = current.Creator
	next.CreatedAt = current.CreatedAt
	next.RunCount = current.RunCount
	next.LastRunAt = current.LastRunAt
	next.LastResult = current.LastResult
	next.ConsecutiveFailures = current.ConsecutiveFailures
	next.Running = current.Running
	next.Revision = current.Revision
	next.Action.SessionKey = scopeSessionKey(scopeCtx.session)
	if err := validateMentionPermission(scopeCtx, next.Action.MentionUserIDs); err != nil {
		return automation.Task{}, err
	}
	return next, nil
}

func scopeSessionKey(session mcpbridge.SessionContext) string {
	sessionKey := strings.TrimSpace(session.SessionKey)
	if sessionKey != "" {
		if idx := strings.Index(sessionKey, "|message:"); idx >= 0 {
			sessionKey = strings.TrimSpace(sessionKey[:idx])
		}
		return sessionKey
	}
	if strings.TrimSpace(session.ReceiveIDType) == "" || strings.TrimSpace(session.ReceiveID) == "" {
		return ""
	}
	return strings.TrimSpace(session.ReceiveIDType) + ":" + strings.TrimSpace(session.ReceiveID)
}

func validateMentionPermission(scopeCtx automationScopeContext, mentionUserIDs []string) error {
	if scopeCtx.isGroup {
		return nil
	}
	for _, mentionID := range mentionUserIDs {
		if strings.TrimSpace(mentionID) != strings.TrimSpace(scopeCtx.actorID) {
			return errors.New("private scope only allows mention current actor")
		}
	}
	return nil
}

func canManageTask(task automation.Task, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == task.Creator.PreferredID() || task.ManageMode == automation.ManageModeScopeAll
}

func (s *Server) dispatchText(ctx context.Context, session mcpbridge.SessionContext, text string) error {
	if sourceMessageID := strings.TrimSpace(session.SourceMessageID); sourceMessageID != "" {
		if !s.prefersThreadReply(session) {
			if replySender, ok := s.sender.(replyTextDirectSender); ok {
				_, err := replySender.ReplyTextDirect(ctx, sourceMessageID, text)
				return err
			}
		}
		if replySender, ok := s.sender.(replyTextSender); ok {
			_, err := replySender.ReplyText(ctx, sourceMessageID, text)
			return err
		}
	}
	return s.sender.SendText(ctx, session.ReceiveIDType, session.ReceiveID, text)
}

func (s *Server) dispatchImage(ctx context.Context, session mcpbridge.SessionContext, imageKey string) error {
	if sourceMessageID := strings.TrimSpace(session.SourceMessageID); sourceMessageID != "" {
		if !s.prefersThreadReply(session) {
			if replySender, ok := s.sender.(replyImageDirectSender); ok {
				_, err := replySender.ReplyImageDirect(ctx, sourceMessageID, imageKey)
				return err
			}
		}
		if replySender, ok := s.sender.(replyImageSender); ok {
			_, err := replySender.ReplyImage(ctx, sourceMessageID, imageKey)
			return err
		}
	}
	return s.sender.SendImage(ctx, session.ReceiveIDType, session.ReceiveID, imageKey)
}

func (s *Server) dispatchFile(ctx context.Context, session mcpbridge.SessionContext, fileKey string) error {
	if sourceMessageID := strings.TrimSpace(session.SourceMessageID); sourceMessageID != "" {
		if !s.prefersThreadReply(session) {
			if replySender, ok := s.sender.(replyFileDirectSender); ok {
				_, err := replySender.ReplyFileDirect(ctx, sourceMessageID, fileKey)
				return err
			}
		}
		if replySender, ok := s.sender.(replyFileSender); ok {
			_, err := replySender.ReplyFile(ctx, sourceMessageID, fileKey)
			return err
		}
	}
	return s.sender.SendFile(ctx, session.ReceiveIDType, session.ReceiveID, fileKey)
}

func (s *Server) prefersThreadReply(session mcpbridge.SessionContext) bool {
	cfg := s.runtimeConfig()
	sessionKey := strings.TrimSpace(session.SessionKey)
	switch {
	case strings.Contains(sessionKey, "|scene:chat"):
		return cfg.groupScenes.Chat.CreateFeishuThread
	case strings.Contains(sessionKey, "|scene:work"):
		return cfg.groupScenes.Work.CreateFeishuThread
	}
	switch strings.TrimSpace(session.ChatType) {
	case "group", "topic_group":
		return true
	default:
		return false
	}
}

func validatePathUnderRoot(path string, root string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("path is empty")
	}
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("resource root is empty")
	}
	pathAbs := filepath.Clean(path)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootAbs = filepath.Clean(rootAbs)
	rootInfo, err := os.Stat(rootAbs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("resource root does not exist: %s", rootAbs)
		}
		return err
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("resource root is not a directory: %s", rootAbs)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	resolvedPath, err := securejoin.SecureJoin(rootAbs, rel)
	if err != nil {
		return fmt.Errorf("path out of allowed root: %s", rootAbs)
	}
	if filepath.Clean(resolvedPath) != pathAbs {
		return fmt.Errorf("path out of allowed root: %s", rootAbs)
	}
	return nil
}
