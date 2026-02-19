package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerInit_CreatesMemoryFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)

	mgr := NewManager(dir)
	mgr.now = func() time.Time { return now }

	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, LongTermFileName)); err != nil {
		t.Fatalf("long-term file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ShortTermDirName, "2026-02-19.md")); err != nil {
		t.Fatalf("short-term file missing: %v", err)
	}
}

func TestManagerBuildPrompt_ContainsLongTermAndShortTermDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	now := time.Date(2026, 2, 19, 11, 30, 0, 0, time.UTC)

	mgr := NewManager(dir)
	mgr.now = func() time.Time { return now }
	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}

	longPath := filepath.Join(dir, LongTermFileName)
	if err := os.WriteFile(longPath, []byte("长期偏好：回答要简洁。"), 0o644); err != nil {
		t.Fatalf("write long-term failed: %v", err)
	}
	shortPath := filepath.Join(dir, ShortTermDirName, "2026-02-19.md")
	if err := os.WriteFile(shortPath, []byte("今天提到：关注连接器稳定性。"), 0o644); err != nil {
		t.Fatalf("write short-term failed: %v", err)
	}

	prompt, err := mgr.BuildPrompt("帮我总结下")
	if err != nil {
		t.Fatalf("build prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "长期偏好：回答要简洁。") {
		t.Fatalf("prompt missing long-term memory: %s", prompt)
	}
	if strings.Contains(prompt, "今天提到：关注连接器稳定性。") {
		t.Fatalf("prompt should not inline short-term memory: %s", prompt)
	}
	if !strings.Contains(prompt, filepath.Join(dir, ShortTermDirName)) {
		t.Fatalf("prompt missing short-term dir location: %s", prompt)
	}
	if !strings.Contains(prompt, "按需记忆更新") {
		t.Fatalf("prompt missing memory-update guidance: %s", prompt)
	}
	if !strings.Contains(prompt, "帮我总结下") {
		t.Fatalf("prompt missing user message: %s", prompt)
	}
}

func TestManagerSaveInteraction_WritesShortAndLongTerm(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	mgr := NewManager(dir)
	mgr.now = func() time.Time { return now }
	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}

	if err := mgr.SaveInteraction("请记住：我偏好中文简洁输出", "好的，已记录。", false); err != nil {
		t.Fatalf("save interaction failed: %v", err)
	}

	shortBytes, err := os.ReadFile(filepath.Join(dir, ShortTermDirName, "2026-02-19.md"))
	if err != nil {
		t.Fatalf("read short-term failed: %v", err)
	}
	shortText := string(shortBytes)
	if !strings.Contains(shortText, "我偏好中文简洁输出") {
		t.Fatalf("short-term memory missing user text: %s", shortText)
	}
	if !strings.Contains(shortText, "好的，已记录。") {
		t.Fatalf("short-term memory missing assistant text: %s", shortText)
	}

	longBytes, err := os.ReadFile(filepath.Join(dir, LongTermFileName))
	if err != nil {
		t.Fatalf("read long-term failed: %v", err)
	}
	longText := string(longBytes)
	if !strings.Contains(longText, "我偏好中文简洁输出") {
		t.Fatalf("long-term memory missing triggered text: %s", longText)
	}
}

func TestManagerSaveInteraction_NoLongTermWhenNoKeyword(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	now := time.Date(2026, 2, 19, 13, 0, 0, 0, time.UTC)

	mgr := NewManager(dir)
	mgr.now = func() time.Time { return now }
	if err := mgr.Init(); err != nil {
		t.Fatalf("init memory failed: %v", err)
	}

	if err := mgr.SaveInteraction("这是一条普通对话", "普通回复", false); err != nil {
		t.Fatalf("save interaction failed: %v", err)
	}

	longBytes, err := os.ReadFile(filepath.Join(dir, LongTermFileName))
	if err != nil {
		t.Fatalf("read long-term failed: %v", err)
	}
	if strings.Contains(string(longBytes), "这是一条普通对话") {
		t.Fatalf("unexpected long-term write: %s", string(longBytes))
	}
}
