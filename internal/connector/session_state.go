package connector

import (
	"fmt"
	"strings"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/statusview"
)

type sessionUsageStats = statusview.UsageStats

type sessionState struct {
	ThreadID      string            `json:"thread_id"`
	Aliases       []string          `json:"aliases,omitempty"`
	WorkThreadID  string            `json:"work_thread_id,omitempty"`
	WorkDir       string            `json:"work_dir,omitempty"`
	ScopeKey      string            `json:"scope_key,omitempty"`
	Usage         sessionUsageStats `json:"usage,omitempty"`
	LastMessageAt time.Time         `json:"last_message_at"`
}

type sessionStateSnapshot struct {
	BotID    string                  `json:"bot_id,omitempty"`
	BotName  string                  `json:"bot_name,omitempty"`
	Sessions map[string]sessionState `json:"sessions"`
}

const maxSessionAliases = 32
const chatSceneToken = "|scene:" + jobSceneChat
const workSceneToken = "|scene:" + jobSceneWork
const workSceneSeedToken = "|seed:"
const workSceneSeedKeyToken = workSceneToken + workSceneSeedToken
const messageAliasToken = "|message:"
const threadAliasToken = "|thread:"
const chatSceneResetToken = "|reset:"

func (p *Processor) getThreadID(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.sessions[sessionKey]
	if !ok {
		return ""
	}
	return strings.TrimSpace(state.ThreadID)
}

func (p *Processor) resolveCanonicalSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resolveCanonicalSessionKeyLocked(sessionKey)
}

func (p *Processor) rememberSessionAliases(sessionKey string, aliases ...string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || len(aliases) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
	if !ok {
		state = sessionState{}
	}
	workSeedAlias := buildWorkSceneSeedAlias(canonicalKey)

	changed := false
	for _, rawAlias := range aliases {
		alias := strings.TrimSpace(rawAlias)
		if alias == "" || alias == canonicalKey {
			continue
		}
		existingKey := p.resolveCanonicalSessionKeyLocked(alias)
		if existingKey != "" && existingKey != canonicalKey {
			continue
		}
		if alias == workSeedAlias {
			continue
		}
		if threadID := extractThreadIDFromAlias(alias); isWorkSceneSessionKey(canonicalKey) && threadID != "" {
			if state.WorkThreadID == threadID {
				continue
			}
			state.WorkThreadID = threadID
			changed = true
			continue
		}
		if containsSessionAlias(state.Aliases, alias) {
			continue
		}
		state.Aliases = appendSessionAliasWithLimit(state.Aliases, alias, maxSessionAliases)
		p.sessionAliases[alias] = canonicalKey
		changed = true
	}
	if !changed && ok {
		return
	}

	p.sessions[canonicalKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) setThreadID(sessionKey string, threadID string) {
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
	if !ok {
		state = sessionState{}
	}
	if state.ThreadID == threadID {
		return
	}
	state.ThreadID = threadID
	p.sessions[canonicalKey] = state
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
	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
	if !ok {
		state = sessionState{}
	}
	if state.WorkThreadID == workThreadID {
		return
	}
	state.WorkThreadID = workThreadID
	p.sessions[canonicalKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) touchSessionMessage(sessionKey string, at time.Time) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
	if !ok {
		state = sessionState{}
	}
	if state.ScopeKey == "" {
		state.ScopeKey = scopeKeyFromSessionKey(canonicalKey)
	}
	if state.LastMessageAt.Equal(at) {
		return
	}
	state.LastMessageAt = at
	p.sessions[canonicalKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) recordSessionUsage(sessionKey string, usage agentbridge.Usage) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || !usage.HasUsage() {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
	if !ok {
		state = sessionState{}
	}
	if state.ScopeKey == "" {
		state.ScopeKey = scopeKeyFromSessionKey(canonicalKey)
	}
	state.Usage.AddUsage(usage, p.now())
	p.sessions[canonicalKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) resetChatSceneSession(receiveIDType, receiveID string) (string, string) {
	baseKey := buildChatSceneSessionKey(receiveIDType, receiveID)
	if baseKey == "" {
		return "", ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	currentKey := p.resolveCanonicalSessionKeyLocked(baseKey)
	if currentKey == "" {
		currentKey = baseKey
	}

	switch {
	case currentKey == baseKey:
		p.removeSessionAliasesFromIndexLocked(currentKey)
		delete(p.sessions, currentKey)
	default:
		state, ok := p.sessions[currentKey]
		if ok {
			delete(p.sessionAliases, baseKey)
			state.Aliases = removeSessionAlias(state.Aliases, baseKey)
			p.sessions[currentKey] = state
		}
	}

	newKey := fmt.Sprintf("%s%s%d", baseKey, chatSceneResetToken, p.now().UnixNano())
	if _, exists := p.sessions[newKey]; exists {
		newKey = fmt.Sprintf("%s%s%d-%d", baseKey, chatSceneResetToken, p.now().UnixNano(), p.stateVersion+1)
	}
	p.sessions[newKey] = sessionState{
		Aliases:  []string{baseKey},
		ScopeKey: scopeKeyFromSessionKey(newKey),
	}
	p.sessionAliases[baseKey] = newKey
	p.markStateChangedLocked()
	return currentKey, newKey
}

func (p *Processor) getSessionWorkDir(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
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
	canonicalKey := p.resolveCanonicalSessionKeyLocked(sessionKey)
	if canonicalKey == "" {
		canonicalKey = sessionKey
	}
	state, ok := p.sessions[canonicalKey]
	if !ok {
		state = sessionState{}
	}
	state.WorkDir = workDir
	p.sessions[canonicalKey] = state
	p.markStateChangedLocked()
}
