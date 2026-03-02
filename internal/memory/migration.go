package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func MigrateToScopedLayout(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("memory dir is empty")
	}
	if err := ensureLayoutDirs(root); err != nil {
		return fmt.Errorf("prepare scoped memory layout failed: %w", err)
	}

	versionPath := layoutVersionPath(root)
	currentVersion, err := readOptionalFile(versionPath)
	if err != nil {
		return fmt.Errorf("read memory layout version failed: %w", err)
	}
	if strings.TrimSpace(currentVersion) == ScopedLayoutVersion {
		return nil
	}

	if err := migrateLegacyLongTermMemory(root); err != nil {
		return err
	}
	if err := migrateLegacyDailyMemory(root); err != nil {
		return err
	}
	if err := migrateLegacyResources(root); err != nil {
		return err
	}
	if err := migrateSessionState(root); err != nil {
		return err
	}
	if err := migrateRuntimeState(root); err != nil {
		return err
	}
	if err := os.WriteFile(versionPath, []byte(ScopedLayoutVersion+"\n"), 0o644); err != nil {
		return fmt.Errorf("write memory layout version failed: %w", err)
	}
	return nil
}

func migrateLegacyLongTermMemory(root string) error {
	legacyPath := filepath.Join(root, LongTermFileName)
	legacyText, err := readOptionalFile(legacyPath)
	if err != nil {
		return fmt.Errorf("read legacy long-term memory failed: %w", err)
	}
	if strings.TrimSpace(legacyText) == "" {
		if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty legacy long-term memory failed: %w", err)
		}
		return nil
	}

	targetPath := globalLongTermPath(root)
	targetText, err := readOptionalFile(targetPath)
	if err != nil {
		return fmt.Errorf("read scoped global memory failed: %w", err)
	}

	legacyText = strings.TrimSpace(legacyText)
	nextText := legacyText
	if trimmedTarget := strings.TrimSpace(targetText); trimmedTarget != "" {
		nextText = trimmedTarget
		if !strings.Contains(trimmedTarget, legacyText) {
			nextText = trimmedTarget + "\n\n" + legacyText
		}
	}
	if err := os.WriteFile(targetPath, []byte(nextText+"\n"), 0o644); err != nil {
		return fmt.Errorf("write scoped global memory failed: %w", err)
	}
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy long-term memory failed: %w", err)
	}
	return nil
}

func migrateLegacyDailyMemory(root string) error {
	legacyDir := filepath.Join(root, ShortTermDirName)
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy daily memory dir failed: %w", err)
	}

	targetDir := globalDailyDir(root)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}

		legacyPath := filepath.Join(legacyDir, name)
		legacyData, err := os.ReadFile(legacyPath)
		if err != nil {
			return fmt.Errorf("read legacy daily memory file failed: %w", err)
		}
		targetPath := filepath.Join(targetDir, name)
		targetData, err := os.ReadFile(targetPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read scoped daily memory file failed: %w", err)
		}

		nextData := legacyData
		if len(targetData) > 0 {
			nextData = targetData
			if !strings.Contains(string(targetData), string(legacyData)) {
				nextData = append(append(targetData, '\n'), legacyData...)
			}
		}
		if err := os.WriteFile(targetPath, nextData, 0o644); err != nil {
			return fmt.Errorf("write scoped daily memory file failed: %w", err)
		}
		if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove legacy daily memory file failed: %w", err)
		}
	}
	if err := os.RemoveAll(legacyDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove legacy daily memory dir failed: %w", err)
	}
	return nil
}

func migrateSessionState(root string) error {
	path := filepath.Join(root, "session_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read session state for migration failed: %w", err)
	}

	type migratedSessionState struct {
		MemoryScopeKey        string `json:"memory_scope_key"`
		ThreadID              string `json:"thread_id"`
		LastMessageAt         any    `json:"last_message_at"`
		LastIdleSummaryAnchor any    `json:"last_idle_summary_anchor"`
	}
	var snapshot struct {
		Sessions map[string]migratedSessionState `json:"sessions"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("parse session state for migration failed: %w", err)
	}
	if len(snapshot.Sessions) == 0 {
		return nil
	}
	for sessionKey, state := range snapshot.Sessions {
		state.MemoryScopeKey = scopeKeyFromSessionKey(sessionKey)
		snapshot.Sessions[sessionKey] = state
	}

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal migrated session state failed: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write migrated session state failed: %w", err)
	}
	return nil
}

func migrateLegacyResources(root string) error {
	baseDir := resourceBaseDir(root)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy resources dir failed: %w", err)
	}

	targetDir := globalResourceDir(root)
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" || name == GlobalDirName || name == ScopeRootDirName {
			continue
		}

		sourcePath := filepath.Join(baseDir, name)
		targetPath := filepath.Join(targetDir, name)
		if err := os.Rename(sourcePath, targetPath); err != nil {
			return fmt.Errorf("move legacy resource %s failed: %w", name, err)
		}
	}
	return nil
}

func migrateRuntimeState(root string) error {
	path := filepath.Join(root, "runtime_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read runtime state for migration failed: %w", err)
	}

	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("parse runtime state for migration failed: %w", err)
	}
	rawPending, ok := snapshot["pending"].([]any)
	if !ok || len(rawPending) == 0 {
		return nil
	}
	for _, rawItem := range rawPending {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		scopeKey := strings.TrimSpace(toString(item["memory_scope_key"]))
		if scopeKey == "" {
			scopeKey = scopeKeyFromSessionKey(toString(item["session_key"]))
		}
		if scopeKey == "" {
			scopeKey = buildScopeKey(toString(item["receive_id_type"]), toString(item["receive_id"]))
		}
		if scopeKey != "" {
			item["memory_scope_key"] = scopeKey
		}
	}

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal migrated runtime state failed: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write migrated runtime state failed: %w", err)
	}
	return nil
}

func scopeKeyFromSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if idx := strings.Index(sessionKey, "|"); idx >= 0 {
		sessionKey = strings.TrimSpace(sessionKey[:idx])
	}
	return sessionKey
}

func buildScopeKey(receiveIDType, receiveID string) string {
	receiveIDType = strings.TrimSpace(receiveIDType)
	receiveID = strings.TrimSpace(receiveID)
	if receiveIDType == "" || receiveID == "" {
		return ""
	}
	return receiveIDType + ":" + receiveID
}

func toString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
