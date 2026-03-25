package runtimeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/gin-gonic/gin"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/mcpbridge"
)

type campaignScopeContext struct {
	session campaign.SessionRoute
	creator campaign.Actor
	actorID string
	isGroup bool
}

func (s *Server) handleCampaignList(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	limit, err := parseListLimit(c.Query("limit"), 20)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := s.campaigns.ListCampaigns(scopeCtx.session.VisibilityKey(), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "count": len(items), "campaigns": items})
}

func (s *Server) handleCampaignCreate(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	var req CreateCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := buildCampaignFromRequest(req, scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	created, err := s.campaigns.CreateCampaign(item)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=campaign_create actor=%s campaign=%s scope=%s", scopeCtx.actorID, created.ID, created.Session.ScopeKey)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": created})
}

func (s *Server) handleCampaignGet(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	item, ok := s.loadScopedCampaign(c, scopeCtx, strings.TrimSpace(c.Param("campaignID")), "")
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": item})
}

func (s *Server) handleCampaignPatch(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	current, ok := s.loadScopedCampaign(c, scopeCtx, campaignID, "campaign update")
	if !ok {
		return
	}
	patchBytes, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := applyCampaignPatch(current, patchBytes, c.ContentType(), scopeCtx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persisted, err := s.campaigns.PatchCampaign(campaignID, func(item *campaign.Campaign) error {
		*item = updated
		return nil
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=campaign_patch actor=%s campaign=%s scope=%s", scopeCtx.actorID, persisted.ID, persisted.Session.ScopeKey)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": persisted})
}

func (s *Server) handleCampaignTrialUpsert(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	if _, ok := s.loadScopedCampaign(c, scopeCtx, campaignID, "trial update"); !ok {
		return
	}
	var req UpsertTrialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, trial, err := s.campaigns.UpsertTrial(campaignID, req.Trial)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=campaign_trial_upsert actor=%s campaign=%s trial=%s", scopeCtx.actorID, updated.ID, trial.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "trial": trial})
}

func (s *Server) handleCampaignGuidanceAdd(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	if _, ok := s.loadScopedCampaign(c, scopeCtx, campaignID, "guidance update"); !ok {
		return
	}
	var req AddGuidanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, guidance, err := s.campaigns.AppendGuidance(campaignID, req.Guidance)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=campaign_guidance_add actor=%s campaign=%s guidance=%s", scopeCtx.actorID, updated.ID, guidance.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "guidance": guidance})
}

func (s *Server) handleCampaignReviewAdd(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	if _, ok := s.loadScopedCampaign(c, scopeCtx, campaignID, "review update"); !ok {
		return
	}
	var req AddReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, review, err := s.campaigns.AppendReview(campaignID, req.Review)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=campaign_review_add actor=%s campaign=%s review=%s", scopeCtx.actorID, updated.ID, review.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "review": review})
}

func (s *Server) handleCampaignPitfallAdd(c *gin.Context) {
	scopeCtx, ok := s.resolveCampaignRequestScope(c)
	if !ok {
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	if _, ok := s.loadScopedCampaign(c, scopeCtx, campaignID, "pitfall update"); !ok {
		return
	}
	var req AddPitfallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, pitfall, err := s.campaigns.AppendPitfall(campaignID, req.Pitfall)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	logging.Infof("runtime api audit action=campaign_pitfall_add actor=%s campaign=%s pitfall=%s", scopeCtx.actorID, updated.ID, pitfall.ID)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "pitfall": pitfall})
}

func (s *Server) resolveCampaignRequestScope(c *gin.Context) (campaignScopeContext, bool) {
	if !s.allowRuntimeCampaigns() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime campaigns are disabled for this bot"})
		return campaignScopeContext{}, false
	}
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return campaignScopeContext{}, false
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return campaignScopeContext{}, false
	}
	return scopeCtx, true
}

func (s *Server) loadScopedCampaign(c *gin.Context, scopeCtx campaignScopeContext, campaignID, manageAction string) (campaign.Campaign, bool) {
	item, err := s.campaigns.GetCampaign(strings.TrimSpace(campaignID))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return campaign.Campaign{}, false
	}
	if !campaignVisibleToSession(item, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return campaign.Campaign{}, false
	}
	if strings.TrimSpace(manageAction) != "" && !canManageCampaign(item, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for " + strings.TrimSpace(manageAction)})
		return campaign.Campaign{}, false
	}
	return item, true
}

