package bootstrap

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/logging"
)

var genericCampaignReconcilePattern = regexp.MustCompile(`(?i)/alice\s+reconcile\s+campaign\s+([A-Za-z0-9_-]+)`)

func (b *connectorRuntimeBuilder) guardCampaignRepoWorkflowTask(_ context.Context, task automation.Task) (automation.WorkflowPreflightDecision, error) {
	task = automation.NormalizeTask(task)
	if task.Action.Type != automation.ActionTypeRunWorkflow || strings.TrimSpace(task.Action.Workflow) != "code_army" {
		return automation.WorkflowPreflightDecision{}, nil
	}

	stateKey := strings.TrimSpace(task.Action.StateKey)
	if strings.HasPrefix(stateKey, campaignDispatchStatePrefix) || strings.HasPrefix(stateKey, campaignWakeStatePrefix) {
		return automation.WorkflowPreflightDecision{}, nil
	}

	campaignID, ok := campaignIDFromGenericReconcilePrompt(task.Action.Prompt)
	if !ok || b == nil || b.campaignStore == nil {
		return automation.WorkflowPreflightDecision{}, nil
	}

	item, err := b.campaignStore.GetCampaign(campaignID)
	if err != nil {
		return automation.WorkflowPreflightDecision{}, nil
	}
	item = campaign.NormalizeCampaign(item)
	if strings.TrimSpace(item.CampaignRepoPath) == "" {
		return automation.WorkflowPreflightDecision{}, nil
	}

	repo, err := campaignrepo.Load(item.CampaignRepoPath)
	if err != nil {
		logging.Warnf("campaign repo workflow guard load failed campaign=%s path=%s: %v", item.ID, item.CampaignRepoPath, err)
		return automation.WorkflowPreflightDecision{}, nil
	}

	planStatus := normalizeCampaignWorkflowPlanStatus(repo.Campaign.Frontmatter.PlanStatus)
	if !isPlanGateBlockingStatus(planStatus) {
		return automation.WorkflowPreflightDecision{}, nil
	}

	reason := fmt.Sprintf(
		"campaign `%s` 仍处于规划门禁阶段（plan_status=%s）；generic code_army reconcile worker 已暂停。请先运行 `alice-code-army.sh repo-reconcile %s` 或使用 `alice-code-army.sh bootstrap ...` 触发正式 planner dispatch，并等待 plan review / human approval。",
		campaignID,
		planStatus,
		campaignID,
	)
	return automation.WorkflowPreflightDecision{
		Block:         true,
		Message:       reason,
		SignalKind:    "needs_human",
		SignalMessage: reason,
		ForceCard:     true,
	}, nil
}

func campaignIDFromGenericReconcilePrompt(prompt string) (string, bool) {
	matches := genericCampaignReconcilePattern.FindStringSubmatch(strings.TrimSpace(prompt))
	if len(matches) < 2 {
		return "", false
	}
	campaignID := strings.TrimSpace(matches[1])
	if campaignID == "" {
		return "", false
	}
	return campaignID, true
}

func normalizeCampaignWorkflowPlanStatus(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	if value == "" {
		return "idle"
	}
	return value
}

func isPlanGateBlockingStatus(planStatus string) bool {
	switch normalizeCampaignWorkflowPlanStatus(planStatus) {
	case "idle", "planning", "plan_review_pending", "plan_reviewing", "plan_approved":
		return true
	default:
		return false
	}
}
