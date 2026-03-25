package runtimeapi

import (
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/runtimecfg"
)

func newAutomationRuntimeConfig(cfg config.Config) automationRuntimeConfig {
	return automationRuntimeConfig{
		llmProvider: cfg.LLMProvider,
		llmProfiles: runtimecfg.CloneLLMProfiles(cfg.LLMProfiles),
		groupScenes: cfg.GroupScenes,
		permissions: cfg.Permissions,
	}
}

func (s *Server) UpdateRuntimeConfig(cfg config.Config) {
	if s == nil {
		return
	}
	s.runtimeMu.Lock()
	s.runtime = newAutomationRuntimeConfig(cfg)
	s.runtimeMu.Unlock()
}

func (s *Server) runtimeConfig() automationRuntimeConfig {
	if s == nil {
		return automationRuntimeConfig{}
	}
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return automationRuntimeConfig{
		llmProvider: s.runtime.llmProvider,
		llmProfiles: runtimecfg.CloneLLMProfiles(s.runtime.llmProfiles),
		groupScenes: s.runtime.groupScenes,
		permissions: s.runtime.permissions,
	}
}

func (s *Server) allowRuntimeMessage() bool {
	return runtimePermissionEnabled(s.runtimeConfig().permissions.RuntimeMessage)
}

func (s *Server) allowRuntimeAutomation() bool {
	return runtimePermissionEnabled(s.runtimeConfig().permissions.RuntimeAutomation)
}

func (s *Server) allowRuntimeCampaigns() bool {
	return runtimePermissionEnabled(s.runtimeConfig().permissions.RuntimeCampaigns)
}

func runtimePermissionEnabled(flag *bool) bool {
	return flag == nil || *flag
}

func (s *Server) prefersThreadReply(sessionKey, chatType string) bool {
	return runtimecfg.ThreadReplyPreferred(s.runtimeConfig().groupScenes, sessionKey, chatType)
}
