package statusview

import (
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/sessionkey"
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

func (r Result) HasErrors() bool {
	return r.TaskError != nil || r.CampaignError != nil || r.UsageError != nil
}

func (r Result) IsSuccess() bool {
	return !r.HasErrors()
}

func (r Result) IsPartialSuccess() bool {
	return r.HasErrors() && r.hasAnyData()
}

func (r Result) IsFailure() bool {
	return r.HasErrors() && !r.hasAnyData()
}

func (r Result) hasAnyData() bool {
	return r.TotalUsage.HasUsage() || len(r.BotUsages) > 0 || len(r.Tasks) > 0 || len(r.Campaigns) > 0
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
	if result.ScopeLabel == "" {
		result.ScopeLabel = sessionkey.Build(req.ReceiveIDType, req.ReceiveID)
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
	if key := sessionkey.Build(req.ReceiveIDType, req.ReceiveID); key != "" {
		return key
	}
	return sessionkey.VisibilityKey(req.SessionKey)
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
