package connector

import (
	"strings"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/statusview"
)

type sessionUsageStats = statusview.UsageStats

type sessionState struct {
	ThreadID               string            `json:"thread_id"`
	WorkThreadID           string            `json:"work_thread_id,omitempty"`
	WorkDir                string            `json:"work_dir,omitempty"`
	BackendProvider        string            `json:"backend_provider,omitempty"`
	BackendModel           string            `json:"backend_model,omitempty"`
	BackendProfile         string            `json:"backend_profile,omitempty"`
	BackendReasoningEffort string            `json:"backend_reasoning_effort,omitempty"`
	BackendVariant         string            `json:"backend_variant,omitempty"`
	BackendPersonality     string            `json:"backend_personality,omitempty"`
	ScopeKey               string            `json:"scope_key,omitempty"`
	ReplyMessageIDs        []string          `json:"reply_message_ids,omitempty"`
	Usage                  sessionUsageStats `json:"usage,omitempty"`
	LastMessageAt          time.Time         `json:"last_message_at"`
}

type sessionStateSnapshot struct {
	BotID    string                  `json:"bot_id,omitempty"`
	BotName  string                  `json:"bot_name,omitempty"`
	Sessions map[string]sessionState `json:"sessions"`
}

const maxReplyMessageIDs = 64
const workSessionToken = "|work:"
const threadBindingToken = "|thread:"
const messageBindingToken = "|message:"

func (p *Processor) getThreadID(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		return ""
	}
	return strings.TrimSpace(state.ThreadID)
}

func (p *Processor) hasActiveSession(sessionKey string) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	state, ok := p.sessions[resolved]
	return ok && (state.ThreadID != "" || !state.LastMessageAt.IsZero())
}

func (p *Processor) resolveSessionKeyLocked(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if _, ok := p.sessions[sessionKey]; ok {
		return sessionKey
	}
	if resolved, ok := p.threadBindings[sessionKey]; ok && resolved != "" {
		if _, exists := p.sessions[resolved]; exists {
			return resolved
		}
	}
	return ""
}

func (p *Processor) setThreadID(sessionKey string, threadID string) {
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}
	if state.ThreadID == threadID {
		return
	}
	state.ThreadID = threadID
	p.sessions[resolved] = state
	p.markStateChangedLocked()
}

func (p *Processor) recordSessionMetadata(sessionKey string, job Job) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	provider := strings.ToLower(strings.TrimSpace(job.LLMProvider))
	model := strings.TrimSpace(job.LLMModel)
	profile := strings.TrimSpace(job.LLMProfile)
	reasoningEffort := strings.ToLower(strings.TrimSpace(job.LLMReasoningEffort))
	variant := strings.ToLower(strings.TrimSpace(job.LLMVariant))
	personality := strings.ToLower(strings.TrimSpace(job.LLMPersonality))
	if provider == "" && model == "" && profile == "" && reasoningEffort == "" && variant == "" && personality == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}
	changed := false
	if state.ScopeKey == "" {
		state.ScopeKey = scopeKeyFromSessionKey(resolved)
		changed = true
	}
	if provider != "" && state.BackendProvider != provider {
		state.BackendProvider = provider
		changed = true
	}
	if model != "" && state.BackendModel != model {
		state.BackendModel = model
		changed = true
	}
	if profile != "" && state.BackendProfile != profile {
		state.BackendProfile = profile
		changed = true
	}
	if reasoningEffort != "" && state.BackendReasoningEffort != reasoningEffort {
		state.BackendReasoningEffort = reasoningEffort
		changed = true
	}
	if variant != "" && state.BackendVariant != variant {
		state.BackendVariant = variant
		changed = true
	}
	if personality != "" && state.BackendPersonality != personality {
		state.BackendPersonality = personality
		changed = true
	}
	if !changed && ok {
		return
	}
	p.sessions[resolved] = state
	p.markStateChangedLocked()
}

func (p *Processor) setWorkThreadID(sessionKey string, workThreadID string) {
	sessionKey = strings.TrimSpace(sessionKey)
	workThreadID = strings.TrimSpace(workThreadID)
	if sessionKey == "" || workThreadID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}
	if state.WorkThreadID == workThreadID {
		return
	}
	state.WorkThreadID = workThreadID
	p.sessions[resolved] = state

	base := scopeKeyFromSessionKey(resolved)
	p.threadBindings[base+threadBindingToken+workThreadID] = resolved

	p.markStateChangedLocked()
}

func (p *Processor) bindReplyMessage(sessionKey, messageID string) {
	sessionKey = strings.TrimSpace(sessionKey)
	messageID = strings.TrimSpace(messageID)
	if sessionKey == "" || messageID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}

	if containsString(state.ReplyMessageIDs, messageID) {
		return
	}
	state.ReplyMessageIDs = appendLimited(state.ReplyMessageIDs, messageID, maxReplyMessageIDs)
	p.sessions[resolved] = state

	base := scopeKeyFromSessionKey(resolved)
	p.threadBindings[base+messageBindingToken+messageID] = resolved

	p.markStateChangedLocked()
}

func containsString(slice []string, value string) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}
	return false
}

