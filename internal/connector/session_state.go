package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/logging"
)

type sessionState struct {
	ThreadID      string    `json:"thread_id"`
	Aliases       []string  `json:"aliases,omitempty"`
	WorkThreadID  string    `json:"work_thread_id,omitempty"`
	LastMessageAt time.Time `json:"last_message_at"`
}

type sessionStateSnapshot struct {
	Sessions map[string]sessionState `json:"sessions"`
}

const maxSessionAliases = 32
const workSceneSeedKeyToken = "|scene:work|seed:"
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
	if state.LastMessageAt.Equal(at) {
		return
	}
	state.LastMessageAt = at
	p.sessions[canonicalKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) markStateChangedLocked() {
	p.stateVersion++
}

func (p *Processor) LoadSessionState(path string) error {
	path = strings.TrimSpace(path)

	p.mu.Lock()
	p.stateFilePath = path
	p.mu.Unlock()

	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read session state failed: %w", err)
	}

	var snapshot sessionStateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("parse session state failed: %w", err)
	}

	loaded := make(map[string]sessionState, len(snapshot.Sessions))
	for rawKey, state := range snapshot.Sessions {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		state.ThreadID = strings.TrimSpace(state.ThreadID)
		state.WorkThreadID = strings.TrimSpace(state.WorkThreadID)
		state.Aliases = normalizeSessionAliases(state.Aliases, key)
		if isWorkSceneSessionKey(key) {
			state.Aliases, state.WorkThreadID = migrateWorkThreadID(state.Aliases, state.WorkThreadID)
		}
		loaded[key] = state
	}

	p.mu.Lock()
	p.sessions = loaded
	p.stateVersion = 0
	p.flushedVersion = 0
	p.mu.Unlock()

	logging.Debugf("session state loaded file=%s sessions=%d", path, len(loaded))
	return nil
}

func (p *Processor) resolveCanonicalSessionKeyLocked(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if _, ok := p.sessions[sessionKey]; ok {
		return sessionKey
	}
	for canonicalKey, state := range p.sessions {
		if stateMatchesSessionKey(canonicalKey, state, sessionKey) {
			return canonicalKey
		}
	}
	return ""
}

func normalizeSessionAliases(aliases []string, canonicalKey string) []string {
	normalized := make([]string, 0, len(aliases))
	for _, rawAlias := range aliases {
		alias := strings.TrimSpace(rawAlias)
		if alias == "" || alias == strings.TrimSpace(canonicalKey) || isThreadSessionAlias(alias) || containsSessionAlias(normalized, alias) {
			continue
		}
		normalized = append(normalized, alias)
		if len(normalized) >= maxSessionAliases {
			break
		}
	}
	return normalized
}

func appendSessionAlias(aliases []string, alias string) []string {
	return appendSessionAliasWithLimit(aliases, alias, maxSessionAliases)
}

func appendSessionAliasWithLimit(aliases []string, alias string, limit int) []string {
	aliases = normalizeSessionAliases(aliases, "")
	alias = strings.TrimSpace(alias)
	if alias == "" || containsSessionAlias(aliases, alias) {
		return aliases
	}
	aliases = append(aliases, alias)
	if limit <= 0 || len(aliases) <= limit {
		return aliases
	}
	return append([]string(nil), aliases[len(aliases)-limit:]...)
}

func removeSessionAlias(aliases []string, alias string) []string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return normalizeSessionAliases(aliases, "")
	}
	filtered := make([]string, 0, len(aliases))
	for _, rawAlias := range aliases {
		existing := strings.TrimSpace(rawAlias)
		if existing == "" || existing == alias {
			continue
		}
		filtered = append(filtered, existing)
	}
	return normalizeSessionAliases(filtered, "")
}

func containsSessionAlias(aliases []string, alias string) bool {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return false
	}
	for _, existing := range aliases {
		if strings.TrimSpace(existing) == alias {
			return true
		}
	}
	return false
}

