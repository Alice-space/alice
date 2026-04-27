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
	cfg := a.runtimeConfig()
	var message *larkim.EventMessage
	if event != nil && event.Event != nil {
		message = event.Event.Message
	}
	if !isGroupChatType(job.ChatType) {
		if message != nil {
			a.resolveJobSessionKey(job, message)
		}
		return true
	}
	if isBuiltinCommandText(job.Text) {
		contextual := isContextualBuiltinCommand(job.Text)
		if !contextual {
			if message != nil {
				a.resolveJobSessionKey(job, message)
			}
			return true
		}
		if cfg.groupScenes.Work.Enabled {
			if sessionKey := a.resolveExistingWorkSession(job, event, message); sessionKey != "" {
				applyWorkSceneToJob(job, cfg, sessionKey)
				if a.processor != nil && message != nil {
					a.processor.setWorkThreadID(sessionKey, strings.TrimSpace(deref(message.ThreadId)))
				}
				return true
			}
			if activeKey := a.findActiveWorkSessionKey(job.ReceiveIDType, job.ReceiveID); activeKey != "" {
				applyWorkSceneToJob(job, cfg, activeKey)
				return true
			}
		}
		if cfg.groupScenes.Chat.Enabled {
			sessionKey := a.resolveCurrentChatSceneSessionKey(job.ReceiveIDType, job.ReceiveID)
			if a.processor != nil && a.processor.hasActiveSession(sessionKey) {
				applyChatSceneToJob(job, cfg, sessionKey)
				return true
			}
		}
		if !isGroupMessageTriggered(event, cfg.triggerMode, cfg.triggerPrefix, a.getBotOpenID(), "") {
			return false
		}
		if cfg.groupScenes.Work.Enabled {
			if activeKey := a.findActiveWorkSessionKey(job.ReceiveIDType, job.ReceiveID); activeKey != "" {
				applyWorkSceneToJob(job, cfg, activeKey)
				return true
			}
		}
		if message != nil {
			a.resolveJobSessionKey(job, message)
		}
		return true
	}

	if cfg.groupScenes.Chat.Enabled || cfg.groupScenes.Work.Enabled {
		return a.routeGroupSceneJob(job, event, message)
	}

	accepted := shouldProcessIncomingMessage(
		event,
		cfg.triggerMode,
		cfg.triggerPrefix,
		a.getBotOpenID(),
		"",
	)
	if !accepted {
		return false
	}
	normalizeIncomingGroupJobTextForTriggerMode(job, cfg.triggerMode, cfg.triggerPrefix)
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

	if cfg.groupScenes.Work.Enabled {
		workTriggerMatched := shouldProcessIncomingMessage(
			event,
			cfg.triggerMode,
			cfg.triggerPrefix,
			a.getBotOpenID(),
			"",
		)
		if sessionKey := a.resolveExistingWorkSession(job, event, message); sessionKey != "" {
			// Existing work threads should accept attachment-only followups even
			// when the user does not repeat @bot on the file/image message.
			if !workTriggerMatched && len(job.Attachments) == 0 {
				return false
			}
			applyWorkSceneToJob(job, cfg, sessionKey)
			normalizeIncomingGroupJobTextForTriggerMode(job, cfg.triggerMode, cfg.triggerPrefix)
			if a.processor != nil && message != nil {
				a.processor.setWorkThreadID(sessionKey, strings.TrimSpace(deref(message.ThreadId)))
			}
			return true
		}

		if workTriggerMatched && hasSceneTriggerTag(job.Text, cfg.groupScenes.Work.TriggerTag) {
			sessionKey := buildWorkSceneSessionKey(job.ReceiveIDType, job.ReceiveID, job.SourceMessageID)
			applyWorkSceneToJob(job, cfg, sessionKey)
			normalizeIncomingGroupJobTextForTriggerMode(job, cfg.triggerMode, cfg.triggerPrefix)
			job.Text = trimSceneTriggerTag(job.Text, cfg.groupScenes.Work.TriggerTag)
			if a.processor != nil && message != nil {
				a.processor.setWorkThreadID(sessionKey, strings.TrimSpace(deref(message.ThreadId)))
			}
			return true
		}
	}

	if cfg.groupScenes.Chat.Enabled {
		applyChatSceneToJob(job, cfg, a.resolveCurrentChatSceneSessionKey(job.ReceiveIDType, job.ReceiveID))
		return true
	}
	return false
}

