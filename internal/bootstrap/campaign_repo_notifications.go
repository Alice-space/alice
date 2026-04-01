package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/logging"
)

func loadPreviousBlockedReasons(campaignRepoPath string) map[string]string {
	path := filepath.Join(strings.TrimSpace(campaignRepoPath), "reports", "live-report.md")
	if path == "" {
		return map[string]string{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	blocked := make(map[string]string)
	inBlockers := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "## Blockers":
			inBlockers = true
			continue
		case inBlockers && strings.HasPrefix(trimmed, "## "):
			return blocked
		case !inBlockers || !strings.HasPrefix(trimmed, "- `"):
			continue
		}
		taskID, reason, ok := parseBlockedReportLine(trimmed)
		if !ok {
			continue
		}
		blocked[taskID] = reason
	}
	return blocked
}

func parseBlockedReportLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- `") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "- `")
	end := strings.Index(line, "`")
	if end <= 0 {
		return "", "", false
	}
	taskID := strings.TrimSpace(line[:end])
	if taskID == "" || taskID == "-" {
		return "", "", false
	}
	reason := ""
	if idx := strings.Index(line, " | "); idx >= 0 {
		reason = strings.TrimSpace(line[idx+3:])
	}
	return taskID, reason, true
}

func newSummaryBlockedEvents(campaignID string, previous map[string]string, summary campaignrepo.Summary) []campaignrepo.ReconcileEvent {
	if len(summary.BlockedTasks) == 0 {
		return nil
	}
	var events []campaignrepo.ReconcileEvent
	for _, task := range summary.BlockedTasks {
		if !shouldNotifySummaryBlockedTask(task) {
			continue
		}
		reason := strings.TrimSpace(task.BlockedReason)
		if reason == "" {
			continue
		}
		if strings.TrimSpace(previous[task.TaskID]) == reason {
			continue
		}
		title := "任务阻塞"
		if prevReason := strings.TrimSpace(previous[task.TaskID]); prevReason != "" && prevReason != reason {
			title = "任务阻塞更新"
		}
		detail := fmt.Sprintf("任务 **%s** %s 当前被 runtime gate 挡住，尚未进入下一步。\n\n**原因**: %s", task.TaskID, strings.TrimSpace(task.Title), reason)
		events = append(events, campaignrepo.ReconcileEvent{
			Kind:       campaignrepo.EventTaskBlocked,
			CampaignID: campaignID,
			TaskID:     task.TaskID,
			Title:      title,
			Detail:     detail,
			Severity:   "warning",
		})
	}
	return events
}

func shouldNotifySummaryBlockedTask(task campaignrepo.TaskSummary) bool {
	switch task.Status {
	case campaignrepo.TaskStatusExecuting, campaignrepo.TaskStatusReviewing, campaignrepo.TaskStatusReviewPending, campaignrepo.TaskStatusBlocked:
		return true
	default:
		return false
	}
}

func (b *connectorRuntimeBuilder) sendCampaignNotifications(item campaign.Campaign, events []campaignrepo.ReconcileEvent) {
	if b == nil || b.sender == nil || len(events) == 0 {
		return
	}
	target, ok := automationTargetFromCampaign(item)
	if !ok {
		return
	}
	timeout := b.cfg.CampaignNotificationTimeout
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultCampaignNotificationTimeoutSecs) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	urgentRecipients := campaignUrgentRecipientOpenIDs(item)
	for _, event := range events {
		title := campaignEventCardTitle(item.Title, item.ID, event)
		cardContent, err := buildCampaignEventCard(title, event)
		if err != nil {
			logging.Warnf("build campaign event card failed campaign=%s kind=%s: %v", item.ID, event.Kind, err)
			continue
		}
		messageID, sendErr := b.sender.SendCardMessage(ctx, target.Route.ReceiveIDType, target.Route.ReceiveID, cardContent)
		if sendErr != nil {
			// Fallback to text
			text := fmt.Sprintf("**%s**\n%s", title, event.Detail)
			messageID, sendErr = b.sender.SendTextMessage(ctx, target.Route.ReceiveIDType, target.Route.ReceiveID, text)
			if sendErr != nil {
				logging.Warnf("send campaign notification failed campaign=%s kind=%s: %v", item.ID, event.Kind, sendErr)
				if shouldEscalateCampaignEvent(event) {
					b.sendCampaignUrgentDirectMessage(ctx, item, target.Route.ReceiveIDType, target.Route.ReceiveID, event, title, urgentRecipients)
				}
				continue
			}
		}
		if shouldEscalateCampaignEvent(event) {
			if target.Route.ReceiveIDType == "chat_id" && messageID != "" && len(urgentRecipients) > 0 {
				if err := b.sender.UrgentApp(ctx, messageID, "open_id", urgentRecipients); err != nil {
					logging.Warnf("send urgent campaign notification failed campaign=%s kind=%s message=%s: %v", item.ID, event.Kind, messageID, err)
				}
			}
			b.sendCampaignUrgentDirectMessage(ctx, item, target.Route.ReceiveIDType, target.Route.ReceiveID, event, title, urgentRecipients)
		}
	}
}

