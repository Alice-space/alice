package connector

import (
	"strings"
	"unicode"
	"unicode/utf8"

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
	a.normalizeBotMentions(job, message)
	if !isGroupChatType(job.ChatType) {
		if cfg.privateScenes.Chat.Enabled || cfg.privateScenes.Work.Enabled {
			return a.routePrivateSceneJob(job, event, message)
		}
		if message != nil {
			a.resolveJobSessionKey(job, message)
		}
		return true
	}
	if isBuiltinCommandText(job.Text) {
		if isStatusCommand(job.Text) {
			if cfg.groupScenes.Work.Enabled {
				if sessionKey := a.resolveExistingWorkSession(job, event, message); sessionKey != "" {
					applyWorkSceneToJob(job, cfg, sessionKey)
					if a.processor != nil && message != nil {
						a.processor.setWorkThreadID(sessionKey, strings.TrimSpace(deref(message.ThreadId)))
					}
					return true
				}
			}
			if hasThreadContext(message) {
				return false
			}
			if message != nil {
				a.resolveJobSessionKey(job, message)
			}
			return true
		}
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

func (a *App) routePrivateSceneJob(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) bool {
	cfg := a.runtimeConfig()
	if !cfg.privateScenes.Chat.Enabled && !cfg.privateScenes.Work.Enabled {
		if message != nil {
			a.resolveJobSessionKey(job, message)
		}
		return true
	}

	if cfg.privateScenes.Work.Enabled {
		if sessionKey := a.resolveExistingPrivateWorkSession(job, message); sessionKey != "" {
			applyPrivateWorkSceneToJob(job, cfg, sessionKey)
			return true
		}

		if activeKey := a.findActiveWorkSessionKey(job.ReceiveIDType, job.ReceiveID); activeKey != "" {
			applyPrivateWorkSceneToJob(job, cfg, activeKey)
			return true
		}

		if hasSceneTriggerTag(job.Text, cfg.privateScenes.Work.TriggerTag) {
			sessionKey := buildWorkSceneSessionKey(job.ReceiveIDType, job.ReceiveID, job.SourceMessageID)
			applyPrivateWorkSceneToJob(job, cfg, sessionKey)
			job.Text = trimSceneTriggerTag(job.Text, cfg.privateScenes.Work.TriggerTag)
			return true
		}
	}

	if cfg.privateScenes.Chat.Enabled {
		sessionKey := a.resolvePrivateChatSceneSessionKey(job.ReceiveIDType, job.ReceiveID)
		applyPrivateChatSceneToJob(job, cfg, sessionKey)
		return true
	}

	if message != nil {
		a.resolveJobSessionKey(job, message)
	}
	return true
}

func (a *App) resolvePrivateChatSceneSessionKey(receiveIDType, receiveID string) string {
	sessionKey := buildChatSceneSessionKey(receiveIDType, receiveID)
	if sessionKey == "" {
		return ""
	}
	if resolved := strings.TrimSpace(a.findExistingSessionKey([]string{sessionKey})); resolved != "" {
		return resolved
	}
	return sessionKey
}

func (a *App) resolveExistingPrivateWorkSession(job *Job, message *larkim.EventMessage) string {
	if job == nil || message == nil {
		return ""
	}
	candidates := buildPrivateWorkSceneLookupCandidates(job.ReceiveIDType, job.ReceiveID, message)
	sessionKey := strings.TrimSpace(a.findExistingSessionKey(candidates))
	if !isWorkSceneSessionKey(sessionKey) {
		return ""
	}
	return sessionKey
}

func buildPrivateWorkSceneLookupCandidates(receiveIDType, receiveID string, message *larkim.EventMessage) []string {
	base := buildSessionKey(receiveIDType, receiveID)
	if base == "" || message == nil {
		return nil
	}

	candidates := make([]string, 0, 4)
	if threadID := strings.TrimSpace(deref(message.ThreadId)); threadID != "" {
		appendSessionKeyCandidate(&candidates, base+threadAliasToken+threadID)
	}
	if rootID := strings.TrimSpace(deref(message.RootId)); rootID != "" {
		appendSessionKeyCandidate(&candidates, buildWorkSceneSessionKey(receiveIDType, receiveID, rootID))
	}
	if parentID := strings.TrimSpace(deref(message.ParentId)); parentID != "" {
		appendSessionKeyCandidate(&candidates, buildWorkSceneSessionKey(receiveIDType, receiveID, parentID))
	}
	if sourceMessageID := strings.TrimSpace(deref(message.MessageId)); sourceMessageID != "" {
		appendSessionKeyCandidate(&candidates, buildWorkSceneSessionKey(receiveIDType, receiveID, sourceMessageID))
	}
	return candidates
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
	applyChatSceneConfigToJob(job, cfg, cfg.groupScenes.Chat, sessionKey)
}

func applyWorkSceneToJob(job *Job, cfg appRuntimeConfig, sessionKey string) {
	applyWorkSceneConfigToJob(job, cfg, cfg.groupScenes.Work, sessionKey)
}

func applyPrivateChatSceneToJob(job *Job, cfg appRuntimeConfig, sessionKey string) {
	applyChatSceneConfigToJob(job, cfg, cfg.privateScenes.Chat, sessionKey)
}

func applyPrivateWorkSceneToJob(job *Job, cfg appRuntimeConfig, sessionKey string) {
	applyWorkSceneConfigToJob(job, cfg, cfg.privateScenes.Work, sessionKey)
}

func applyChatSceneConfigToJob(job *Job, cfg appRuntimeConfig, sceneCfg config.GroupSceneConfig, sessionKey string) {
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
	job.CreateFeishuThread = sceneCfg.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(sceneCfg.NoReplyToken)
	applyLLMProfileToJob(job, cfg.llmProvider, sceneCfg.LLMProfile, cfg.llmProfiles[sceneCfg.LLMProfile])
}

func applyWorkSceneConfigToJob(job *Job, cfg appRuntimeConfig, sceneCfg config.GroupSceneConfig, sessionKey string) {
	if job == nil {
		return
	}
	job.Scene = jobSceneWork
	job.ResponseMode = jobResponseModeReply
	job.DisableAck = false
	job.SessionKey = strings.TrimSpace(sessionKey)
	job.ResourceScopeKey = buildWorkSceneResourceScopeKeyFromSessionKey(sessionKey)
	job.CreateFeishuThread = sceneCfg.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(sceneCfg.NoReplyToken)
	applyLLMProfileToJob(job, cfg.llmProvider, sceneCfg.LLMProfile, cfg.llmProfiles[sceneCfg.LLMProfile])
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

func (a *App) normalizeBotMentions(job *Job, message *larkim.EventMessage) {
	if job == nil || message == nil || !isGroupChatType(job.ChatType) {
		return
	}
	job.Text = stripBotMentionText(job.Text, message.Mentions, a.getBotOpenID(), "")
}

func stripBotMentionText(
	text string,
	mentions []*larkim.MentionEvent,
	botOpenID string,
	botUserID string,
) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" || len(mentions) == 0 {
		return normalized
	}

	for {
		changed := false
		for _, mention := range mentions {
			if !isBotMentionEvent(mention, botOpenID, botUserID) {
				continue
			}
			for _, token := range mentionTokens(mention) {
				next, ok := removeMentionToken(normalized, token)
				if !ok {
					continue
				}
				normalized = next
				changed = true
				break
			}
			if changed {
				break
			}
		}
		if !changed {
			return normalized
		}
	}
}

func isBotMentionEvent(mention *larkim.MentionEvent, botOpenID, botUserID string) bool {
	if mention == nil || mention.Id == nil {
		return false
	}
	botOpenID = strings.TrimSpace(botOpenID)
	botUserID = strings.TrimSpace(botUserID)
	openID := strings.TrimSpace(deref(mention.Id.OpenId))
	userID := strings.TrimSpace(deref(mention.Id.UserId))
	return (botOpenID != "" && openID == botOpenID) ||
		(botUserID != "" && userID == botUserID)
}

func mentionTokens(mention *larkim.MentionEvent) []string {
	if mention == nil {
		return nil
	}
	tokens := make([]string, 0, 2)
	if key := strings.TrimSpace(deref(mention.Key)); key != "" {
		tokens = append(tokens, key)
	}
	if name := formatMentionDisplayName(deref(mention.Name)); name != "" {
		tokens = append(tokens, name)
	}
	return tokens
}

func removeMentionToken(text, token string) (string, bool) {
	text = strings.TrimSpace(text)
	token = strings.TrimSpace(token)
	if text == "" || token == "" || !strings.Contains(text, token) {
		return text, false
	}

	var b strings.Builder
	removed := false
	start := 0
	for {
		idx := strings.Index(text[start:], token)
		if idx < 0 {
			b.WriteString(text[start:])
			break
		}
		idx += start
		end := idx + len(token)
		if isInboundMentionBoundaryBefore(text, idx) && isInboundMentionBoundaryAfter(text, end) {
			b.WriteString(text[start:idx])
			b.WriteByte(' ')
			start = end
			removed = true
			continue
		}
		b.WriteString(text[start:end])
		start = end
	}
	if !removed {
		return text, false
	}
	return cleanupMentionRemovalWhitespace(b.String()), true
}

func isInboundMentionBoundaryBefore(text string, idx int) bool {
	if idx <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:idx])
	return unicode.IsSpace(r)
}

func isInboundMentionBoundaryAfter(text string, idx int) bool {
	if idx >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[idx:])
	return r == '#' || r == '/' || unicode.IsSpace(r) || unicode.IsPunct(r)
}

func cleanupMentionRemovalWhitespace(text string) string {
	text = strings.TrimSpace(text)
	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}
	text = strings.ReplaceAll(text, " \n", "\n")
	text = strings.ReplaceAll(text, "\n ", "\n")
	return text
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

func hasThreadContext(message *larkim.EventMessage) bool {
	if message == nil {
		return false
	}
	return strings.TrimSpace(deref(message.ThreadId)) != "" ||
		strings.TrimSpace(deref(message.RootId)) != "" ||
		strings.TrimSpace(deref(message.ParentId)) != ""
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
