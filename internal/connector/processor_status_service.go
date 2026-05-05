package connector

import (
	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/statusview"
)

type builtinStatusService struct {
	query statusview.Service
	usage *processorStatusUsageProvider
	store *automation.Store
}

func newBuiltinStatusService(processor *Processor) *builtinStatusService {
	usage := &processorStatusUsageProvider{processor: processor}
	return &builtinStatusService{
		query: statusview.Service{
			Usage: usage,
		},
		usage: usage,
	}
}

func (s *builtinStatusService) SetStores(automationStore *automation.Store) {
	if s == nil {
		return
	}
	s.query.Automation = automationStore
	s.store = automationStore
}

func (s *builtinStatusService) SetIdentity(botID, botName string) {
	if s == nil || s.usage == nil {
		return
	}
	s.usage.SetIdentity(botID, botName)
}

func (s *builtinStatusService) SetUsageSources(sources []StatusUsageSource) {
	if s == nil || s.usage == nil {
		return
	}
	s.usage.SetSources(sources)
}

func (s *builtinStatusService) Query(job Job) statusview.Result {
	if s == nil {
		return statusview.Result{}
	}
	return s.query.Query(statusview.Request{
		ChatType:      job.ChatType,
		ReceiveIDType: job.ReceiveIDType,
		ReceiveID:     job.ReceiveID,
		SenderUserID:  job.SenderUserID,
		SenderOpenID:  job.SenderOpenID,
		SessionKey:    job.SessionKey,
		Limit:         20,
	})
}

func (s *builtinStatusService) IsAvailable() bool {
	return s != nil && s.query.Automation != nil
}

func (s *builtinStatusService) Identity() (string, string) {
	if s == nil || s.usage == nil {
		return "", ""
	}
	return s.usage.Identity()
}

func (s *builtinStatusService) GetGoal(scope automation.Scope) (automation.GoalTask, error) {
	if s == nil || s.store == nil {
		return automation.GoalTask{}, automation.ErrGoalNotFound
	}
	return s.store.GetGoal(scope)
}