func shouldEscalateCampaignEvent(event campaignrepo.ReconcileEvent) bool {
	switch event.Kind {
	case campaignrepo.EventTaskBlocked:
		return true
	default:
		return false
	}
}

func campaignUrgentRecipientOpenIDs(item campaign.Campaign) []string {
	creatorOpenID := strings.TrimSpace(item.Creator.OpenID)
	if creatorOpenID == "" {
		return nil
	}
	return []string{creatorOpenID}
}

func buildCampaignUrgentDirectText(campaignTitle, campaignID string, event campaignrepo.ReconcileEvent) string {
	title := campaignEventCardTitle(campaignTitle, campaignID, event)
	detail := strings.TrimSpace(strings.NewReplacer("**", "", "`", "").Replace(event.Detail))
	if detail == "" {
		return fmt.Sprintf("【Alice加急提醒】\n%s", title)
	}
	return fmt.Sprintf("【Alice加急提醒】\n%s\n\n%s", title, detail)
}

func (b *connectorRuntimeBuilder) sendCampaignUrgentDirectMessage(
	ctx context.Context,
	item campaign.Campaign,
	primaryReceiveIDType, primaryReceiveID string,
	event campaignrepo.ReconcileEvent,
	title string,
	urgentRecipients []string,
) {
	if b == nil || b.sender == nil || len(urgentRecipients) == 0 {
		return
	}
	text := buildCampaignUrgentDirectText(item.Title, item.ID, event)
	for _, openID := range urgentRecipients {
		recipient := strings.TrimSpace(openID)
		if recipient == "" {
			continue
		}
		if primaryReceiveIDType == "open_id" && strings.TrimSpace(primaryReceiveID) == recipient {
			continue
		}
		if err := b.sender.SendText(ctx, "open_id", recipient, text); err != nil {
			logging.Warnf("send urgent direct campaign notification failed campaign=%s kind=%s user=%s title=%s: %v", item.ID, event.Kind, recipient, title, err)
		}
	}
}

func buildCampaignEventCard(title string, event campaignrepo.ReconcileEvent) (string, error) {
	colorTemplate := severityToCardTemplate(event.Severity)
	title = strings.TrimSpace(title)
	detail := strings.TrimSpace(event.Detail)
	if detail == "" {
		detail = " "
	}
	blocks := []string{detail}
	if taskID := strings.TrimSpace(event.TaskID); taskID != "" {
		blocks = []string{
			fmt.Sprintf("**任务**: %s", taskID),
			detail,
		}
	}
	if action := campaignEventActionElement(event); action != nil {
		return buildLegacyCampaignEventCard(title, colorTemplate, blocks, action)
	}
	elements := make([]any, 0, len(blocks))
	for _, block := range blocks {
		elements = append(elements, campaignEventCardMarkdown(block))
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
			"template": colorTemplate,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal campaign event card failed: %w", err)
	}
	return string(raw), nil
}