func migrateWorkThreadID(aliases []string, workThreadID string) ([]string, string) {
	regular := make([]string, 0, len(aliases))
	workThreadID = strings.TrimSpace(workThreadID)
	for _, rawAlias := range aliases {
		alias := strings.TrimSpace(rawAlias)
		if alias == "" {
			continue
		}
		if isThreadSessionAlias(alias) {
			if workThreadID == "" {
				workThreadID = extractThreadIDFromAlias(alias)
			}
			continue
		}
		regular = appendSessionAliasWithLimit(regular, alias, maxSessionAliases)
	}
	return regular, workThreadID
}

func stateMatchesSessionKey(canonicalKey string, state sessionState, sessionKey string) bool {
	if containsSessionAlias(state.Aliases, sessionKey) {
		return true
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if buildWorkSceneSeedAlias(canonicalKey) == sessionKey {
		return true
	}
	return buildWorkSceneThreadAlias(canonicalKey, state.WorkThreadID) == sessionKey
}

func buildWorkSceneSeedAlias(canonicalKey string) string {
	canonicalKey = strings.TrimSpace(canonicalKey)
	idx := strings.Index(canonicalKey, workSceneSeedKeyToken)
	if idx <= 0 {
		return ""
	}
	base := strings.TrimSpace(canonicalKey[:idx])
	seedMessageID := strings.TrimSpace(canonicalKey[idx+len(workSceneSeedKeyToken):])
	if base == "" || seedMessageID == "" {
		return ""
	}
	return base + messageAliasToken + seedMessageID
}

func isThreadSessionAlias(alias string) bool {
	return strings.Contains(strings.TrimSpace(alias), threadAliasToken)
}

func buildWorkSceneThreadAlias(canonicalKey, workThreadID string) string {
	canonicalKey = strings.TrimSpace(canonicalKey)
	workThreadID = strings.TrimSpace(workThreadID)
	idx := strings.Index(canonicalKey, workSceneSeedKeyToken)
	if idx <= 0 || workThreadID == "" {
		return ""
	}
	base := strings.TrimSpace(canonicalKey[:idx])
	if base == "" {
		return ""
	}
	return base + threadAliasToken + workThreadID
}

func extractThreadIDFromAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	idx := strings.Index(alias, threadAliasToken)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(alias[idx+len(threadAliasToken):])
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
		delete(p.sessions, currentKey)
	default:
		state, ok := p.sessions[currentKey]
		if ok {
			state.Aliases = removeSessionAlias(state.Aliases, baseKey)
			p.sessions[currentKey] = state
		}
	}

	newKey := fmt.Sprintf("%s%s%d", baseKey, chatSceneResetToken, p.now().UnixNano())
	if _, exists := p.sessions[newKey]; exists {
		newKey = fmt.Sprintf("%s%s%d-%d", baseKey, chatSceneResetToken, p.now().UnixNano(), p.stateVersion+1)
	}
	p.sessions[newKey] = sessionState{
		Aliases: []string{baseKey},
	}
	p.markStateChangedLocked()
	return currentKey, newKey
}

func (p *Processor) FlushSessionState() error {
	return p.flushSessionState(true)
}

func (p *Processor) FlushSessionStateIfDirty() error {
	return p.flushSessionState(false)
}

func (p *Processor) flushSessionState(force bool) error {
	p.mu.Lock()
	path := strings.TrimSpace(p.stateFilePath)
	currentVersion := p.stateVersion
	flushedVersion := p.flushedVersion
	if !force && currentVersion == flushedVersion {
		p.mu.Unlock()
		return nil
	}
	if path == "" {
		p.mu.Unlock()
		return nil
	}

	snapshot := sessionStateSnapshot{
		Sessions: make(map[string]sessionState, len(p.sessions)),
	}
	for key, state := range p.sessions {
		snapshot.Sessions[key] = state
	}
	p.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session state dir failed: %w", err)
	}

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session state failed: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".session_state.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp session state failed: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp session state failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp session state failed: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace session state failed: %w", err)
	}

	p.mu.Lock()
	if currentVersion > p.flushedVersion {
		p.flushedVersion = currentVersion
	}
	p.mu.Unlock()
	return nil
}
