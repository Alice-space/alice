package statusview

import (
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
)

const defaultLimit = 20

type AutomationTaskStore interface {
	ListTasks(scope automation.Scope, statusFilter string, limit int) ([]automation.Task, error)
}

type CampaignStore interface {
	ListCampaigns(visibilityKey, statusFilter string, limit int) ([]campaign.Campaign, error)
}

type UsageProvider interface {
	UsageForScope(scopeKey string) (UsageStats, []BotUsage, error)
}

type Request struct {
	ChatType      string
	ReceiveIDType string
	ReceiveID     string
	SenderUserID  string
	SenderOpenID  string
	SessionKey    string
	Limit         int
}

type Result struct {
	ScopeLabel    string
	TotalUsage    UsageStats
	BotUsages     []BotUsage
	Tasks         []automation.Task
	Campaigns     []campaign.Campaign
	TaskError     error
	CampaignError error
	UsageError    error
}

type Service struct {
	Automation AutomationTaskStore
	Campaigns  CampaignStore
	Usage      UsageProvider
}

func (s Service) Query(req Request) Result {
	limit := req.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	result := Result{
		ScopeLabel: VisibilityKey(req),
	}
	if result.ScopeLabel == "" && strings.TrimSpace(req.ReceiveIDType) != "" && strings.TrimSpace(req.ReceiveID) != "" {
		result.ScopeLabel = strings.TrimSpace(req.ReceiveIDType) + ":" + strings.TrimSpace(req.ReceiveID)
	}
	if s.Usage != nil {
		scopeKey := VisibilityKey(req)
		result.TotalUsage, result.BotUsages, result.UsageError = s.Usage.UsageForScope(scopeKey)
	}
	if s.Automation != nil {
		scope, err := AutomationScope(req)
		if err != nil {
			result.TaskError = err
		} else {
			result.Tasks, result.TaskError = s.Automation.ListTasks(scope, string(automation.TaskStatusActive), limit)
		}
	}
	if s.Campaigns != nil {
		visibilityKey := VisibilityKey(req)
		if visibilityKey == "" {
			result.CampaignError = fmt.Errorf("missing scope session key")
		} else {
			items, err := s.Campaigns.ListCampaigns(visibilityKey, "", limit)
			if err != nil {
				result.CampaignError = err
			} else {
				result.Campaigns = filterActiveCampaigns(items)
			}
		}
	}
	return result
}

func VisibilityKey(req Request) string {
	receiveIDType := strings.TrimSpace(req.ReceiveIDType)
	receiveID := strings.TrimSpace(req.ReceiveID)
	if receiveIDType != "" && receiveID != "" {
		return receiveIDType + ":" + receiveID
	}
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, "|"); idx >= 0 {
		return strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}

func AutomationScope(req Request) (automation.Scope, error) {
	chatType := strings.ToLower(strings.TrimSpace(req.ChatType))
	if chatType == "group" || chatType == "topic_group" {
		receiveID := strings.TrimSpace(req.ReceiveID)
		if receiveID == "" {
			return automation.Scope{}, fmt.Errorf("missing chat_id for group scope")
		}
		return automation.Scope{Kind: automation.ScopeKindChat, ID: receiveID}, nil
	}
	actorID := strings.TrimSpace(req.SenderUserID)
	if actorID == "" {
		actorID = strings.TrimSpace(req.SenderOpenID)
	}
	if actorID == "" {
		return automation.Scope{}, fmt.Errorf("missing actor id")
	}
	return automation.Scope{Kind: automation.ScopeKindUser, ID: actorID}, nil
}

func filterActiveCampaigns(items []campaign.Campaign) []campaign.Campaign {
	filtered := make([]campaign.Campaign, 0, len(items))
	for _, item := range items {
		switch item.Status {
		case campaign.StatusPlanned, campaign.StatusRunning, campaign.StatusHold:
			filtered = append(filtered, item)
		}
	}
	return filtered
}