func buildLegacyCampaignEventCard(title, colorTemplate string, blocks []string, action map[string]any) (string, error) {
	elements := make([]any, 0, len(blocks)+1)
	for _, block := range blocks {
		elements = append(elements, campaignEventLegacyMarkdown(block))
	}
	if action != nil {
		elements = append(elements, action)
	}
	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"enable_forward":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
			"template": colorTemplate,
		},
		"elements": elements,
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal legacy campaign event card failed: %w", err)
	}
	return string(raw), nil
}

func buildCampaignPlanDecisionCard(campaignTitle, campaignID string, approved bool, nextRound int) (string, error) {
	title := "方案已批准"
	detail := fmt.Sprintf("Campaign **%s** 当前方案已获人工批准，Alice 将继续派发执行任务。", campaignDisplayTitle(campaignTitle, campaignID))
	status := "**审批结果**：已批准"
	colorTemplate := "green"
	if !approved {
		title = "方案未批准"
		detail = fmt.Sprintf("Campaign **%s** 当前方案未获人工批准，已回到第 %d 轮规划。", campaignDisplayTitle(campaignTitle, campaignID), nextRound)
		status = "**审批结果**：已拒绝"
		colorTemplate = "orange"
	}
	return buildLegacyCampaignEventCard(
		campaignEventCardTitle(campaignTitle, campaignID, campaignrepo.ReconcileEvent{Title: title}),
		colorTemplate,
		[]string{detail, status},
		nil,
	)
}

func campaignEventCardTitle(campaignTitle, campaignID string, event campaignrepo.ReconcileEvent) string {
	base := strings.TrimSpace(event.Title)
	if base == "" {
		base = string(event.Kind)
	}
	campaignLabel := campaignDisplayTitle(campaignTitle, campaignID)
	if campaignLabel == "" {
		return base
	}
	return fmt.Sprintf("%s · %s", campaignLabel, base)
}

func campaignDisplayTitle(campaignTitle, campaignID string) string {
	campaignLabel := strings.TrimSpace(campaignTitle)
	if campaignLabel == "" {
		campaignLabel = strings.TrimSpace(campaignID)
	}
	return campaignLabel
}

func severityToCardTemplate(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "success":
		return "green"
	case "warning":
		return "orange"
	case "error":
		return "red"
	default:
		return "blue"
	}
}

func campaignEventCardMarkdown(content string) map[string]any {
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func campaignEventLegacyMarkdown(content string) map[string]any {
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "lark_md",
			"content": content,
		},
	}
}

func campaignEventActionElement(event campaignrepo.ReconcileEvent) map[string]any {
	if event.Kind != campaignrepo.EventHumanApprovalNeeded || strings.TrimSpace(event.CampaignID) == "" {
		return nil
	}
	value := map[string]any{
		"alice_action": connector.CardActionKindCampaignPlanApproval,
		"campaign_id":  strings.TrimSpace(event.CampaignID),
		"plan_round":   event.PlanRound,
	}
	approveValue := cloneCardActionValue(value)
	approveValue["decision"] = connector.CardActionDecisionApprove
	rejectValue := cloneCardActionValue(value)
	rejectValue["decision"] = connector.CardActionDecisionReject
	return map[string]any{
		"tag":    "action",
		"layout": "bisected",
		"actions": []any{
			campaignEventActionButton("批准", "primary", approveValue,
				"确认批准当前计划？", "批准后会把计划切到执行阶段，并继续派发任务。"),
			campaignEventActionButton("不批准", "danger", rejectValue,
				"确认不批准当前计划？", "不批准后会退回 planning，并进入下一轮规划。"),
		},
	}
}

func campaignEventActionButton(label, buttonType string, value map[string]any, confirmTitle, confirmText string) map[string]any {
	button := map[string]any{
		"tag":   "button",
		"type":  strings.TrimSpace(buttonType),
		"text":  campaignEventPlainText(strings.TrimSpace(label)),
		"value": value,
	}
	if strings.TrimSpace(confirmTitle) != "" || strings.TrimSpace(confirmText) != "" {
		button["confirm"] = map[string]any{
			"title": campaignEventPlainText(strings.TrimSpace(confirmTitle)),
			"text":  campaignEventPlainText(strings.TrimSpace(confirmText)),
		}
	}
	return button
}

func campaignEventPlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

func cloneCardActionValue(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