func (a *App) resolveCurrentChatSceneSessionKey(receiveIDType, receiveID string) string {
	sessionKey := buildChatSceneSessionKey(receiveIDType, receiveID)
	if sessionKey == "" {
		return ""
	}
	if resolved := strings.TrimSpace(a.findExistingSessionKey([]string{sessionKey})); resolved != "" {
		return resolved
	}
	return sessionKey
}

func (a *App) resolveExistingWorkSession(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) string {
	if job == nil || message == nil {
		return ""
	}
	sessionKey := strings.TrimSpace(a.findExistingSessionKey(buildWorkSceneLookupCandidates(job.ReceiveIDType, job.ReceiveID, message)))
	if !isWorkSceneSessionKey(sessionKey) {
		return ""
	}
	return sessionKey
}

func buildWorkSceneLookupCandidates(receiveIDType, receiveID string, message *larkim.EventMessage) []string {
	base := buildSessionKey(receiveIDType, receiveID)
	if base == "" || message == nil {
		return nil
	}

	candidates := make([]string, 0, 3)
	if threadID := strings.TrimSpace(deref(message.ThreadId)); threadID != "" {
		appendSessionKeyCandidate(&candidates, base+threadAliasToken+threadID)
	}
	if rootID := strings.TrimSpace(deref(message.RootId)); rootID != "" {
		appendSessionKeyCandidate(&candidates, buildWorkSceneSessionKey(receiveIDType, receiveID, rootID))
	}
	if parentID := strings.TrimSpace(deref(message.ParentId)); parentID != "" {
		appendSessionKeyCandidate(&candidates, buildWorkSceneSessionKey(receiveIDType, receiveID, parentID))
	}
	return candidates
}

func applyChatSceneToJob(job *Job, cfg appRuntimeConfig, sessionKey string) {
	if job == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = buildChatSceneSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	job.Scene = jobSceneChat
	job.ResponseMode = jobResponseModeReply
	job.DisableAck = true
	job.SessionKey = sessionKey
	job.ResourceScopeKey = sessionKey
	job.CreateFeishuThread = cfg.groupScenes.Chat.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(cfg.groupScenes.Chat.NoReplyToken)
	applyLLMProfileToJob(job, cfg.llmProvider, cfg.groupScenes.Chat.LLMProfile, cfg.llmProfiles[cfg.groupScenes.Chat.LLMProfile])
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
	applyLLMProfileToJob(job, cfg.llmProvider, cfg.groupScenes.Work.LLMProfile, cfg.llmProfiles[cfg.groupScenes.Work.LLMProfile])
}

func applyLLMProfileToJob(job *Job, defaultProvider, profileName string, profile config.LLMProfileConfig) {
	if job == nil {
		return
	}
	provider := strings.TrimSpace(profile.Provider)
	if provider == "" {
		provider = strings.TrimSpace(defaultProvider)
	}
	job.LLMProvider = provider
	job.LLMModel = strings.TrimSpace(profile.Model)
	job.LLMProfile = strings.TrimSpace(profileName)
	job.LLMReasoningEffort = strings.TrimSpace(profile.ReasoningEffort)
	job.LLMVariant = strings.TrimSpace(profile.Variant)
	job.LLMPersonality = strings.TrimSpace(profile.Personality)
	job.LLMPromptPrefix = strings.TrimSpace(profile.PromptPrefix)
}

func buildChatSceneSessionKey(receiveIDType, receiveID string) string {
	base := buildSessionKey(receiveIDType, receiveID)
	if base == "" {
		return ""
	}
	return base + chatSceneToken
}

func buildWorkSceneSessionKey(receiveIDType, receiveID, sourceMessageID string) string {
	base := buildSessionKey(receiveIDType, receiveID)
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if base == "" || sourceMessageID == "" {
		return ""
	}
	return base + workSceneToken + workSceneSeedToken + sourceMessageID
}

func buildWorkSceneResourceScopeKeyFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	return strings.Replace(sessionKey, workSceneSeedToken, threadAliasToken, 1)
}

func isWorkSceneSessionKey(sessionKey string) bool {
	return strings.Contains(strings.TrimSpace(sessionKey), workSceneToken)
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
