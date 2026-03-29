package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/logging"
)

func (b *connectorRuntimeBuilder) handleCampaignRepoTaskSignals(campaignID string, task automation.Task) {
	task = automation.NormalizeTask(task)
	signalKind := strings.ToLower(strings.TrimSpace(task.LastSignalKind))
	if signalKind == "" {
		return
	}
	signalMessage := strings.TrimSpace(task.LastSignalMessage)

	item, err := b.campaignStore.GetCampaign(campaignID)
	if err != nil {
		logging.Warnf("load campaign for signal handling failed campaign=%s: %v", campaignID, err)
		return
	}
	item = campaign.NormalizeCampaign(item)
	repoPath := strings.TrimSpace(item.CampaignRepoPath)

	switch signalKind {
	case "replan":
		b.handleReplanSignal(item, repoPath, task, signalMessage)
	case "blocked":
		b.handleBlockedSignal(item, repoPath, task, signalMessage)
	case "discovery":
		b.handleDiscoverySignal(item, repoPath, task, signalMessage)
	}
}

func (b *connectorRuntimeBuilder) handleReplanSignal(item campaign.Campaign, repoPath string, task automation.Task, reason string) {
	taskID := extractTaskIDFromStateKey(task.Action.StateKey)
	if reason == "" {
		reason = "executor requested replanning"
	}
	// Write to findings.md so planner can read the replan reason
	if repoPath != "" {
		findingsPath := filepath.Join(repoPath, "findings.md")
		entry := fmt.Sprintf("\n## Replan Request (%s)\n\n**Task**: %s\n\n%s\n",
			time.Now().Local().Format("2006-01-02 15:04:05"), taskID, reason)
		if err := appendToFile(findingsPath, entry); err != nil {
			logging.Warnf("append findings failed campaign=%s: %v", item.ID, err)
		}
		// Mark the current task as blocked in the campaign repo
		if taskID != "" {
			if err := campaignrepo.MarkTaskBlocked(repoPath, taskID, "replan requested: "+reason); err != nil {
				logging.Warnf("mark task blocked for replan failed campaign=%s task=%s: %v", item.ID, taskID, err)
			}
		}
		// Reset plan_status to planning with incremented plan_round
		if err := campaignrepo.ResetPlanForReplan(repoPath); err != nil {
			logging.Warnf("reset plan for replan failed campaign=%s: %v", item.ID, err)
		}
	}
	// Send notification
	b.sendCampaignSignalNotification(item, campaignrepo.ReconcileEvent{
		Kind:       campaignrepo.EventReplanRequested,
		CampaignID: item.ID,
		TaskID:     taskID,
		Title:      "请求重新规划",
		Detail:     fmt.Sprintf("任务 **%s** 发现新情况，请求重新规划。\n\n**原因**: %s", taskID, reason),
		Severity:   "error",
	})
}

func (b *connectorRuntimeBuilder) handleBlockedSignal(item campaign.Campaign, repoPath string, task automation.Task, reason string) {
	taskID := extractTaskIDFromStateKey(task.Action.StateKey)
	if reason == "" {
		reason = "executor reported task is blocked"
	}
	title := "任务阻塞"
	detail := fmt.Sprintf("任务 **%s** 遇到阻塞，无法继续执行。\n\n**原因**: %s", taskID, reason)
	if repoPath != "" && taskID != "" {
		outcome, err := campaignrepo.HandleTaskBlocked(repoPath, taskID, reason)
		if err != nil {
			logging.Warnf("handle task blocked failed campaign=%s task=%s: %v", item.ID, taskID, err)
		} else if outcome.GuidanceRequested {
			title = "任务遇到阻塞，转评审指导"
			detail = fmt.Sprintf("任务 **%s** 遇到阻塞，已转 reviewer 指导（第 %d/%d 次）。\n\n**原因**: %s", taskID, outcome.GuidanceAttempt, 3, outcome.Reason)
		} else if outcome.TerminalBlocked {
			title = "任务阻塞"
			detail = fmt.Sprintf("任务 **%s** 多次阻塞后仍未恢复，已进入真正阻塞状态。\n\n**原因**: %s", taskID, outcome.Reason)
		}
	}
	b.sendCampaignSignalNotification(item, campaignrepo.ReconcileEvent{
		Kind:       campaignrepo.EventTaskBlocked,
		CampaignID: item.ID,
		TaskID:     taskID,
		Title:      title,
		Detail:     detail,
		Severity:   "warning",
	})
}

func (b *connectorRuntimeBuilder) handleDiscoverySignal(item campaign.Campaign, repoPath string, task automation.Task, finding string) {
	taskID := extractTaskIDFromStateKey(task.Action.StateKey)
	if finding == "" {
		finding = "executor reported a new discovery"
	}
	// Append to findings.md
	if repoPath != "" {
		findingsPath := filepath.Join(repoPath, "findings.md")
		entry := fmt.Sprintf("\n## Discovery (%s)\n\n**Task**: %s\n\n%s\n",
			time.Now().Local().Format("2006-01-02 15:04:05"), taskID, finding)
		if err := appendToFile(findingsPath, entry); err != nil {
			logging.Warnf("append discovery to findings failed campaign=%s: %v", item.ID, err)
		}
	}
	// Send notification
	b.sendCampaignSignalNotification(item, campaignrepo.ReconcileEvent{
		Kind:       campaignrepo.EventDiscoveryReported,
		CampaignID: item.ID,
		TaskID:     taskID,
		Title:      "新发现",
		Detail:     fmt.Sprintf("任务 **%s** 报告了新发现。\n\n**发现**: %s", taskID, finding),
		Severity:   "info",
	})
}

func (b *connectorRuntimeBuilder) sendCampaignSignalNotification(item campaign.Campaign, event campaignrepo.ReconcileEvent) {
	b.sendCampaignNotifications(item, []campaignrepo.ReconcileEvent{event})
}

func extractTaskIDFromStateKey(stateKey string) string {
	parts := strings.Split(strings.TrimSpace(stateKey), ":")
	if len(parts) >= 4 && parts[0] == strings.TrimSuffix(campaignDispatchStatePrefix, ":") {
		return parts[3]
	}
	if len(parts) >= 3 && parts[0] == strings.TrimSuffix(campaignWakeStatePrefix, ":") {
		return parts[2]
	}
	return ""
}

func appendToFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open file failed: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
