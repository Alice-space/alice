package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateToScopedLayout_MigratesLegacyFilesAndState(t *testing.T) {
	root := t.TempDir()

	legacyLongPath := filepath.Join(root, LongTermFileName)
	if err := os.WriteFile(legacyLongPath, []byte("旧共享长期记忆"), 0o644); err != nil {
		t.Fatalf("write legacy long-term memory failed: %v", err)
	}
	legacyDailyDir := filepath.Join(root, ShortTermDirName)
	if err := os.MkdirAll(legacyDailyDir, 0o755); err != nil {
		t.Fatalf("create legacy daily dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDailyDir, "2026-03-02.md"), []byte("旧共享日记忆"), 0o644); err != nil {
		t.Fatalf("write legacy daily memory failed: %v", err)
	}
	legacyResourceDir := filepath.Join(root, ResourceDirName)
	if err := os.MkdirAll(filepath.Join(legacyResourceDir, "manual-send"), 0o755); err != nil {
		t.Fatalf("create legacy resources dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyResourceDir, "manual-send", "a.txt"), []byte("legacy resource"), 0o644); err != nil {
		t.Fatalf("write legacy resource failed: %v", err)
	}

	sessionStatePath := filepath.Join(root, "session_state.json")
	sessionRaw, err := json.Marshal(map[string]any{
		"sessions": map[string]any{
			"chat_id:oc_chat|thread:omt_1": map[string]any{
				"thread_id":                "thread_1",
				"last_message_at":          time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
				"last_idle_summary_anchor": time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy session state failed: %v", err)
	}
	if err := os.WriteFile(sessionStatePath, sessionRaw, 0o644); err != nil {
		t.Fatalf("write legacy session state failed: %v", err)
	}

	runtimeStatePath := filepath.Join(root, "runtime_state.json")
	runtimeRaw, err := json.Marshal(map[string]any{
		"latest": map[string]uint64{
			"chat_id:oc_chat|thread:omt_1": 1,
		},
		"pending": []map[string]any{
			{
				"receive_id":      "oc_chat",
				"receive_id_type": "chat_id",
				"session_key":     "chat_id:oc_chat|thread:omt_1",
				"session_version": 1,
				"text":            "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy runtime state failed: %v", err)
	}
	if err := os.WriteFile(runtimeStatePath, runtimeRaw, 0o644); err != nil {
		t.Fatalf("write legacy runtime state failed: %v", err)
	}

	if err := MigrateToScopedLayout(root); err != nil {
		t.Fatalf("migrate scoped layout failed: %v", err)
	}

	versionText, err := os.ReadFile(filepath.Join(root, LayoutVersionFileName))
	if err != nil {
		t.Fatalf("read layout version failed: %v", err)
	}
	if string(versionText) != ScopedLayoutVersion+"\n" {
		t.Fatalf("unexpected layout version: %q", string(versionText))
	}

	if _, err := os.Stat(legacyLongPath); !os.IsNotExist(err) {
		t.Fatalf("legacy long-term memory should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(legacyDailyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy daily dir should be removed, stat err=%v", err)
	}

	globalLongData, err := os.ReadFile(filepath.Join(root, GlobalDirName, LongTermFileName))
	if err != nil {
		t.Fatalf("read migrated global memory failed: %v", err)
	}
	if string(globalLongData) != "旧共享长期记忆\n" {
		t.Fatalf("unexpected migrated global memory: %q", string(globalLongData))
	}

	globalDailyData, err := os.ReadFile(filepath.Join(root, GlobalDirName, ShortTermDirName, "2026-03-02.md"))
	if err != nil {
		t.Fatalf("read migrated global daily failed: %v", err)
	}
	if string(globalDailyData) != "旧共享日记忆" {
		t.Fatalf("unexpected migrated global daily: %q", string(globalDailyData))
	}

	resourceData, err := os.ReadFile(filepath.Join(root, ResourceDirName, GlobalDirName, "manual-send", "a.txt"))
	if err != nil {
		t.Fatalf("read migrated global resource failed: %v", err)
	}
	if string(resourceData) != "legacy resource" {
		t.Fatalf("unexpected migrated resource contents: %q", string(resourceData))
	}

	type migratedSessionState struct {
		MemoryScopeKey string `json:"memory_scope_key"`
	}
	var migratedSession struct {
		Sessions map[string]migratedSessionState `json:"sessions"`
	}
	sessionData, err := os.ReadFile(sessionStatePath)
	if err != nil {
		t.Fatalf("read migrated session state failed: %v", err)
	}
	if err := json.Unmarshal(sessionData, &migratedSession); err != nil {
		t.Fatalf("unmarshal migrated session state failed: %v", err)
	}
	if migratedSession.Sessions["chat_id:oc_chat|thread:omt_1"].MemoryScopeKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected migrated session scope: %#v", migratedSession.Sessions)
	}

	var migratedRuntime map[string]any
	runtimeData, err := os.ReadFile(runtimeStatePath)
	if err != nil {
		t.Fatalf("read migrated runtime state failed: %v", err)
	}
	if err := json.Unmarshal(runtimeData, &migratedRuntime); err != nil {
		t.Fatalf("unmarshal migrated runtime state failed: %v", err)
	}
	pending, ok := migratedRuntime["pending"].([]any)
	if !ok || len(pending) != 1 {
		t.Fatalf("unexpected migrated pending runtime state: %#v", migratedRuntime["pending"])
	}
	job, ok := pending[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected migrated runtime job payload: %#v", pending[0])
	}
	if job["memory_scope_key"] != "chat_id:oc_chat" {
		t.Fatalf("unexpected migrated runtime scope: %#v", job)
	}
}
