package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/logging"
)

func (b *connectorRuntimeBuilder) HandleCardAction(ctx context.Context, req connector.CardActionRequest) (connector.CardActionResult, error) {
	switch req.Kind {
	case connector.CardActionKindCampaignPlanApproval:
		return b.handleCampaignPlanApprovalCardAction(ctx, req)
	default:
		return connector.CardActionResult{}, errors.New("暂不支持这个卡片动作")
	}
}

func (b *connectorRuntimeBuilder) handleCampaignPlanApprovalCardAction(ctx context.Context, req connector.CardActionRequest) (connector.CardActionResult, error) {
	if b == nil || b.campaignStore == nil {
		return connector.CardActionResult{}, errors.New("campaign store 不可用，无法处理审批")
	}
	item, err := b.campaignStore.GetCampaign(req.CampaignID)
	if err != nil {
		return connector.CardActionResult{}, err
	}
	if !canManageCampaignCardAction(item, preferredCardActionActorID(req)) {
		return connector.CardActionResult{}, errors.New("只有 campaign 创建者或 scope_all 管理者可以审批")
	}

	b.campaignRepoMu.Lock()
	defer b.campaignRepoMu.Unlock()

	repo, err := campaignrepo.Load(item.CampaignRepoPath)
	if err != nil {
		return connector.CardActionResult{}, err
	}
	if campaignrepo.PlanStatusPlanApproved != repo.Campaign.Frontmatter.PlanStatus &&
		campaignrepo.PlanStatusPlanApproved != normalizeRepoPlanStatus(repo.Campaign.Frontmatter.PlanStatus) {
		return connector.CardActionResult{}, errors.New("当前计划已经不在待人工批准状态，请刷新最新卡片")
	}
	if req.PlanRound > 0 && repo.Campaign.Frontmatter.PlanRound != req.PlanRound {
		return connector.CardActionResult{}, fmt.Errorf("卡片已过期：当前已进入第 %d 轮规划", repo.Campaign.Frontmatter.PlanRound)
	}

	switch req.Decision {
	case connector.CardActionDecisionApprove:
		return b.approveCampaignPlanFromCard(ctx, item, req.OpenMessageID)
	case connector.CardActionDecisionReject:
		return b.rejectCampaignPlanFromCard(ctx, item, req.OpenMessageID)
	default:
		return connector.CardActionResult{}, errors.New("不支持的审批动作")
	}
}

func (b *connectorRuntimeBuilder) approveCampaignPlanFromCard(
	ctx context.Context,
	item campaign.Campaign,
	openMessageID string,
) (connector.CardActionResult, error) {
	_, validation, err := campaignrepo.ApprovePlan(item.CampaignRepoPath)
	if err != nil {
		return connector.CardActionResult{}, err
	}
	if !validation.Valid {
		if validationErr := validation.Error(); validationErr != nil {
			return connector.CardActionResult{}, fmt.Errorf("当前计划未通过批准前校验：%w", validationErr)
		}
		return connector.CardActionResult{}, errors.New("当前计划未通过批准前校验")
	}
	if _, err := b.campaignStore.PatchCampaign(item.ID, func(current *campaign.Campaign) error {
		current.Status = campaign.StatusRunning
		return nil
	}); err != nil {
		return connector.CardActionResult{}, err
	}
	result := connector.CardActionResult{
		ToastType: "success",
		Toast:     "已批准当前计划，Alice 将继续派发执行任务。",
	}
	if err := b.patchCampaignPlanDecisionCard(ctx, openMessageID, item, true, 0); err != nil {
		logging.Warnf("patch approved campaign plan card failed campaign=%s message=%s: %v", item.ID, openMessageID, err)
		result.ToastType = "warning"
		result.Toast = "已批准当前计划，但更新审批卡片失败，请刷新群消息确认。"
	}
	b.reconcileCampaignRepoLocked(item.ID)
	return result, nil
}

func (b *connectorRuntimeBuilder) rejectCampaignPlanFromCard(
	ctx context.Context,
	item campaign.Campaign,
	openMessageID string,
) (connector.CardActionResult, error) {
	repo, err := campaignrepo.RejectPlan(item.CampaignRepoPath)
	if err != nil {
		return connector.CardActionResult{}, err
	}
	b.reconcileCampaignRepoLocked(item.ID)
	updatedItem, err := b.campaignStore.GetCampaign(item.ID)
	if err == nil {
		item = updatedItem
	}
	nextRound := repo.Campaign.Frontmatter.PlanRound
	result := connector.CardActionResult{
		ToastType: "success",
		Toast:     fmt.Sprintf("已不批准当前计划，Alice 已退回第 %d 轮规划。", nextRound),
	}
	if err := b.patchCampaignPlanDecisionCard(ctx, openMessageID, item, false, nextRound); err != nil {
		logging.Warnf("patch rejected campaign plan card failed campaign=%s message=%s: %v", item.ID, openMessageID, err)
		result.ToastType = "warning"
		result.Toast = fmt.Sprintf("已不批准当前计划，并已退回第 %d 轮规划，但更新审批卡片失败，请刷新群消息确认。", nextRound)
	}
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = item.ID
	}
	b.sendCampaignNotifications(item, []campaignrepo.ReconcileEvent{{
		Kind:       campaignrepo.EventPlanReviewVerdict,
		CampaignID: item.ID,
		PlanRound:  nextRound,
		Title:      "方案未批准，重新规划",
		Detail:     fmt.Sprintf("Campaign **%s** 当前方案未获人工批准，已回到第 %d 轮规划。", title, nextRound),
		Severity:   "warning",
	}})
	return result, nil
}

func (b *connectorRuntimeBuilder) patchCampaignPlanDecisionCard(
	ctx context.Context,
	openMessageID string,
	item campaign.Campaign,
	approved bool,
	nextRound int,
) error {
	if b == nil || b.sender == nil {
		return errors.New("sender 不可用，无法更新审批卡片")
	}
	openMessageID = strings.TrimSpace(openMessageID)
	if openMessageID == "" {
		return errors.New("卡片消息 ID 为空，无法更新审批卡片")
	}
	cardContent, err := buildCampaignPlanDecisionCard(item.Title, item.ID, approved, nextRound)
	if err != nil {
		return err
	}
	return b.sender.PatchCard(ctx, openMessageID, cardContent)
}

func preferredCardActionActorID(req connector.CardActionRequest) string {
	if id := strings.TrimSpace(req.ActorUserID); id != "" {
		return id
	}
	return strings.TrimSpace(req.ActorOpenID)
}

func canManageCampaignCardAction(item campaign.Campaign, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == item.Creator.PreferredID() || item.ManageMode == campaign.ManageModeScopeAll
}

func normalizeRepoPlanStatus(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "_")
	raw = strings.ReplaceAll(raw, " ", "_")
	return raw
}
