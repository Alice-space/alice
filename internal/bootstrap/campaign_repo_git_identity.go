package bootstrap

import (
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
)

func campaignBlockedEventSeverity(reason string) string {
	if campaignrepo.LooksLikeGitIdentityProblem(reason) {
		return "error"
	}
	return "warning"
}

func campaignRepoGitIdentityBlockedSummary(campaignRepoPath string) string {
	campaignRepoPath = strings.TrimSpace(campaignRepoPath)
	if campaignRepoPath == "" {
		return "Blocked: campaign repo git identity missing"
	}
	return fmt.Sprintf("Blocked: campaign repo git identity missing (%s)", campaignRepoPath)
}

func newCampaignRepoGitIdentityFailureEvent(item campaign.Campaign, err error) (campaignrepo.ReconcileEvent, string, bool) {
	reason := strings.TrimSpace(errorString(err))
	if !campaignrepo.LooksLikeGitIdentityProblem(reason) {
		return campaignrepo.ReconcileEvent{}, "", false
	}
	repoPath := strings.TrimSpace(item.CampaignRepoPath)
	detail := fmt.Sprintf(
		"Campaign repo 自动提交需要可用 git 身份，Alice 不会再代写作者。\n\n**repo**: `%s`\n\n**问题**: %s\n\n**修复**: 在该 repo 的 local 或 global 配置 `user.name` 和 `user.email`，然后重新执行 `repo-reconcile`。",
		displayOrDash(repoPath),
		displayOrDash(reason),
	)
	return campaignrepo.ReconcileEvent{
			Kind:       campaignrepo.EventAutomationFailed,
			CampaignID: item.ID,
			Title:      "缺少 Git 作者配置，自动提交已阻塞",
			Detail:     detail,
			Severity:   "error",
		},
		campaignRepoGitIdentityBlockedSummary(repoPath),
		true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func displayOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
