package connector

import (
	"context"
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
	MemoryScopeKey        string    `json:"memory_scope_key"`
	ThreadID              string    `json:"thread_id"`
	LastMessageAt         time.Time `json:"last_message_at"`
	LastIdleSummaryAnchor time.Time `json:"last_idle_summary_anchor"`
	SummaryRunning        bool      `json:"-"`
}

type sessionStateSnapshot struct {
	Sessions map[string]sessionState `json:"sessions"`
}

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

func (p *Processor) setThreadID(sessionKey string, threadID string) {
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" || threadID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.sessions[sessionKey]
	if !ok {
		state = sessionState{}
	}
	if state.ThreadID == threadID {
		return
	}
	state.ThreadID = threadID
	p.sessions[sessionKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) rememberSessionScope(sessionKey, memoryScopeKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	memoryScopeKey = strings.TrimSpace(memoryScopeKey)
	if sessionKey == "" || memoryScopeKey == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state, ok := p.sessions[sessionKey]
	if !ok {
		state = sessionState{}
	}
	if state.MemoryScopeKey == memoryScopeKey {
		return
	}
	state.MemoryScopeKey = memoryScopeKey
	p.sessions[sessionKey] = state
	p.markStateChangedLocked()
}

func (p *Processor) touchSessionMessage(sessionKey string, at time.Time) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state, ok := p.sessions[sessionKey]
	if !ok {
		state = sessionState{}
	}
	if state.LastMessageAt.Equal(at) {
		return
	}
	state.LastMessageAt = at
	p.sessions[sessionKey] = state
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
		state.MemoryScopeKey = strings.TrimSpace(state.MemoryScopeKey)
		state.ThreadID = strings.TrimSpace(state.ThreadID)
		state.SummaryRunning = false
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
		state.SummaryRunning = false
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

func (p *Processor) RunIdleSummaryScan(ctx context.Context, idleThreshold time.Duration) {
	if idleThreshold <= 0 {
		return
	}

	now := p.now()
	candidates := make([]idleSummaryCandidate, 0, 8)

	p.mu.Lock()
	for sessionKey, state := range p.sessions {
		if state.SummaryRunning {
			continue
		}
		if strings.TrimSpace(state.ThreadID) == "" {
			continue
		}
		if state.LastMessageAt.IsZero() {
			continue
		}
		if now.Sub(state.LastMessageAt) < idleThreshold {
			continue
		}
		if state.LastIdleSummaryAnchor.Equal(state.LastMessageAt) {
			continue
		}

		state.SummaryRunning = true
		p.sessions[sessionKey] = state
		candidates = append(candidates, idleSummaryCandidate{
			SessionKey:     sessionKey,
			MemoryScopeKey: defaultIfEmpty(state.MemoryScopeKey, memoryScopeKeyFromSessionKey(sessionKey)),
			ThreadID:       state.ThreadID,
			Anchor:         state.LastMessageAt,
		})
	}
	p.mu.Unlock()

	for _, candidate := range candidates {
		go p.runIdleSummaryTask(ctx, candidate)
	}
}

func (p *Processor) runIdleSummaryTask(ctx context.Context, candidate idleSummaryCandidate) {
	defer p.clearSummaryRunning(candidate.SessionKey)

	if !p.isSummaryAnchorCurrent(candidate.SessionKey, candidate.Anchor) {
		return
	}

	reply, nextThreadID, err := p.runLLM(ctx, candidate.ThreadID, idleSummaryPrompt, nil, nil)
	if err != nil {
		logging.Errorf("idle summary llm failed session=%s thread_id=%s: %v", candidate.SessionKey, candidate.ThreadID, err)
		return
	}
	if strings.TrimSpace(nextThreadID) != "" {
		p.setThreadID(candidate.SessionKey, nextThreadID)
	}

	if !p.isSummaryAnchorCurrent(candidate.SessionKey, candidate.Anchor) {
		return
	}

	summary := strings.TrimSpace(reply)
	if summary == "" {
		summary = "无重要新增信息"
	}
	if p.memory == nil {
		logging.Debugf("idle summary skipped write session=%s reason=no_memory_manager", candidate.SessionKey)
		return
	}
	if err := p.memory.AppendDailySummary(candidate.MemoryScopeKey, candidate.SessionKey, summary, p.now()); err != nil {
		logging.Errorf("append daily summary failed session=%s: %v", candidate.SessionKey, err)
		return
	}

	p.mu.Lock()
	state, ok := p.sessions[candidate.SessionKey]
	if ok && state.LastMessageAt.Equal(candidate.Anchor) {
		state.LastIdleSummaryAnchor = candidate.Anchor
		p.sessions[candidate.SessionKey] = state
		p.markStateChangedLocked()
	}
	p.mu.Unlock()
}

func (p *Processor) clearSummaryRunning(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.sessions[sessionKey]
	if !ok || !state.SummaryRunning {
		return
	}
	state.SummaryRunning = false
	p.sessions[sessionKey] = state
}

func (p *Processor) isSummaryAnchorCurrent(sessionKey string, anchor time.Time) bool {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.sessions[sessionKey]
	if !ok {
		return false
	}
	return state.LastMessageAt.Equal(anchor)
}

type idleSummaryCandidate struct {
	SessionKey     string
	MemoryScopeKey string
	ThreadID       string
	Anchor         time.Time
}
