package statusview

import (
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/sessionkey"
)

const defaultLimit = 20

type AutomationTaskStore interface {
	ListTasks(scope automation.Scope, statusFilter string, limit int) ([]automation.Task, error)
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
	ScopeLabel string
	TotalUsage UsageStats
	BotUsages  []BotUsage
	Tasks      []automation.Task
	TaskError  error
	UsageError error
}

func (r Result) HasErrors() bool {
	return r.TaskError != nil || r.UsageError != nil
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
	return r.TotalUsage.HasUsage() || len(r.BotUsages) > 0 || len(r.Tasks) > 0
}

type Service struct {
	Automation AutomationTaskStore
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
		scopeID := strings.TrimSpace(req.SessionKey)
		if scopeID != "" {
			scopeID = sessionkey.WithoutMessage(scopeID)
		}
		if scopeID == "" {
			scopeID = strings.TrimSpace(req.ReceiveID)
		}
		if scopeID == "" {
			return automation.Scope{}, fmt.Errorf("missing scope id for group")
		}
		return automation.Scope{Kind: automation.ScopeKindChat, ID: scopeID}, nil
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
