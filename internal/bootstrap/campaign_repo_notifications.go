package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/logging"
)

func (b *connectorRuntimeBuilder) sendCampaignNotifications(item campaign.Campaign, events []campaignrepo.ReconcileEvent) {
	if b == nil || b.sender == nil || len(events) == 0 {
		return
	}
	target, ok := automationTargetFromCampaign(item)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, event := range events {
		cardContent, err := buildCampaignEventCard(event)
		if err != nil {
			logging.Warnf("build campaign event card failed campaign=%s kind=%s: %v", item.ID, event.Kind, err)
			continue
		}
		if err := b.sender.SendCard(ctx, target.Route.ReceiveIDType, target.Route.ReceiveID, cardContent); err != nil {
			// Fallback to text
			text := fmt.Sprintf("**%s**\n%s", event.Title, event.Detail)
			if sendErr := b.sender.SendText(ctx, target.Route.ReceiveIDType, target.Route.ReceiveID, text); sendErr != nil {
				logging.Warnf("send campaign notification failed campaign=%s kind=%s: %v", item.ID, event.Kind, sendErr)
			}
		}
	}
}

func buildCampaignEventCard(event campaignrepo.ReconcileEvent) (string, error) {
	colorTemplate := severityToCardTemplate(event.Severity)
	title := strings.TrimSpace(event.Title)
	if title == "" {
		title = string(event.Kind)
	}
	detail := strings.TrimSpace(event.Detail)
	if detail == "" {
		detail = " "
	}
	elements := []any{
		campaignEventCardMarkdown(detail),
	}
	if taskID := strings.TrimSpace(event.TaskID); taskID != "" {
		elements = []any{
			campaignEventCardMarkdown(fmt.Sprintf("**任务**: %s", taskID)),
			campaignEventCardMarkdown(detail),
		}
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
