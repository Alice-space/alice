package connector

import (
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/config"
)

func (a *App) routeIncomingJob(job *Job, event *larkim.P2MessageReceiveV1) bool {
	if job == nil {
		return false
	}
	var message *larkim.EventMessage
	if event != nil && event.Event != nil {
		message = event.Event.Message
	}
	if !isGroupChatType(job.ChatType) || isBuiltinCommandText(job.Text) {
		if message != nil {
			a.resolveJobSessionKey(job, message)
		}
		return true
	}

	if a.routeGroupSceneJob(job, event, message) {
		return true
	}

	accepted := shouldProcessIncomingMessage(
		event,
		a.runtimeConfig().triggerMode,
		a.runtimeConfig().triggerPrefix,
		a.runtimeConfig().feishuBotOpenID,
		a.runtimeConfig().feishuBotUserID,
	)
	if !accepted {
		return false
	}
	normalizeIncomingGroupJobTextForTriggerMode(job, a.runtimeConfig().triggerMode, a.runtimeConfig().triggerPrefix)
	if message != nil {
		a.resolveJobSessionKey(job, message)
	}
	return true
}

func (a *App) routeGroupSceneJob(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) bool {
	cfg := a.runtimeConfig()
	if !cfg.groupScenes.Chat.Enabled && !cfg.groupScenes.Work.Enabled {
		return false
	}

	if sessionKey := a.resolveExistingWorkSession(job, event, message); sessionKey != "" {
		applyWorkSceneToJob(job, cfg, sessionKey)
		normalizeIncomingGroupJobTextForTriggerMode(job, cfg.triggerMode, cfg.triggerPrefix)
		if a.processor != nil && message != nil {
			a.processor.rememberSessionAliases(sessionKey, buildSessionKeyCandidatesForMessage(job.ReceiveIDType, job.ReceiveID, message)...)
		}
		return true
	}

	if cfg.groupScenes.Work.Enabled &&
		shouldProcessIncomingMessage(
			event,
			cfg.triggerMode,
			cfg.triggerPrefix,
			cfg.feishuBotOpenID,
			cfg.feishuBotUserID,
		) &&
		hasSceneTriggerTag(job.Text, cfg.groupScenes.Work.TriggerTag) {
		sessionKey := buildWorkSceneSessionKey(job.ReceiveIDType, job.ReceiveID, job.SourceMessageID)
		applyWorkSceneToJob(job, cfg, sessionKey)
		normalizeIncomingGroupJobTextForTriggerMode(job, cfg.triggerMode, cfg.triggerPrefix)
		job.Text = trimSceneTriggerTag(job.Text, cfg.groupScenes.Work.TriggerTag)
		if a.processor != nil && message != nil {
			a.processor.rememberSessionAliases(sessionKey, buildSessionKeyCandidatesForMessage(job.ReceiveIDType, job.ReceiveID, message)...)
		}
		return true
	}

	if cfg.groupScenes.Chat.Enabled {
		applyChatSceneToJob(job, cfg)
		return true
	}
	return false
}

func (a *App) resolveExistingWorkSession(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) string {
	if job == nil || message == nil {
		return ""
	}
	sessionKey := strings.TrimSpace(a.findExistingSessionKey(buildSessionKeyCandidatesForMessage(job.ReceiveIDType, job.ReceiveID, message)))
	if !isWorkSceneSessionKey(sessionKey) {
		return ""
	}
	return sessionKey
}

func applyChatSceneToJob(job *Job, cfg appRuntimeConfig) {
	if job == nil {
		return
	}
	job.Scene = jobSceneChat
	job.ResponseMode = jobResponseModeReply
	job.DisableAck = true
	job.SessionKey = buildChatSceneSessionKey(job.ReceiveIDType, job.ReceiveID)
	job.ResourceScopeKey = buildChatSceneResourceScopeKey(job.ReceiveIDType, job.ReceiveID)
	job.CreateFeishuThread = cfg.groupScenes.Chat.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(cfg.groupScenes.Chat.NoReplyToken)
	applyLLMProfileToJob(job, cfg.llmProvider, cfg.llmProfiles[cfg.groupScenes.Chat.LLMProfile])
}

func applyWorkSceneToJob(job *Job, cfg appRuntimeConfig, sessionKey string) {
	if job == nil {
		return
	}
	job.Scene = jobSceneWork
	job.ResponseMode = jobResponseModeReply
	job.DisableAck = false
	job.SessionKey = strings.TrimSpace(sessionKey)
	job.ResourceScopeKey = buildWorkSceneResourceScopeKeyFromSessionKey(sessionKey)
	job.CreateFeishuThread = cfg.groupScenes.Work.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(cfg.groupScenes.Work.NoReplyToken)
	applyLLMProfileToJob(job, cfg.llmProvider, cfg.llmProfiles[cfg.groupScenes.Work.LLMProfile])
}

func applyLLMProfileToJob(job *Job, defaultProvider string, profile config.LLMProfileConfig) {
	if job == nil {
		return
	}
	_ = defaultProvider
	job.LLMModel = strings.TrimSpace(profile.Model)
	job.LLMProfile = strings.TrimSpace(profile.Profile)
	job.LLMReasoningEffort = strings.TrimSpace(profile.ReasoningEffort)
	job.LLMPersonality = strings.TrimSpace(profile.Personality)
}

func buildChatSceneSessionKey(receiveIDType, receiveID string) string {
	base := buildSessionKey(receiveIDType, receiveID)
	if base == "" {
		return ""
	}
	return base + "|scene:" + jobSceneChat
}

func buildChatSceneResourceScopeKey(receiveIDType, receiveID string) string {
	return buildChatSceneSessionKey(receiveIDType, receiveID)
}

func buildWorkSceneSessionKey(receiveIDType, receiveID, sourceMessageID string) string {
	base := buildSessionKey(receiveIDType, receiveID)
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if base == "" || sourceMessageID == "" {
		return ""
	}
	return base + "|scene:" + jobSceneWork + "|seed:" + sourceMessageID
}

func buildWorkSceneResourceScopeKeyFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	return strings.Replace(sessionKey, "|seed:", "|thread:", 1)
}

func isWorkSceneSessionKey(sessionKey string) bool {
	return strings.Contains(strings.TrimSpace(sessionKey), "|scene:"+jobSceneWork)
}

func hasSceneTriggerTag(text, tag string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	tag = strings.ToLower(strings.TrimSpace(tag))
	if text == "" || tag == "" {
		return false
	}
	return strings.Contains(text, tag)
}

func trimSceneTriggerTag(text, tag string) string {
	text = strings.TrimSpace(text)
	tag = strings.TrimSpace(tag)
	if text == "" || tag == "" {
		return text
	}
	normalizedText := strings.ToLower(text)
	normalizedTag := strings.ToLower(tag)
	if idx := strings.Index(normalizedText, normalizedTag); idx >= 0 {
		text = strings.TrimSpace(text[:idx] + text[idx+len(tag):])
	}
	return strings.TrimSpace(text)
}
