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

	builder := newSessionKeyBuilder("group", cfg.triggerMode, cfg.triggerPrefix, a.getBotOpenID(),
		cfg.groupScenes.Chat.Enabled, cfg.groupScenes.Work.Enabled,
		cfg.groupScenes.Work.TriggerTag, a, job)
	return a.routeWithSessionKeyBuilder(job, event, message, builder)
}

func (a *App) routePrivateSceneJob(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) bool {
	cfg := a.runtimeConfig()
	if !cfg.privateScenes.Chat.Enabled && !cfg.privateScenes.Work.Enabled {
		if message != nil {
			a.resolveJobSessionKey(job, message)
		}
		return true
	}

	builder := newSessionKeyBuilder("private", cfg.triggerMode, cfg.triggerPrefix, a.getBotOpenID(),
		cfg.privateScenes.Chat.Enabled, cfg.privateScenes.Work.Enabled,
		cfg.privateScenes.Work.TriggerTag, a, job)
	return a.routeWithSessionKeyBuilder(job, event, message, builder)
}

type sessionKeyBuilder struct {
	scope       string
	triggerMode string
	botOpenID   string
	chatEnabled bool
	workEnabled bool
	workTag     string
	app         *App
	job         *Job

	matchedWorkTag bool
	triggerMatched bool
	existingWork   string
	activeWork     string
}

func newSessionKeyBuilder(scope, triggerMode, triggerPrefix, botOpenID string,
	chatEnabled, workEnabled bool, workTag string,
	app *App, job *Job,
) *sessionKeyBuilder {
	b := &sessionKeyBuilder{
		scope:       scope,
		triggerMode: triggerMode,
		botOpenID:   botOpenID,
		chatEnabled: chatEnabled,
		workEnabled: workEnabled,
		workTag:     strings.TrimSpace(workTag),
		app:         app,
		job:         job,
	}
	if b.workTag == "" {
		b.workTag = "#work"
	}
	return b
}

func (b *sessionKeyBuilder) evaluate(event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) {
	if !b.workEnabled && !b.chatEnabled {
		return
	}

	triggerMatched := isGroupMessageTriggered(event, b.triggerMode, "", b.botOpenID, "")
	b.triggerMatched = triggerMatched || isBuiltinCommandText(b.job.Text)

	if b.workEnabled && message != nil {
		candidates := buildWorkSessionLookupCandidates(
			b.job.ReceiveIDType, b.job.ReceiveID,
			strings.TrimSpace(deref(message.ThreadId)),
			strings.TrimSpace(deref(message.RootId)),
			strings.TrimSpace(deref(message.ParentId)),
		)
		for _, candidate := range candidates {
			if resolved := b.app.findExistingSessionKey([]string{candidate}); resolved != "" {
				if isWorkSessionKey(resolved) {
					b.existingWork = resolved
					return
				}
			}
		}

		if activeKey := b.app.findActiveWorkSessionKey(b.job.ReceiveIDType, b.job.ReceiveID); activeKey != "" {
			b.activeWork = activeKey
			return
		}
	}

	if b.workEnabled && triggerMatched && hasSceneTriggerTag(b.job.Text, b.workTag) {
		b.matchedWorkTag = true
		return
	}
	if b.workEnabled && hasSceneTriggerTag(b.job.Text, b.workTag) {
		return
	}
}

func (b *sessionKeyBuilder) resolveScene() (sessionKey string, scene string) {
	if b.existingWork != "" {
		return b.existingWork, jobSceneWork
	}
	if b.activeWork != "" {
		return b.activeWork, jobSceneWork
	}
	if b.matchedWorkTag {
		key := buildWorkSessionKey(b.job.ReceiveIDType, b.job.ReceiveID, b.job.SourceMessageID)
		return key, jobSceneWork
	}
	if b.chatEnabled {
		key := restoreChatSceneKey(b.job.ReceiveIDType, b.job.ReceiveID)
		return key, jobSceneChat
	}
	return "", ""
}

