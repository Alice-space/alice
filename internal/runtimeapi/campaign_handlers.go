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
	"github.com/Alice-space/alice/internal/mcpbridge"
)

type campaignScopeContext struct {
	session campaign.SessionRoute
	creator campaign.Actor
	actorID string
	isGroup bool
}

func (s *Server) handleCampaignList(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
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
	items, err := s.campaigns.ListCampaigns(scopeCtx.session.VisibilityKey(), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "count": len(items), "campaigns": items})
}

func (s *Server) handleCampaignCreate(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": created})
}

func (s *Server) handleCampaignGet(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := s.campaigns.GetCampaign(strings.TrimSpace(c.Param("campaignID")))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if !campaignVisibleToSession(item, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": item})
}

func (s *Server) handleCampaignPatch(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	current, err := s.campaigns.GetCampaign(campaignID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if !campaignVisibleToSession(current, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return
	}
	if !canManageCampaign(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for campaign update"})
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
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": persisted})
}

func (s *Server) handleCampaignTrialUpsert(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	current, err := s.campaigns.GetCampaign(campaignID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if !campaignVisibleToSession(current, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return
	}
	if !canManageCampaign(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for trial update"})
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
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "trial": trial})
}

func (s *Server) handleCampaignGuidanceAdd(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	current, err := s.campaigns.GetCampaign(campaignID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if !campaignVisibleToSession(current, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return
	}
	if !canManageCampaign(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for guidance update"})
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
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "guidance": guidance})
}

func (s *Server) handleCampaignReviewAdd(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	current, err := s.campaigns.GetCampaign(campaignID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if !campaignVisibleToSession(current, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return
	}
	if !canManageCampaign(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for review update"})
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
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "review": review})
}

func (s *Server) handleCampaignPitfallAdd(c *gin.Context) {
	if s.campaigns == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "campaign store is unavailable"})
		return
	}
	scopeCtx, err := resolveCampaignScope(sessionContextFromHeadersNoError(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	campaignID := strings.TrimSpace(c.Param("campaignID"))
	current, err := s.campaigns.GetCampaign(campaignID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, campaign.ErrCampaignNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	if !campaignVisibleToSession(current, scopeCtx.session) {
		c.JSON(http.StatusNotFound, gin.H{"error": "campaign not found in current scope"})
		return
	}
	if !canManageCampaign(current, scopeCtx.actorID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied for pitfall update"})
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
	c.JSON(http.StatusOK, gin.H{"status": "ok", "campaign": updated, "pitfall": pitfall})
}

func resolveCampaignScope(session mcpbridge.SessionContext) (campaignScopeContext, error) {
	if err := session.Validate(); err != nil {
		return campaignScopeContext{}, err
	}
	actorUserID := strings.TrimSpace(session.ActorUserID)
	actorOpenID := strings.TrimSpace(session.ActorOpenID)
	actorID := actorUserID
	if actorID == "" {
		actorID = actorOpenID
	}
	if actorID == "" {
		return campaignScopeContext{}, errors.New("missing actor id in runtime context")
	}
	scopeKey := scopeSessionKey(session)
	if scopeKey == "" {
		return campaignScopeContext{}, errors.New("missing scope session key in runtime context")
	}
	chatType := strings.ToLower(strings.TrimSpace(session.ChatType))
	isGroup := chatType == "group" || chatType == "topic_group"
	return campaignScopeContext{
		session: campaign.SessionRoute{
			ScopeKey:      scopeKey,
			ReceiveIDType: strings.TrimSpace(session.ReceiveIDType),
			ReceiveID:     strings.TrimSpace(session.ReceiveID),
			ChatType:      chatType,
		},
		creator: campaign.Actor{
			UserID: actorUserID,
			OpenID: actorOpenID,
		},
		actorID: actorID,
		isGroup: isGroup,
	}, nil
}

func buildCampaignFromRequest(req CreateCampaignRequest, scopeCtx campaignScopeContext) (campaign.Campaign, error) {
	item := campaign.Campaign{
		Title:             strings.TrimSpace(req.Title),
		Objective:         strings.TrimSpace(req.Objective),
		Repo:              strings.TrimSpace(req.Repo),
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
