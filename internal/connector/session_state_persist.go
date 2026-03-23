package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Alice-space/alice/internal/logging"
)

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
		state.ScopeKey = strings.TrimSpace(state.ScopeKey)
		state.Aliases = normalizeSessionAliases(state.Aliases, key)
		if isWorkSceneSessionKey(key) {
			state.Aliases, state.WorkThreadID = migrateWorkThreadID(state.Aliases, state.WorkThreadID)
		}
		if state.ScopeKey == "" {
			state.ScopeKey = scopeKeyFromSessionKey(key)
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
	if runtime := p.runtimeSnapshot(); runtime.statusService != nil {
		snapshot.BotID, snapshot.BotName = runtime.statusService.Identity()
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