func resolveCampaignScope(session mcpbridge.SessionContext) (campaignScopeContext, error) {
	runtimeCtx, err := resolveRuntimeSessionContext(session)
	if err != nil {
		return campaignScopeContext{}, err
	}
	if runtimeCtx.scopeKey == "" {
		return campaignScopeContext{}, errors.New("missing scope session key in runtime context")
	}
	return campaignScopeContext{
		session: campaign.SessionRoute{
			ScopeKey:      runtimeCtx.scopeKey,
			ReceiveIDType: runtimeCtx.receiveIDType,
			ReceiveID:     runtimeCtx.receiveID,
			ChatType:      runtimeCtx.chatType,
		},
		creator: campaign.Actor{
			UserID: runtimeCtx.actorUserID,
			OpenID: runtimeCtx.actorOpenID,
		},
		actorID: runtimeCtx.actorID,
		isGroup: runtimeCtx.isGroup,
	}, nil
}

func buildCampaignFromRequest(req CreateCampaignRequest, scopeCtx campaignScopeContext) (campaign.Campaign, error) {
	item := campaign.Campaign{
		Title:             strings.TrimSpace(req.Title),
		Objective:         strings.TrimSpace(req.Objective),
		Repo:              strings.TrimSpace(req.Repo),
		CampaignRepoPath:  strings.TrimSpace(req.CampaignRepoPath),
		IssueIID:          strings.TrimSpace(req.IssueIID),
		IssueURL:          strings.TrimSpace(req.IssueURL),
		Session:           scopeCtx.session,
		Creator:           scopeCtx.creator,
		ManageMode:        req.ManageMode,
		Status:            campaign.StatusPlanned,
		MaxParallelTrials: req.MaxParallelTrials,
		Summary:           strings.TrimSpace(req.Summary),
		Baseline:          req.Baseline,
		Gates:             req.Gates,
		Tags:              req.Tags,
	}
	return campaign.NormalizeCampaign(item), nil
}

func applyCampaignPatch(
	current campaign.Campaign,
	patchBytes []byte,
	contentType string,
	scopeCtx campaignScopeContext,
) (campaign.Campaign, error) {
	current = campaign.NormalizeCampaign(current)
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return campaign.Campaign{}, err
	}

	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	patchedJSON := patchBytes
	switch contentType {
	case "application/json-patch+json":
		patch, err := jsonpatch.DecodePatch(patchBytes)
		if err != nil {
			return campaign.Campaign{}, err
		}
		patchedJSON, err = patch.Apply(currentJSON)
		if err != nil {
			return campaign.Campaign{}, err
		}
	case "application/merge-patch+json", "application/json", "":
		patchedJSON, err = jsonpatch.MergePatch(currentJSON, patchBytes)
		if err != nil {
			return campaign.Campaign{}, err
		}
	default:
		return campaign.Campaign{}, fmt.Errorf("unsupported patch content type %q", contentType)
	}

	var next campaign.Campaign
	if err := json.Unmarshal(patchedJSON, &next); err != nil {
		return campaign.Campaign{}, err
	}
	next = campaign.NormalizeCampaign(next)
	next.ID = current.ID
	next.Session = current.Session
	next.Creator = current.Creator
	next.CreatedAt = current.CreatedAt
	next.Revision = current.Revision
	if next.ManageMode == campaign.ManageModeScopeAll && !scopeCtx.isGroup {
		return campaign.Campaign{}, errors.New("private scope does not support scope_all manage mode")
	}
	return next, nil
}

func campaignVisibleToSession(item campaign.Campaign, session campaign.SessionRoute) bool {
	visibilityKey := session.VisibilityKey()
	if visibilityKey == "" {
		return false
	}
	return item.Session.VisibilityKey() == visibilityKey
}

func canManageCampaign(item campaign.Campaign, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == item.Creator.PreferredID() || item.ManageMode == campaign.ManageModeScopeAll
}