func (a *App) routeWithSessionKeyBuilder(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage, builder *sessionKeyBuilder) bool {
	builder.evaluate(event, message)

	sessionKey, scene := builder.resolveScene()
	if sessionKey == "" {
		return false
	}

	switch scene {
	case jobSceneWork:
		if builder.matchedWorkTag {
			a.applyWorkSceneToJob(job, sessionKey)
			normalizeIncomingGroupJobTextForTriggerMode(job, builder.triggerMode, "")
			job.Text = trimSceneTriggerTag(job.Text, builder.workTag)
			if a.processor != nil && message != nil {
				a.processor.setWorkThreadID(sessionKey, strings.TrimSpace(deref(message.ThreadId)))
			}
			return true
		}
		if builder.existingWork != "" {
			if !builder.triggerMatched && len(job.Attachments) == 0 {
				return false
			}
			a.applyWorkSceneToJob(job, sessionKey)
			normalizeIncomingGroupJobTextForTriggerMode(job, builder.triggerMode, "")
			if a.processor != nil && message != nil {
				a.processor.setWorkThreadID(sessionKey, strings.TrimSpace(deref(message.ThreadId)))
			}
			return true
		}
		if builder.activeWork != "" {
			a.applyWorkSceneToJob(job, sessionKey)
			return true
		}
	default:
		applyChatSceneToJob(job, sessionKey)
		return true
	}

	return false
}

func (a *App) resolveCurrentChatSceneSessionKey(receiveIDType, receiveID string) string {
	return restoreChatSceneKey(receiveIDType, receiveID)
}

func (a *App) resolveExistingWorkSession(job *Job, event *larkim.P2MessageReceiveV1, message *larkim.EventMessage) string {
	if job == nil || message == nil {
		return ""
	}
	builder := newSessionKeyBuilder("group", "", "", a.getBotOpenID(), false, true, "#work", a, job)
	builder.evaluate(event, message)
	if builder.existingWork != "" {
		return builder.existingWork
	}
	return ""
}

func (a *App) resolveExistingPrivateWorkSession(job *Job, message *larkim.EventMessage) string {
	if job == nil || message == nil {
		return ""
	}
	builder := newSessionKeyBuilder("private", "", "", a.getBotOpenID(), false, true, "#work", a, job)
	builder.evaluate(nil, message)
	if builder.existingWork != "" {
		return builder.existingWork
	}
	return ""
}

// applySceneConfigToJob applies the scene configuration from a job, group scene config, and session key.
func applyChatSceneToJob(job *Job, sessionKey string) {
	if job == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = restoreChatSceneKey(job.ReceiveIDType, job.ReceiveID)
	}
	job.Scene = jobSceneChat
	job.ResponseMode = jobResponseModeReply
	job.SessionKey = sessionKey
	job.ResourceScopeKey = sessionKey
}

func (a *App) applyWorkSceneToJob(job *Job, sessionKey string) {
	if job == nil {
		return
	}
	cfg := a.runtimeConfig()
	sceneCfg := cfg.groupScenes.Work
	job.Scene = jobSceneWork
	job.ResponseMode = jobResponseModeReply
	job.SessionKey = strings.TrimSpace(sessionKey)
	job.ResourceScopeKey = buildWorkSessionResourceScopeKey(sessionKey)
	job.CreateFeishuThread = sceneCfg.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(sceneCfg.NoReplyToken)
	applyLLMProfile(job, cfg.llmProvider, sceneCfg.LLMProfile, cfg.llmProfiles[sceneCfg.LLMProfile])
}

func (a *App) applyPrivateSceneJob(job *Job, sessionKey string, scene string, sceneCfg config.GroupSceneConfig) {
	if job == nil {
		return
	}
	cfg := a.runtimeConfig()
	job.Scene = scene
	job.ResponseMode = jobResponseModeReply
	job.SessionKey = strings.TrimSpace(sessionKey)
	if scene == jobSceneWork {
		job.ResourceScopeKey = buildWorkSessionResourceScopeKey(sessionKey)
	} else {
		job.ResourceScopeKey = sessionKey
	}
	job.CreateFeishuThread = sceneCfg.CreateFeishuThread
	job.NoReplyToken = strings.TrimSpace(sceneCfg.NoReplyToken)
	applyLLMProfile(job, cfg.llmProvider, sceneCfg.LLMProfile, cfg.llmProfiles[sceneCfg.LLMProfile])
}

func applyLLMProfile(job *Job, defaultProvider, profileName string, profile config.LLMProfileConfig) {
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

func hasThreadContext(message *larkim.EventMessage) bool {
	if message == nil {
		return false
	}
	return strings.TrimSpace(deref(message.ThreadId)) != "" ||
		strings.TrimSpace(deref(message.RootId)) != "" ||
		strings.TrimSpace(deref(message.ParentId)) != ""
}