func appendLimited(slice []string, value string, limit int) []string {
	slice = append(slice, value)
	if len(slice) > limit {
		slice = append([]string(nil), slice[len(slice)-limit:]...)
	}
	return slice
}

func (p *Processor) touchSessionMessage(sessionKey string, at time.Time) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}
	if state.ScopeKey == "" {
		state.ScopeKey = scopeKeyFromSessionKey(resolved)
	}
	if state.LastMessageAt.Equal(at) {
		return
	}
	state.LastMessageAt = at
	p.sessions[resolved] = state
	p.markStateChangedLocked()
}

func (p *Processor) recordSessionUsage(sessionKey string, usage llm.Usage) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || !usage.HasUsage() {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}
	if state.ScopeKey == "" {
		state.ScopeKey = scopeKeyFromSessionKey(resolved)
	}
	state.Usage.AddUsage(usage, p.now())
	p.sessions[resolved] = state
	p.markStateChangedLocked()
}

func (p *Processor) resetChatSceneSession(receiveIDType, receiveID string) (string, string) {
	baseKey := buildSessionKey(receiveIDType, receiveID)
	if baseKey == "" {
		return "", ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	currentKey := p.resolveSessionKeyLocked(baseKey)
	if currentKey == "" {
		currentKey = baseKey
	}
	oldKey := currentKey

	state, ok := p.sessions[currentKey]
	if !ok {
		return oldKey, oldKey
	}

	state.ThreadID = ""
	state.ReplyMessageIDs = nil
	p.sessions[currentKey] = state

	p.markStateChangedLocked()
	return oldKey, oldKey
}

func (p *Processor) getSessionWorkDir(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		return ""
	}
	return strings.TrimSpace(state.WorkDir)
}

func (p *Processor) setSessionWorkDir(sessionKey string, workDir string) {
	sessionKey = strings.TrimSpace(sessionKey)
	workDir = strings.TrimSpace(workDir)
	if sessionKey == "" || workDir == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	if !ok {
		state = sessionState{}
	}
	state.WorkDir = workDir
	p.sessions[resolved] = state
	p.markStateChangedLocked()
}

func (p *Processor) snapshotSessionState(sessionKey string) (string, sessionState, bool) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", sessionState{}, false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	resolved := p.resolveSessionKeyLocked(sessionKey)
	if resolved == "" {
		resolved = sessionKey
	}
	state, ok := p.sessions[resolved]
	return resolved, state, ok
}

func scopeKeyFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, "|"); idx >= 0 {
		return strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}

func isWorkSessionKey(sessionKey string) bool {
	return strings.Contains(strings.TrimSpace(sessionKey), workSessionToken)
}

func buildWorkSessionKey(receiveIDType, receiveID, seedMessageID string) string {
	base := buildSessionKey(receiveIDType, receiveID)
	seedMessageID = strings.TrimSpace(seedMessageID)
	if base == "" || seedMessageID == "" {
		return ""
	}
	return base + workSessionToken + seedMessageID
}

func detectSceneFromSessionKey(sessionKey string) string {
	if isWorkSessionKey(sessionKey) {
		return jobSceneWork
	}
	return jobSceneChat
}

func rebuildThreadBindingsLocked(p *Processor) {
	p.threadBindings = make(map[string]string, len(p.sessions))
	for sessionKey, state := range p.sessions {
		base := scopeKeyFromSessionKey(sessionKey)
		if state.WorkThreadID != "" {
			p.threadBindings[base+threadBindingToken+state.WorkThreadID] = sessionKey
		}
		for _, msgID := range state.ReplyMessageIDs {
			if msgID = strings.TrimSpace(msgID); msgID != "" {
				p.threadBindings[base+messageBindingToken+msgID] = sessionKey
			}
		}
	}
}

// buildWorkSessionResourceScopeKey builds a resource scope key from a work session key.
//
//	chat_id:oc_chat|work:om_seed → chat_id:oc_chat|thread:om_seed
func buildWorkSessionResourceScopeKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	return strings.Replace(sessionKey, workSessionToken, threadBindingToken, 1)
}

func restoreChatSceneKey(receiveIDType, receiveID string) string {
	return buildSessionKey(receiveIDType, receiveID)
}

// buildWorkSessionLookupCandidates returns all keys that might resolve to an existing work session.
func buildWorkSessionLookupCandidates(receiveIDType, receiveID string, threadID, rootID, parentID string) []string {
	base := buildSessionKey(receiveIDType, receiveID)
	if base == "" {
		return nil
	}

	var candidates []string
	appendCandidate := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		for _, c := range candidates {
			if c == key {
				return
			}
		}
		candidates = append(candidates, key)
	}

	if threadID != "" {
		appendCandidate(base + threadBindingToken + threadID)
	}
	if rootID != "" {
		appendCandidate(buildWorkSessionKey(receiveIDType, receiveID, rootID))
	}
	if parentID != "" {
		appendCandidate(buildWorkSessionKey(receiveIDType, receiveID, parentID))
	}

	return candidates
}

func (p *Processor) resolveWorkSessionByThread(baseKey, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.threadBindings[baseKey+threadBindingToken+threadID]
}

func (p *Processor) resolveSessionLookup(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resolveSessionKeyLocked(sessionKey)
}
